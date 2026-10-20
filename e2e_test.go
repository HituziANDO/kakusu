//go:build !windows

package main

// End-to-end tests: they build the real binary and run its subcommands under a
// temporary HOME. Each child gets only the environment set here, so a
// KAKUSU_FILE in the caller's shell never points it at a real vault, and runs
// in a new session, so it has no controlling terminal and reads passwords from
// stdin.

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
			if !strings.Contains(r.stderr, "Master password: ") {
				t.Errorf("stderr %q; want the master password prompt", r.stderr)
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
