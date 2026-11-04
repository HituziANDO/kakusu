//go:build !windows

package main

// End-to-end tests: they build the real binary and run its subcommands under a
// temporary HOME. Each child gets only the environment set here, so a
// KAKUSU_FILE in the caller's shell never points it at a real vault, and runs
// in a new session, so it has no controlling terminal, reads passwords from
// stdin, and prints the password prompts to stderr.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const testPassword = "correct-horse"

// secrets are stored by newVault. Values use only uppercase letters, which
// nothing else in the list output contains, so tests can check single
// characters. db_password is longer than 6 characters and api_key is not.
var secrets = map[string]string{
	"app/db_password": "QXZVMTHJLN",
	"app/api_key":     "BCFGW",
}

var kakusuBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kakusu-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	kakusuBin = filepath.Join(dir, "kakusu")
	if out, err := exec.Command("go", "build", "-o", kakusuBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code           int
	stdout, stderr string
}

// syncBuffer lets the test read stderr while the child is still writing it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var promptRe = regexp.MustCompile(`(?:password|Confirm): `)

// kakusu runs the binary under home with extraEnv added. With no input, stdin
// is /dev/null. Otherwise each line is written only after the child prints one
// more password prompt: ui.PromptPassword creates a new bufio.Reader per call,
// so lines written together would be swallowed by the first prompt.
func kakusu(t *testing.T, home string, extraEnv, input []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(kakusuBin, args...)
	cmd.Dir = home
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "KAKUSU_LANG=en"}, extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stdout bytes.Buffer
	var stderr syncBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	var stdin *os.File
	if len(input) > 0 {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdin = r
		stdin = w
		defer r.Close()
		defer w.Close()
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	deadline := time.After(10 * time.Second)
feed:
	for i, line := range input {
		for len(promptRe.FindAllString(stderr.String(), -1)) <= i {
			select {
			case <-done:
				break feed
			case <-deadline:
				cmd.Process.Kill()
				t.Fatalf("kakusu %v: no prompt for input line %d; stderr: %q", args, i+1, stderr.String())
			case <-time.After(10 * time.Millisecond):
			}
		}
		fmt.Fprintln(stdin, line)
	}
	if stdin != nil {
		stdin.Close()
	}
	select {
	case <-done:
	case <-deadline:
		cmd.Process.Kill()
		t.Fatalf("kakusu %v did not exit; stderr: %q", args, stderr.String())
	}
	return result{code: cmd.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
}

func mustSucceed(t *testing.T, r result) result {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d; stdout %q; stderr %q", r.code, r.stdout, r.stderr)
	}
	return r
}

// newVault initializes a vault under a new HOME and stores secrets, leaving
// the key cached by the agent as after the user typed the password once.
func newVault(t *testing.T) string {
	t.Helper()
	// Short path: macOS limits Unix socket paths to 104 bytes.
	home, err := os.MkdirTemp("", "kk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	// The agent detaches from the child, so stop it explicitly.
	t.Cleanup(func() { kakusu(t, home, nil, nil, "agent", "stop") })

	mustSucceed(t, kakusu(t, home, nil, []string{testPassword, testPassword}, "init"))
	// stdin is /dev/null, so set succeeds only by using the cache.
	for ref, v := range secrets {
		mustSucceed(t, kakusu(t, home, nil, nil, "set", ref, v))
	}
	return home
}

func writeEnvFile(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, "x.env")
	if err := os.WriteFile(path, []byte("X=kks://app/db_password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValueCommandsAskForPasswordWhileCached(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"get", []string{"get", "app/db_password"}},
		{"show", []string{"show", "app/db_password"}},
		{"export_key", []string{"export", "app/db_password"}},
		{"export_group", []string{"export", "app"}},
		{"export_env", []string{"export", "--env", envFile}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := kakusu(t, home, nil, nil, tc.args...)
			if r.code == 0 || r.stdout != "" {
				t.Errorf("exit %d, stdout %q; want non-zero exit and empty stdout", r.code, r.stdout)
			}
			if !strings.Contains(r.stderr, "enter the master password: ") {
				t.Errorf("stderr %q; want the master password prompt", r.stderr)
			}
		})
	}
}

