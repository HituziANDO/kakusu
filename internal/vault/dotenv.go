package vault

import (
	"bufio"
	"errors"
	"os"
	"strings"

	"github.com/HituziANDO/kakusu/internal/i18n"
)

// ResolveDotenv parses an env file, resolving kks:// references against the vault data.
// A value that starts with kks:// is a reference: kks://group/key, or kks://key
// for the default group. A reference with an empty group or key, with
// whitespace, or to a missing secret is an error.
// It returns every variable, and separately the values resolved from kks://
// references.
func ResolveDotenv(envPath string, data Data) (map[string]string, []string, error) {
	f, err := os.Open(envPath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	result := make(map[string]string)
	var secrets []string
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

		if strings.HasPrefix(rawVal, "kks://") {
			group, key := ParseRef(rawVal)
			// Whitespace means the characters \s matches in Go regexp.
			if group == "" || key == "" || strings.ContainsAny(rawVal, " \t\n\f\r") {
				missing = append(missing, i18n.Msgf(i18n.MsgErrRefMalformed, lineno, envKey, rawVal))
				continue
			}
			secret, ok := GetSecret(data, group, key)
			if !ok {
				missing = append(missing, i18n.Msgf(i18n.MsgErrRefDetail, lineno, envKey, group, key))
			} else {
				result[envKey] = secret
				secrets = append(secrets, secret)
			}
		} else {
			result[envKey] = rawVal
		}
	}

	if len(missing) > 0 {
		return nil, nil, errors.New(i18n.Msgf(i18n.MsgErrUnresolvedRefs, strings.Join(missing, "\n")))
	}
	return result, secrets, nil
}
