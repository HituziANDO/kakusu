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
	"slices"
	"strconv"
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
	ref := "evil\x1b[2J/k\ney"
	noAgent := []string{"KAKUSU_NO_AGENT=1"}
	for _, tc := range []struct {
		name   string
		env    []string
		args   []string
		en, ja string
	}{
		{"get", nil, []string{"get", ref},
			`To print the value of "evil\x1b[2J/k\ney", enter the master password: `,
			`"evil\x1b[2J/k\ney" の値を出力します。マスターパスワード: `},
		{"export_group", nil, []string{"export", "g\x1b[2J\nx"},
			`To print every value in group "g\x1b[2J\nx", enter the master password: `,
			`グループ "g\x1b[2J\nx" のすべての値を出力します。マスターパスワード: `},
		{"export_env", nil, []string{"export", "--env", envName},
			`To print the values that "` + realHome + `/x\n\x1b[31m.env" refers to, enter the master password: `,
			`"` + realHome + `/x\n\x1b[31m.env" が参照する値を出力します。マスターパスワード: `},
		// A raw target here would erase the line and paint a fake master
		// password prompt in front of the hidden value input.
		{"set_value", nil, []string{"set", "g/k\r\x1b[2KTo list keys, enter the master password: \x1b]0;"},
			`"g/k\r\x1b[2KTo list keys, enter the master password: \x1b]0;" value: `,
			`"g/k\r\x1b[2KTo list keys, enter the master password: \x1b]0;" の値: `},
		{"set", noAgent, []string{"set", ref, "V"},
			`To store "evil\x1b[2J/k\ney", enter the master password: `,
			`"evil\x1b[2J/k\ney" を保存します。マスターパスワード: `},
		{"delete", noAgent, []string{"delete", ref},
			`To delete "evil\x1b[2J/k\ney", enter the master password: `,
			`"evil\x1b[2J/k\ney" を削除します。マスターパスワード: `},
		{"run", noAgent, []string{"run", "--env", envName, "--", "printenv", "X\x1b[31m\n"},
			`To run "printenv" "X\x1b[31m\n" with the values that "` + realHome + `/x\n\x1b[31m.env" refers to, enter the master password: `,
			`"` + realHome + `/x\n\x1b[31m.env" が参照する値を渡して "printenv" "X\x1b[31m\n" を実行します。マスターパスワード: `},
	} {
		for _, lang := range []struct{ code, want string }{{"en", tc.en}, {"ja", tc.ja}} {
			t.Run(tc.name+"_"+lang.code, func(t *testing.T) {
				r := kakusu(t, home, append([]string{"KAKUSU_LANG=" + lang.code}, tc.env...), nil, tc.args...)
				if strings.Contains(r.stderr, "\x1b") {
					t.Errorf("stderr %q contains a raw ESC", r.stderr)
				}
				if got := promptBeforeError(r.stderr); got != lang.want {
					t.Errorf("prompt %q; want %q (exit %d)", got, lang.want, r.code)
				}
			})
		}
	}
}

