package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ---------------------------------------------------------------------------
// .env パーサー（kks:// 参照を解決）
// ---------------------------------------------------------------------------

func resolveDotenv(envPath string, data kakusuData) (map[string]string, error) {
	f, err := os.Open(envPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]string)
	var missing []string
	lineno := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineno++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		envKey := strings.TrimSpace(line[:idx])
		rawVal := strings.TrimSpace(line[idx+1:])
		if len(rawVal) >= 2 {
			if (rawVal[0] == '"' && rawVal[len(rawVal)-1] == '"') ||
				(rawVal[0] == '\'' && rawVal[len(rawVal)-1] == '\'') {
				rawVal = rawVal[1 : len(rawVal)-1]
			}
		}

		m := kakusuRefRe.FindStringSubmatch(rawVal)
		if m != nil {
			group, key := m[1], m[2]
			secret, ok := getSecret(data, group, key)
			if !ok {
				missing = append(missing, i18nMsgf(MsgErrRefDetail, lineno, envKey, group, key))
			} else {
				result[envKey] = secret
			}
		} else {
			result[envKey] = rawVal
		}
	}

	if len(missing) > 0 {
		return nil, errors.New(i18nMsgf(MsgErrUnresolvedRefs, strings.Join(missing, "\n")))
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// パスワード入力ユーティリティ
// ---------------------------------------------------------------------------

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	tty, err := os.Open("/dev/tty")
	if err == nil {
		defer tty.Close()
		pw, err := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(pw), err
	}
	// /dev/tty が開けない場合（Windows等）: stdin で非表示入力を試みる
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(pw), err
	}
	// パイプ等の場合: フォールバック
	reader := bufio.NewReader(os.Stdin)
	pw, err := reader.ReadString('\n')
	return strings.TrimRight(pw, "\n"), err
}

func promptNewPassword() (string, error) {
	for {
		pw, err := promptPassword(i18nMsg(MsgPromptNewPassword))
		if err != nil {
			return "", err
		}
		if len(pw) < 8 {
			fmt.Fprintln(os.Stderr, i18nMsg(MsgPromptMinLength))
			continue
		}
		pw2, err := promptPassword(i18nMsg(MsgPromptConfirm))
		if err != nil {
			return "", err
		}
		if pw == pw2 {
			return pw, nil
		}
		fmt.Fprintln(os.Stderr, i18nMsg(MsgPromptMismatch))
	}
}

// ---------------------------------------------------------------------------
// UI ユーティリティ
// ---------------------------------------------------------------------------

func mask(v string) string {
	if len(v) > 6 {
		return v[:6] + "..."
	}
	return "***"
}

func die(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	os.Exit(1)
}

func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line)) == "y"
}
