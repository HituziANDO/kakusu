package ui

import (
	"os"
	"path/filepath"
	"testing"
)

const ttyContent = "not a terminal\n"

// useFakeTTY points ttyPath at a regular file and replaces stdin and stderr
// with files, so that the test never opens the real terminal. It returns the
// paths of the fake tty and of the stderr file.
func useFakeTTY(t *testing.T, stdin string) (tty, stderr string) {
	t.Helper()
	dir := t.TempDir()
	tty = filepath.Join(dir, "tty")
	stderr = filepath.Join(dir, "stderr")
	stdinPath := filepath.Join(dir, "stdin")
	if err := os.WriteFile(tty, []byte(ttyContent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stdinPath, []byte(stdin), 0600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(stderr)
	if err != nil {
		t.Fatal(err)
	}
	oldTTY, oldStdin, oldStderr := ttyPath, os.Stdin, os.Stderr
	ttyPath, os.Stdin, os.Stderr = tty, in, errFile
	t.Cleanup(func() {
		ttyPath, os.Stdin, os.Stderr = oldTTY, oldStdin, oldStderr
		in.Close()
		errFile.Close()
	})
	return tty, stderr
}

func checkFakeTTYUnchanged(t *testing.T, tty, stderr, wantStderr string) {
	t.Helper()
	if b, err := os.ReadFile(tty); err != nil || string(b) != ttyContent {
		t.Errorf("fake tty %q (%v); want it unchanged %q", b, err, ttyContent)
	}
	if b, err := os.ReadFile(stderr); err != nil || string(b) != wantStderr {
		t.Errorf("stderr %q (%v); want %q", b, err, wantStderr)
	}
}

func TestPromptPasswordSkipsTTYPathThatIsNotATerminal(t *testing.T) {
	tty, stderr := useFakeTTY(t, "secret\n")
	if pw, err := PromptPassword("Prompt: "); pw != "secret" || err != nil {
		t.Errorf("PromptPassword = %q, %v; want %q from stdin", pw, err, "secret")
	}
	checkFakeTTYUnchanged(t, tty, stderr, "Prompt: ")
}

func TestPromptNewPasswordSkipsTTYPathThatIsNotATerminal(t *testing.T) {
	tty, stderr := useFakeTTY(t, "short\n")
	if _, err := PromptNewPassword(); err == nil {
		t.Error("PromptNewPassword succeeded; want an error at the end of stdin")
	}
	checkFakeTTYUnchanged(t, tty, stderr, "New master password: must be at least 8 characters\nNew master password: ")
}
