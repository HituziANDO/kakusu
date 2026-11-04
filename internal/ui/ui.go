package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/HituziANDO/kakusu/internal/i18n"
)

// PromptPassword reads a line without echo from the controlling terminal and
// writes the prompt there, so that the prompt shows even when stderr is
// redirected. Without a controlling terminal (and on Windows, which has no
// /dev/tty) it writes the prompt to stderr and reads stdin.
func PromptPassword(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		defer tty.Close()
		fmt.Fprint(tty, prompt)
		pw, err := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(tty)
		return string(pw), err
	}
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(pw), err
	}
	reader := bufio.NewReader(os.Stdin)
	pw, err := reader.ReadString('\n')
	return strings.TrimRight(pw, "\n"), err
}

func PromptNewPassword() (string, error) {
	for {
		pw, err := PromptPassword(i18n.Msg(i18n.MsgPromptNewPassword))
		if err != nil {
			return "", err
		}
		if len(pw) < 8 {
			fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgPromptMinLength))
			continue
		}
		pw2, err := PromptPassword(i18n.Msg(i18n.MsgPromptConfirm))
		if err != nil {
			return "", err
		}
		if pw == pw2 {
			return pw, nil
		}
		fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgPromptMismatch))
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
