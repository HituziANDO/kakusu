package vault

import (
	"bufio"
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/HituziANDO/kakusu/internal/i18n"
)

var RefRe = regexp.MustCompile(`^kks://([^/\s]+)/([^\s]+)$`)

// ResolveDotenv parses an env file, resolving kks:// references against the vault data.
func ResolveDotenv(envPath string, data Data) (map[string]string, error) {
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

		m := RefRe.FindStringSubmatch(rawVal)
		if m != nil {
			group, key := m[1], m[2]
			secret, ok := GetSecret(data, group, key)
			if !ok {
				missing = append(missing, i18n.Msgf(i18n.MsgErrRefDetail, lineno, envKey, group, key))
			} else {
				result[envKey] = secret
			}
		} else {
			result[envKey] = rawVal
		}
	}

	if len(missing) > 0 {
		return nil, errors.New(i18n.Msgf(i18n.MsgErrUnresolvedRefs, strings.Join(missing, "\n")))
	}
	return result, nil
}
