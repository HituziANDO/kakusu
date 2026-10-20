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
	"io"
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
	status         syscall.WaitStatus
	stdout, stderr string
}

// syncBuffer lets the test read output while the child is still writing it.
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

// process is a started child whose output the test can read while it runs.
type process struct {
	cmd            *exec.Cmd
	stdout, stderr syncBuffer
	done           chan struct{}
	deadline       <-chan time.Time
}

// start starts name with args under home with extraEnv added. A nil stdin
// means /dev/null.
func start(t *testing.T, home string, extraEnv []string, stdin io.Reader, name string, args ...string) *process {
	t.Helper()
	p := &process{cmd: exec.Command(name, args...), done: make(chan struct{}), deadline: time.After(10 * time.Second)}
	p.cmd.Dir = home
	p.cmd.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "KAKUSU_LANG=en"}, extraEnv...)
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	p.cmd.Stdin = stdin
	p.cmd.Stdout = &p.stdout
	p.cmd.Stderr = &p.stderr
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		p.cmd.Wait()
		close(p.done)
	}()
	return p
}

// kill kills the child and the commands it started. The child leads its own
// session, so its process group ID is its PID.
func (p *process) kill() {
	syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// waitUntil waits until ok returns true and reports whether it did before the
// child exited.
func (p *process) waitUntil(t *testing.T, ok func() bool) bool {
	t.Helper()
	for !ok() {
		select {
		case <-p.done:
			return ok()
		case <-p.deadline:
			p.kill()
			t.Fatalf("%v: timed out; stdout %q; stderr %q", p.cmd.Args, p.stdout.String(), p.stderr.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	return true
}

func (p *process) wait(t *testing.T) result {
	t.Helper()
	select {
	case <-p.done:
	case <-p.deadline:
		p.kill()
		t.Fatalf("%v did not exit; stdout %q; stderr %q", p.cmd.Args, p.stdout.String(), p.stderr.String())
	}
	status, _ := p.cmd.ProcessState.Sys().(syscall.WaitStatus)
	return result{code: p.cmd.ProcessState.ExitCode(), status: status, stdout: p.stdout.String(), stderr: p.stderr.String()}
}

var promptRe = regexp.MustCompile(`(?:password|Confirm): `)

// kakusu runs the binary under home with extraEnv added. With no input, stdin
// is /dev/null. Otherwise each line is written only after the child prints one
// more password prompt: ui.PromptPassword creates a new bufio.Reader per call,
// so lines written together would be swallowed by the first prompt.
func kakusu(t *testing.T, home string, extraEnv, input []string, args ...string) result {
	t.Helper()
	var stdin io.Reader
	var w *os.File
	if len(input) > 0 {
		r, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		defer pw.Close()
		stdin, w = r, pw
	}
	p := start(t, home, extraEnv, stdin, kakusuBin, args...)
	for i, line := range input {
		if !p.waitUntil(t, func() bool { return len(promptRe.FindAllString(p.stderr.String(), -1)) > i }) {
			break
		}
		fmt.Fprintln(w, line)
	}
	if w != nil {
		w.Close()
	}
	return p.wait(t)
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

// writeEnvFile writes an env file with X=kks://app/db_password followed by
// extra lines.
func writeEnvFile(t *testing.T, home string, extra ...string) string {
	t.Helper()
	path := filepath.Join(home, "x.env")
	lines := append([]string{"X=kks://app/db_password"}, extra...)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
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

// placeholder replaces values in run's output that is not a terminal. The
// tests run kakusu with stdout and stderr as pipes.
const placeholder = "<concealed by kakusu>"

func TestRunUsesCache(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)

	// stdin is /dev/null, so run succeeds only by using the cache.
	r := mustSucceed(t, kakusu(t, home, nil, nil, "run", "--env", envFile, "--", "printenv", "X"))
	if want := placeholder + "\n"; r.stdout != want {
		t.Errorf("stdout %q; want %q", r.stdout, want)
	}
	if v := secrets["app/db_password"]; strings.Contains(r.stdout+r.stderr, v) {
		t.Errorf("stdout %q, stderr %q; want neither to contain %q", r.stdout, r.stderr, v)
	}
}

func TestRunConcealsStderrButNotLiteralValues(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home, "H=localhost", "P=5432")

	r := mustSucceed(t, kakusu(t, home, nil, nil, "run", "--env", envFile, "--",
		"sh", "-c", `echo "$X" >&2; echo "$H:$P"`))
	if want := "localhost:5432\n"; r.stdout != want {
		t.Errorf("stdout %q; want %q", r.stdout, want)
	}
	if v := secrets["app/db_password"]; !strings.Contains(r.stderr, placeholder+"\n") || strings.Contains(r.stderr, v) {
		t.Errorf("stderr %q; want %q in place of %q", r.stderr, placeholder, v)
	}
}

func TestRunConcealsValueSplitAcrossWrites(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)

	// The pause lets kakusu read the first half of the value on its own.
	r := mustSucceed(t, kakusu(t, home, nil, nil, "run", "--env", envFile, "--",
		"sh", "-c", `a=${X%?????}; printf %s "$a"; sleep 0.3; printf '%s\n' "${X#"$a"}"`))
	if want := placeholder + "\n"; r.stdout != want {
		t.Errorf("stdout %q; want %q", r.stdout, want)
	}
}

func TestRunPrintsPromptWhileCommandRuns(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	// The command waits for a line on stdin, so the prompt, which cannot be
	// the start of the value, reaches stdout only if kakusu does not hold it.
	p := start(t, home, nil, r, kakusuBin, "run", "--env", envFile, "--",
		"sh", "-c", `printf 'ready> '; read l; echo "got $l"`)
	if !p.waitUntil(t, func() bool { return p.stdout.String() == "ready> " }) {
		t.Fatalf("exited before the prompt; stdout %q; stderr %q", p.stdout.String(), p.stderr.String())
	}
	fmt.Fprintln(w, "ok")
	res := mustSucceed(t, p.wait(t))
	if want := "ready> got ok\n"; res.stdout != want {
		t.Errorf("stdout %q; want %q", res.stdout, want)
	}
}

func TestRunNoMaskingPrintsValue(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)

	r := mustSucceed(t, kakusu(t, home, nil, nil, "run", "--env", envFile, "--no-masking", "--", "printenv", "X"))
	if want := secrets["app/db_password"] + "\n"; r.stdout != want {
		t.Errorf("stdout %q; want %q", r.stdout, want)
	}
}

func TestRunExitsAsCommandDid(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)
	run := func(script string) result {
		return kakusu(t, home, nil, nil, "run", "--env", envFile, "--", "sh", "-c", script)
	}

	if r := run("exit 7"); r.code != 7 {
		t.Errorf("exit 7: kakusu exit %d; want 7", r.code)
	}
	// The caller sees the same wait status as when kakusu replaced itself with
	// the command.
	if r := run("kill -TERM $$"); !r.status.Signaled() || r.status.Signal() != syscall.SIGTERM {
		t.Errorf("killed by SIGTERM: kakusu exit %d, signaled %v by %v; want killed by SIGTERM",
			r.code, r.status.Signaled(), r.status.Signal())
	}
	// SIGKILL is outside SIGHUP, SIGINT, and SIGTERM.
	if r := run("kill -KILL $$"); r.status.Signaled() || r.code != 128+int(syscall.SIGKILL) {
		t.Errorf("killed by SIGKILL: kakusu exit %d, signaled %v by %v; want exit %d",
			r.code, r.status.Signaled(), r.status.Signal(), 128+int(syscall.SIGKILL))
	}
}

func TestRunForwardsSignals(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)
	// kakusu runs with no controlling terminal, so it forwards SIGINT too.
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			p := start(t, home, nil, nil, kakusuBin, "run", "--env", envFile, "--",
				"sh", "-c", `trap 'exit 42' TERM INT; echo ready; while :; do sleep 0.05; done`)
			if !p.waitUntil(t, func() bool { return p.stdout.String() == "ready\n" }) {
				t.Fatalf("exited before ready; stdout %q; stderr %q", p.stdout.String(), p.stderr.String())
			}
			p.cmd.Process.Signal(sig)
			if r := p.wait(t); r.code != 42 {
				t.Errorf("exit %d; want 42 from the trap; stderr %q", r.code, r.stderr)
			}
		})
	}
}

