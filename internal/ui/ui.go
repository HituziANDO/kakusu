package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/HituziANDO/kakusu/internal/i18n"
)

// PromptPassword reads a password without echo from the controlling terminal
// and writes the prompt there, so that the prompt shows even when stderr is
// redirected. If the terminal opens only for reading, the prompt goes to
// stderr. It reads stdin only when /dev/tty does not open or is not a terminal
// (without a controlling terminal, and on Windows), and then writes the prompt
// to stderr.
func PromptPassword(prompt string) (string, error) {
	tty, out := openTTY()
	fmt.Fprint(out, prompt)
	if tty != nil {
		defer tty.Close()
		pw, err := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(out)
		return string(pw), err
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out)
		return string(pw), err
	}
	reader := bufio.NewReader(os.Stdin)
	pw, err := reader.ReadString('\n')
	return strings.TrimRight(pw, "\n"), err
}

// ttyPath is the controlling terminal. Tests point it at a regular file.
var ttyPath = "/dev/tty"

// openTTY opens the controlling terminal to read a password from, or returns
// nil when it does not open or is not a terminal. Writing to a file that is
// not a terminal would overwrite it: on Windows, /dev/tty is \dev\tty on the
// current drive, and a container can put a regular file at /dev/tty. out is
// where the prompt and the messages about the input go: the terminal when it
// opens for writing, stderr otherwise.
func openTTY() (*os.File, io.Writer) {
	tty, err := os.OpenFile(ttyPath, os.O_RDWR, 0)
	writable := err == nil
	if !writable {
		tty, err = os.Open(ttyPath)
	}
	if err != nil {
		return nil, os.Stderr
	}
	if !term.IsTerminal(int(tty.Fd())) {
		tty.Close()
		return nil, os.Stderr
	}
	if !writable {
		return tty, os.Stderr
	}
	return tty, tty
}

// printNotice writes msg as a line to where PromptPassword writes its prompt.
func printNotice(msg string) {
	tty, out := openTTY()
	if tty != nil {
		defer tty.Close()
	}
	fmt.Fprintln(out, msg)
}

func PromptNewPassword() (string, error) {
	for {
		pw, err := PromptPassword(i18n.Msg(i18n.MsgPromptNewPassword))
		if err != nil {
			return "", err
		}
		if len(pw) < 8 {
			printNotice(i18n.Msg(i18n.MsgPromptMinLength))
			continue
		}
		pw2, err := PromptPassword(i18n.Msg(i18n.MsgPromptConfirm))
		if err != nil {
			return "", err
		}
		if pw == pw2 {
			return pw, nil
		}
		printNotice(i18n.Msg(i18n.MsgPromptMismatch))
	}
}

func Die(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	os.Exit(1)
}

func Confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line)) == "y"
}