// promptBeforeError returns what a command printed to stderr before the error
// it died with. With stdin /dev/null, a command that asks for the password
// dies at the prompt, so that is the whole prompt.
func promptBeforeError(stderr string) string {
	prompt, _, _ := strings.Cut(stderr, "Error: ")
	return prompt
}

func TestPasswordPromptNamesActionAndTarget(t *testing.T) {
	home := newVault(t)
	writeEnvFile(t, home)
	// The commands get --env x.env relative to home and show it as the
	// absolute path, which the child builds on its resolved working directory.
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	envAbs := filepath.Join(realHome, "x.env")
	// get, show, export, and passwd always ask; the others ask only without the cache.
	noAgent := []string{"KAKUSU_NO_AGENT=1"}
	for _, tc := range []struct {
		name   string
		env    []string
		args   []string
		en, ja string
	}{
		{"get", nil, []string{"get", "app/db_password"},
			`To print the value of "app/db_password", enter the master password: `,
			`"app/db_password" の値を出力します。マスターパスワード: `},
		{"get_default_group", nil, []string{"get", "foo"},
			`To print the value of "default/foo", enter the master password: `,
			`"default/foo" の値を出力します。マスターパスワード: `},
		{"show", nil, []string{"show", "app/db_password"},
			`To print the value of "app/db_password", enter the master password: `,
			`"app/db_password" の値を出力します。マスターパスワード: `},
		{"export_key", nil, []string{"export", "app/db_password"},
			`To print the value of "app/db_password", enter the master password: `,
			`"app/db_password" の値を出力します。マスターパスワード: `},
		{"export_group", nil, []string{"export", "app"},
			`To print every value in group "app", enter the master password: `,
			`グループ "app" のすべての値を出力します。マスターパスワード: `},
		{"export_env", nil, []string{"export", "--env", "x.env"},
			`To print the values that "` + envAbs + `" refers to, enter the master password: `,
			`"` + envAbs + `" が参照する値を出力します。マスターパスワード: `},
		{"passwd", nil, []string{"passwd"},
			`Current master password: `,
			`現在のマスターパスワード: `},
		{"list", noAgent, []string{"list"},
			`To list keys, enter the master password: `,
			`キー一覧を表示します。マスターパスワード: `},
		{"set", noAgent, []string{"set", "app/api_key", "NEWVALUE"},
			`To store "app/api_key", enter the master password: `,
			`"app/api_key" を保存します。マスターパスワード: `},
		{"delete", noAgent, []string{"delete", "app/api_key"},
			`To delete "app/api_key", enter the master password: `,
			`"app/api_key" を削除します。マスターパスワード: `},
		{"run", noAgent, []string{"run", "--env", "x.env", "--", "printenv", "X"},
			`To run "printenv" "X" with the values that "` + envAbs + `" refers to, enter the master password: `,
			`"` + envAbs + `" が参照する値を渡して "printenv" "X" を実行します。マスターパスワード: `},
	} {
		for _, lang := range []struct{ code, want string }{{"en", tc.en}, {"ja", tc.ja}} {
			t.Run(tc.name+"_"+lang.code, func(t *testing.T) {
				r := kakusu(t, home, append([]string{"KAKUSU_LANG=" + lang.code}, tc.env...), nil, tc.args...)
				if got := promptBeforeError(r.stderr); got != lang.want {
					t.Errorf("prompt %q; want %q (exit %d)", got, lang.want, r.code)
				}
			})
		}
	}
}