func TestRunKeepsIgnoredSignalsIgnored(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)
	for _, sig := range []string{"HUP", "INT"} {
		t.Run(sig, func(t *testing.T) {
			// As nohup does, sh ignores the signal and replaces itself with kakusu.
			p := start(t, home, nil, nil, "sh", "-c", `trap "" `+sig+`; exec "$0" "$@"`,
				kakusuBin, "run", "--env", envFile, "--", "sh", "-c", "kill -"+sig+` $$; echo survived`)
			if r := mustSucceed(t, p.wait(t)); r.stdout != "survived\n" {
				t.Errorf("stdout %q; want %q", r.stdout, "survived\n")
			}
		})
	}
}

func TestRunPassesStdin(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)

	t.Run("open_pipe", func(t *testing.T) {
		// stdin stays open with nothing written, so run must not wait for it.
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		defer w.Close()
		mustSucceed(t, start(t, home, nil, r, kakusuBin, "run", "--env", envFile, "--", "true").wait(t))
	})
	t.Run("cat", func(t *testing.T) {
		res := mustSucceed(t, start(t, home, nil, strings.NewReader("hello\n"), kakusuBin,
			"run", "--env", envFile, "--", "cat").wait(t))
		if res.stdout != "hello\n" {
			t.Errorf("stdout %q; want %q", res.stdout, "hello\n")
		}
	})
}

func TestRunKeepsOrderOfStdoutAndStderrInSameFile(t *testing.T) {
	home := newVault(t)
	envFile := writeEnvFile(t, home)
	const n = 200
	script := fmt.Sprintf(`i=0; while [ $i -lt %d ]; do echo "out $i"; echo "err $i" >&2; i=$((i+1)); done; echo "$X" >&2`, n)

	// sh makes kakusu's stderr the same pipe as its stdout, as 2>&1 does.
	r := mustSucceed(t, start(t, home, nil, nil, "sh", "-c", `exec "$0" "$@" 2>&1`,
		kakusuBin, "run", "--env", envFile, "--", "sh", "-c", script).wait(t))
	want := []string{"✓ injected: X"}
	for i := 0; i < n; i++ {
		want = append(want, fmt.Sprintf("out %d", i), fmt.Sprintf("err %d", i))
	}
	want = append(want, placeholder, "")
	got := strings.Split(r.stdout, "\n")
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("line %d of stdout differs; want %q; stdout %q", i+1, want[i], r.stdout)
		}
	}
	if len(got) != len(want) {
		t.Errorf("stdout has %d lines; want %d; stdout %q", len(got), len(want), r.stdout)
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
