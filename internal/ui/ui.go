package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/HituziANDO/kakusu/internal/i18n"
)

func PromptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	tty, err := os.Open("/dev/tty")
	if err == nil {
		defer tty.Close()
		pw, err := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(pw), err
	}
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

func Mask(v string) string {
	if len(v) > 6 {
		return v[:6] + "..."
	}
	return "***"
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