func TestPasswordPromptEscapesTarget(t *testing.T) {
	home := newVault(t)
	envName := "x\n\x1b[31m.env"
	if err := os.WriteFile(filepath.Join(home, envName), []byte("X=kks://app/db_password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"export_env", []string{"export", "--env", envName},
			`To print the values that "` + realHome + `/x\n\x1b[31m.env" refers to, enter the master password: `},
		{"get", []string{"get", "evil\x1b[2J/k\ney"},
			`To print the value of "evil\x1b[2J/k\ney", enter the master password: `},
		// A raw target here would erase the line and paint a fake master
		// password prompt in front of the hidden value input.
		{"set_value", []string{"set", "g/k\r\x1b[2KTo list keys, enter the master password: \x1b]0;"},
			`"g/k\r\x1b[2KTo list keys, enter the master password: \x1b]0;" value: `},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := kakusu(t, home, nil, nil, tc.args...)
			if strings.Contains(r.stderr, "\x1b") {
				t.Errorf("stderr %q contains a raw ESC", r.stderr)
			}
			if got := promptBeforeError(r.stderr); got != tc.want {
				t.Errorf("prompt %q; want %q (exit %d)", got, tc.want, r.code)
			}
		})
	}
}

func TestGetWithPasswordPrintsValueAndCachesKey(t *testing.T) {
	home := newVault(t)
	mustSucceed(t, kakusu(t, home, nil, nil, "agent", "stop"))

	r := mustSucceed(t, kakusu(t, home, nil, []string{testPassword}, "get", "app/db_password"))
	if want := secrets["app/db_password"] + "\n"; r.stdout != want {
		t.Errorf("stdout %q; want %q", r.stdout, want)
	}
	// stdin is /dev/null, so list succeeds only with the key cached by get.
	mustSucceed(t, kakusu(t, home, nil, nil, "list"))
}

func TestPasswdAsksForCurrentPasswordWhileCached(t *testing.T) {
	home := newVault(t)
	vaultPath := filepath.Join(home, ".kakusu", "secrets.enc")
	before, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		input []string
	}{
		{"no_input", nil},
		{"new_password_only", []string{"NEWPASS12", "NEWPASS12"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := kakusu(t, home, nil, tc.input, "passwd")
			if r.code == 0 {
				t.Errorf("exit 0; want non-zero; stderr %q", r.stderr)
			}
			after, err := os.ReadFile(vaultPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Error("vault file changed")
			}
		})
	}
}

func TestPasswdChangesPasswordWithCurrentPassword(t *testing.T) {
	home := newVault(t)
	mustSucceed(t, kakusu(t, home, nil, []string{testPassword, "NEWPASS12", "NEWPASS12"}, "passwd"))

	r := mustSucceed(t, kakusu(t, home, nil, []string{"NEWPASS12"}, "get", "app/db_password"))
	if want := secrets["app/db_password"] + "\n"; r.stdout != want {
		t.Errorf("stdout %q; want %q", r.stdout, want)
	}
}

func TestRunUsesCache(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)

	r := mustSucceed(t, kakusu(t, home, nil, nil, "run", "--env", envFile, "--", "printenv", "X"))
	if want := secrets["app/db_password"] + "\n"; r.stdout != want {
		t.Errorf("stdout %q; want %q", r.stdout, want)
	}
}

func TestListPrintsNoPartOfValues(t *testing.T) {
	home := newVault(t)

	r := mustSucceed(t, kakusu(t, home, nil, nil, "list"))
	for ref, v := range secrets {
		key := ref[strings.Index(ref, "/")+1:]
		if !strings.Contains(r.stdout, key) {
			t.Errorf("stdout %q; want key %s listed", r.stdout, key)
		}
		if i := strings.IndexAny(r.stdout, v); i >= 0 {
			t.Errorf("stdout %q contains %q from the value of %s", r.stdout, r.stdout[i], ref)
		}
	}
}

func TestAgentClampsTTLTo8Hours(t *testing.T) {
	home := newVault(t)
	mustSucceed(t, kakusu(t, home, nil, nil, "agent", "stop"))
	// list starts the agent with KAKUSU_TTL as its startup argument.
	mustSucceed(t, kakusu(t, home, []string{"KAKUSU_TTL=1000h"}, []string{testPassword}, "list"))

	r := mustSucceed(t, kakusu(t, home, nil, nil, "agent", "status"))
	if !strings.Contains(r.stderr, "TTL:       8h0m0s\n") {
		t.Errorf("stderr %q; want TTL 8h0m0s", r.stderr)
	}
}