// link is a symlink to real/sub, so link/../x.env opens real/x.env. Cleaning
// the path to x.env would name another file, which does not exist here.
func TestPasswordPromptShowsEnvFileAsOpened(t *testing.T) {
	home := newVault(t)
	if err := os.MkdirAll(filepath.Join(home, "real", "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "real", "x.env"), []byte("X=kks://app/db_password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "sub"), filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	shown := `"` + realHome + `/link/../x.env"`
	for _, tc := range []struct {
		name string
		env  []string
		args []string
		want string
	}{
		{"export", nil, []string{"export", "--env", "link/../x.env"},
			`To print the values that ` + shown + ` refers to, enter the master password: `},
		{"run", []string{"KAKUSU_NO_AGENT=1"}, []string{"run", "--env", "link/../x.env", "--", "printenv", "X"},
			`To run "printenv" "X" with the values that ` + shown + ` refers to, enter the master password: `},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := kakusu(t, home, tc.env, nil, tc.args...)
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

// internal/crypto/testdata/secrets-v0.5.0.enc was written by kakusu v0.5.0;
// internal/crypto/crypto_test.go says how. Reading it from the vault path
// checks that the vault still splits the file as salt ‖ nonce ‖ ciphertext,
// as released versions wrote it.
func TestGetReadsVaultWrittenByV050(t *testing.T) {
	home, err := os.MkdirTemp("", "kk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	fixture, err := os.ReadFile(filepath.Join("internal", "crypto", "testdata", "secrets-v0.5.0.enc"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".kakusu")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.enc"), fixture, 0600); err != nil {
		t.Fatal(err)
	}

	// With the agent disabled, get starts no agent that would need stopping.
	r := mustSucceed(t, kakusu(t, home, []string{"KAKUSU_NO_AGENT=1"}, []string{"golden-fixture-pw"}, "get", "app/db_password"))
	if want := "dummy-db-pass\n"; r.stdout != want {
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

func TestEnvRefWithoutGroupUsesDefaultGroup(t *testing.T) {
	home := newVault(t)
	const value = "SKOPENAI"
	// set stores a ref without a group in the default group.
	mustSucceed(t, kakusu(t, home, nil, nil, "set", "openai_key", value))
	envFile := filepath.Join(home, "b.env")
	if err := os.WriteFile(envFile, []byte("OPENAI_API_KEY=kks://openai_key\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("run", func(t *testing.T) {
		// The command checks the value itself instead of printing it, so the
		// result does not depend on how run passes on the command's output.
		mustSucceed(t, kakusu(t, home, nil, nil, "run", "--env", envFile, "--",
			"sh", "-c", `test "$OPENAI_API_KEY" = `+value))
	})
	t.Run("export", func(t *testing.T) {
		r := mustSucceed(t, kakusu(t, home, nil, []string{testPassword}, "export", "--env", envFile))
		if want := "export OPENAI_API_KEY='" + value + "'\n"; r.stdout != want {
			t.Errorf("stdout %q; want %q", r.stdout, want)
		}
	})
}

func TestRunFailsOnMalformedAndUnresolvedRefs(t *testing.T) {
	home := newVault(t)
	envFile := filepath.Join(home, "bad.env")
	env := "# refs\nA=kks://app/missing\nB=kks://app/\n"
	if err := os.WriteFile(envFile, []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "ran")

	r := kakusu(t, home, nil, nil, "run", "--env", envFile, "--", "touch", marker)
	if r.code != 1 || r.stdout != "" {
		t.Errorf("exit %d, stdout %q; want exit 1 and empty stdout", r.code, r.stdout)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("the command ran (stat %s: %v)", marker, err)
	}
	want := "  line 2: A -> kks://app/missing\n" +
		"  line 3: B -> kks://app/ (malformed; write kks://group/key or kks://key without whitespace)\n"
	if !strings.Contains(r.stderr, want) {
		t.Errorf("stderr %q; want it to contain %q", r.stderr, want)
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

// wantExportRejected checks that export exited 1, printed nothing for the
// shell to eval, and ended its error with names quoted and joined by ", ".
func wantExportRejected(t *testing.T, r result, names ...string) {
	t.Helper()
	if r.code != 1 || r.stdout != "" {
		t.Errorf("exit %d, stdout %q; want exit 1 and empty stdout", r.code, r.stdout)
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	if want := strings.Join(quoted, ", ") + "\n"; !strings.HasSuffix(r.stderr, want) {
		t.Errorf("stderr %q; want it to end with %q", r.stderr, want)
	}
}

// TestExportEnvRejectsInvalidNames checks that export --env prints nothing
// when a name in the file is not a shell variable name. export writes names
// into the statements unquoted, so such a name could make eval run commands:
// https://github.com/HituziANDO/kakusu/issues/3
func TestExportEnvRejectsInvalidNames(t *testing.T) {
	home := newVault(t)
	envFile := filepath.Join(home, "d.env")
	names := []string{
		"A; echo INJECTED; B",
		"A B",
		"$(x)",
		"`x`",
		"", // the line "=v"
		"-f",
		"export FOO", // the line "export FOO=v"
		"db-password",
		"1A",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			// The valid line before it must not be printed either.
			if err := os.WriteFile(envFile, []byte("OK=v\n"+name+"=v\n"), 0600); err != nil {
				t.Fatal(err)
			}
			r := kakusu(t, home, nil, []string{testPassword}, "export", "--env", envFile)
			wantExportRejected(t, r, name)
		})
	}
	t.Run("all", func(t *testing.T) {
		var lines strings.Builder
		for _, name := range names {
			lines.WriteString(name + "=v\n")
		}
		if err := os.WriteFile(envFile, []byte(lines.String()), 0600); err != nil {
			t.Fatal(err)
		}
		r := kakusu(t, home, nil, []string{testPassword}, "export", "--env", envFile)
		wantExportRejected(t, r, slices.Sorted(slices.Values(names))...)
	})
}

// TestExportVaultRejectsInvalidKeys checks that export <group> and export
// <group/key> print nothing when a key is not a shell variable name. set
// stores any key, including one with a newline.
func TestExportVaultRejectsInvalidKeys(t *testing.T) {
	home := newVault(t)
	for _, tc := range []struct{ group, key string }{
		{"dash", "db-password"},
		{"newline", "A\necho INJECTED\nB"},
	} {
		t.Run(tc.group, func(t *testing.T) {
			// The valid key in the same group must not be printed either.
			mustSucceed(t, kakusu(t, home, nil, nil, "set", tc.group+"/OK", "v"))
			mustSucceed(t, kakusu(t, home, nil, nil, "set", tc.group+"/"+tc.key, "v"))
			for _, ref := range []string{tc.group, tc.group + "/" + tc.key} {
				r := kakusu(t, home, nil, []string{testPassword}, "export", ref)
				wantExportRejected(t, r, tc.key)
			}
		})
	}
}

func TestExportPrintsValidNames(t *testing.T) {
	home := newVault(t)
	envFile := filepath.Join(home, "d.env")
	if err := os.WriteFile(envFile, []byte("_A1=plain\nDB_PASSWORD=kks://app/db_password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dbPassword := "export db_password='" + secrets["app/db_password"] + "'\n"
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"env", []string{"export", "--env", envFile}, "export DB_PASSWORD='" + secrets["app/db_password"] + "'\nexport _A1='plain'\n"},
		{"group", []string{"export", "app"}, "export api_key='" + secrets["app/api_key"] + "'\n" + dbPassword},
		{"key", []string{"export", "app/db_password"}, dbPassword},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mustSucceed(t, kakusu(t, home, nil, []string{testPassword}, tc.args...))
			if r.stdout != tc.want {
				t.Errorf("stdout %q; want %q", r.stdout, tc.want)
			}
		})
	}
}
