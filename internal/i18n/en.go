package i18n

var catalogEN = messages{
	// Errors
	MsgErrMessageTooLarge:    "message too large",
	MsgErrAgentStartFailed:   "failed to start agent",
	MsgErrCiphertextTooShort: "ciphertext too short",
	MsgErrFileCorrupted:      "file is corrupted",
	MsgErrJSONParse:          "JSON parse error: %v",
	MsgErrPasswordWrong:      "wrong password (or corrupted vault)",
	MsgErrUnresolvedRefs:     "unresolved kks:// references:\n%s",
	MsgErrRefDetail:          "  line %d: %s -> kks://%s/%s",
	MsgErrSecretNotFound:     "kks://%s/%s not found",
	MsgErrGroupNotFound:      "group '%s' not found",
	MsgErrEnvFileNotFound:    ".env file not found: %s",
	MsgErrCommandNotFound:    "command not found: %s",
	MsgErrUnknownSubcommand:  "unknown subcommand: agent %s",
	MsgErrAgentNotRunning:    "agent is not running",
	MsgErrAgentConnFailed:    "cannot connect to agent",
	MsgErrUnknownCommand:     "unknown command '%s'",
	MsgErrVaultNotFound:      "vault not found. run 'kakusu init' first",

	// Usage
	MsgUsageSet:    "usage: kakusu set <group/key> [value]",
	MsgUsageGet:    "usage: kakusu get <group/key>",
	MsgUsageShow:   "usage: kakusu show <group/key>",
	MsgUsageDelete: "usage: kakusu delete <group/key>",
	MsgUsageRun:    "usage: kakusu run [--env FILE] -- <command> [args...]",
	MsgUsageAgent:  "usage: kakusu agent <stop|status>",
	MsgUsageHint:   "  run 'kakusu help' for a list of commands",

	// Prompts
	MsgPromptMasterPassword: "Master password: ",
	MsgPromptNewPassword:    "New master password: ",
	MsgPromptConfirm:        "Confirm: ",
	MsgPromptMinLength:      "must be at least 8 characters",
	MsgPromptMismatch:       "passwords do not match. please try again.",
	MsgPromptOverwrite:      "Existing vault found. Overwrite? [y/N]: ",
	MsgPromptSecretValue:    "%s/%s value: ",
	MsgPromptDeleteConfirm:  "Delete kks://%s/%s? [y/N]: ",

	// Status
	MsgCancelled:       "cancelled",
	MsgInitDone:        "initialized: %s",
	MsgSecretSaved:     "saved kks://%s/%s",
	MsgNoSecrets:       "(no secrets stored)",
	MsgSecretDeleted:   "deleted kks://%s/%s",
	MsgInjected:        "injected: %s",
	MsgPasswordChanged: "master password changed",
	MsgAgentStopped:    "agent stopped",
	MsgAgentStatus:     "Agent:     running (PID %s)",
	MsgAgentTTL:        "TTL:       %s",
	MsgKeyCachePresent: "Key cache: active (%s remaining)",
	MsgKeyCacheNone:    "Key cache: none",
	MsgKeyCacheCleared: "key cache cleared",

	// Help (full text)
	MsgHelp: `
kakusu - local secrets manager

[Basic]
  kakusu init                          Initialize vault (set master password)
  kakusu set <group/key> [value]       Store a secret (hidden input if value omitted)
  kakusu get <group/key>               Get value (stdout, pipeable)
  kakusu show <group/key>              Show the full value
  kakusu list [group]                  List secrets (values masked)
  kakusu delete <group/key>            Delete a secret
  kakusu passwd                        Change master password
  kakusu version                       Show version

[Key Cache (Agent)]
  kakusu lock                          Clear cached key immediately
  kakusu agent stop                    Stop the agent
  kakusu agent status                  Show agent status

[.env Integration]
  kakusu run [--env FILE] -- <cmd>     Resolve kks:// refs and run command
  kakusu export [--env FILE]           Output export statements for eval
  kakusu export <group>                Export all secrets in a group
  kakusu export <group/key>            Export a single secret

[.env File Format]
  DB_HOST=localhost
  DB_PASSWORD=kks://myproject/db_password
  OPENAI_API_KEY=kks://shared/openai_key

[Examples]
  kakusu set myproject/db_password
  kakusu set shared/openai_key sk-xxx...
  kakusu run -- python app.py
  kakusu run --env .env.staging -- python app.py
  eval "$(kakusu export)"
  eval "$(kakusu export myproject)"
  kakusu list
  kakusu list myproject

[Environment Variables]
  KAKUSU_FILE       Custom vault path (e.g. ~/Dropbox/kakusu/secrets.enc)
  KAKUSU_LANG       Output language (en, ja; default: en)
  KAKUSU_TTL        Key cache TTL (default: 30m, e.g. 1h, 45m)
  KAKUSU_NO_AGENT   Set to 1 to disable agent

[Security]
  Encryption : AES-256-GCM (authenticated encryption with tamper detection)
  Key deriv. : PBKDF2-HMAC-SHA256 (600,000 iterations)
  Key cache  : Agent process holds key in memory only (auto-start, auto-expire)
  Vault      : ~/.kakusu/secrets.enc (customizable via KAKUSU_FILE)
  Groups     : Single file, logically separated by group
`,
	MsgVersion: "kakusu version %s",
}
