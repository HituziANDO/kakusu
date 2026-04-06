package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Message key constants.
const (
	// Errors
	MsgErrMessageTooLarge    = "err.message_too_large"
	MsgErrAgentStartFailed   = "err.agent_start_failed"
	MsgErrCiphertextTooShort = "err.ciphertext_too_short"
	MsgErrFileCorrupted      = "err.file_corrupted"
	MsgErrJSONParse          = "err.json_parse"
	MsgErrPasswordWrong      = "err.password_wrong"
	MsgErrUnresolvedRefs     = "err.unresolved_refs"
	MsgErrRefDetail          = "err.ref_detail"
	MsgErrSecretNotFound     = "err.secret_not_found"
	MsgErrGroupNotFound      = "err.group_not_found"
	MsgErrEnvFileNotFound    = "err.env_file_not_found"
	MsgErrCommandNotFound    = "err.command_not_found"
	MsgErrUnknownSubcommand  = "err.unknown_subcommand"
	MsgErrAgentNotRunning    = "err.agent_not_running"
	MsgErrAgentConnFailed    = "err.agent_conn_failed"
	MsgErrUnknownCommand     = "err.unknown_command"
	MsgErrVaultNotFound      = "err.vault_not_found"

	// Usage
	MsgUsageSet    = "usage.set"
	MsgUsageGet    = "usage.get"
	MsgUsageShow   = "usage.show"
	MsgUsageDelete = "usage.delete"
	MsgUsageRun    = "usage.run"
	MsgUsageAgent  = "usage.agent"
	MsgUsageHint   = "usage.hint"

	// Prompts
	MsgPromptMasterPassword = "prompt.master_password"
	MsgPromptNewPassword    = "prompt.new_password"
	MsgPromptConfirm        = "prompt.confirm_password"
	MsgPromptMinLength      = "prompt.min_length"
	MsgPromptMismatch       = "prompt.mismatch"
	MsgPromptOverwrite      = "prompt.overwrite"
	MsgPromptSecretValue    = "prompt.secret_value"
	MsgPromptDeleteConfirm  = "prompt.delete_confirm"

	// Status
	MsgCancelled       = "status.cancelled"
	MsgInitDone        = "status.init_done"
	MsgSecretSaved     = "status.secret_saved"
	MsgNoSecrets       = "status.no_secrets"
	MsgSecretDeleted   = "status.secret_deleted"
	MsgInjected        = "status.injected"
	MsgPasswordChanged = "status.password_changed"
	MsgAgentStopped    = "status.agent_stopped"
	MsgAgentStatus     = "status.agent_status"
	MsgAgentTTL        = "status.agent_ttl"
	MsgKeyCachePresent = "status.key_cache_present"
	MsgKeyCacheNone    = "status.key_cache_none"
	MsgKeyCacheCleared = "status.key_cache_cleared"

	// Help / version
	MsgHelp    = "help.full"
	MsgVersion = "version"
)

type messages map[string]string

var catalogs = map[string]messages{
	"en": catalogEN,
	"ja": catalogJA,
}

var currentLang string

func InitLang() {
	currentLang = detectLang()
}

func detectLang() string {
	for _, key := range []string{"KAKUSU_LANG", "LC_MESSAGES", "LC_ALL", "LANG"} {
		if v := os.Getenv(key); v != "" {
			lang := strings.ToLower(v)
			if len(lang) >= 2 {
				code := lang[:2]
				if _, ok := catalogs[code]; ok {
					return code
				}
			}
		}
	}
	return "en"
}

// Msg returns a message without format arguments.
func Msg(key string) string {
	if m, ok := catalogs[currentLang][key]; ok {
		return m
	}
	if m, ok := catalogs["en"][key]; ok {
		return m
	}
	return key
}

// Msgf returns a formatted message.
func Msgf(key string, args ...any) string {
	return fmt.Sprintf(Msg(key), args...)
}
