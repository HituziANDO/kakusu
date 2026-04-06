package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// ルートコマンド
// ---------------------------------------------------------------------------

var rootCmd = &cobra.Command{
	Use:           "kakusu",
	Short:         "local secrets manager",
	Version:       version,
	SilenceErrors: true,
	SilenceUsage:  true,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(i18nMsg(MsgHelp))
	},
}

func init() {
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == rootCmd {
			fmt.Print(i18nMsg(MsgHelp))
		} else {
			defaultHelp(cmd, args)
		}
	})

	setCmd.Flags().SetInterspersed(false)
	runCmd.Flags().String("env", ".env", "env file path")
	runCmd.Flags().SetInterspersed(false)
	exportCmd.Flags().String("env", ".env", "env file path")

	agentCmd.AddCommand(agentStopCmd, agentStatusCmd)

	rootCmd.AddCommand(
		initCmd, setCmd, getCmd, showCmd, listCmd, deleteCmd,
		runCmd, exportCmd, passwdCmd, agentCmd, lockCmd, versionCmd,
	)
}

// ---------------------------------------------------------------------------
// init
// ---------------------------------------------------------------------------

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize vault (set master password)",
	Run: func(cmd *cobra.Command, args []string) {
		path := kakusuFile()
		if _, err := os.Stat(path); err == nil {
			if !confirm(i18nMsg(MsgPromptOverwrite)) {
				fmt.Fprintln(os.Stderr, i18nMsg(MsgCancelled))
				return
			}
		}
		pw, err := promptNewPassword()
		if err != nil {
			die(err.Error())
		}
		salt := make([]byte, saltSize)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			die(err.Error())
		}
		key := deriveKey(pw, salt)
		s := &kakusuState{data: make(kakusuData), key: key, salt: salt}
		if err := s.save(); err != nil {
			die(err.Error())
		}
		if !agentDisabled() {
			if err := ensureAgent(); err == nil {
				agentSetKey(key, salt)
			}
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18nMsgf(MsgInitDone, path))
	},
}

// ---------------------------------------------------------------------------
// set
// ---------------------------------------------------------------------------

var setCmd = &cobra.Command{
	Use:   "set <group/key> [value]",
	Short: "Store a secret (hidden input if value omitted)",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			die(i18nMsg(MsgUsageSet))
		}
		group, key := parseRef(args[0])

		var value string
		if len(args) >= 2 {
			value = strings.Join(args[1:], " ")
		} else {
			var err error
			value, err = promptPassword(i18nMsgf(MsgPromptSecretValue, group, key))
			if err != nil {
				die(err.Error())
			}
		}

		s, err := loadKakusu("", true)
		if err != nil {
			die(err.Error())
		}
		setSecret(s.data, group, key, value)
		if err := s.save(); err != nil {
			die(err.Error())
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18nMsgf(MsgSecretSaved, group, key))
	},
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

var getCmd = &cobra.Command{
	Use:   "get <group/key>",
	Short: "Get secret value (stdout, pipeable)",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			die(i18nMsg(MsgUsageGet))
		}
		group, key := parseRef(args[0])
		s, err := loadKakusu("", false)
		if err != nil {
			die(err.Error())
		}
		v, ok := getSecret(s.data, group, key)
		if !ok {
			die(i18nMsgf(MsgErrSecretNotFound, group, key))
		}
		fmt.Println(v)
	},
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

var showCmd = &cobra.Command{
	Use:   "show <group/key>",
	Short: "Show secret with label",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			die(i18nMsg(MsgUsageShow))
		}
		group, key := parseRef(args[0])
		s, err := loadKakusu("", false)
		if err != nil {
			die(err.Error())
		}
		v, ok := getSecret(s.data, group, key)
		if !ok {
			die(i18nMsgf(MsgErrSecretNotFound, group, key))
		}
		fmt.Printf("\nkks://%s/%s\n  %s\n\n", group, key, v)
	},
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

var listCmd = &cobra.Command{
	Use:   "list [group]",
	Short: "List secrets (values masked)",
	Run: func(cmd *cobra.Command, args []string) {
		s, err := loadKakusu("", false)
		if err != nil {
			die(err.Error())
		}
		if len(s.data) == 0 {
			fmt.Fprintln(os.Stderr, i18nMsg(MsgNoSecrets))
			return
		}

		filterGroup := ""
		if len(args) > 0 {
			filterGroup = args[0]
			if _, ok := s.data[filterGroup]; !ok {
				die(i18nMsgf(MsgErrGroupNotFound, filterGroup))
			}
		}

		groups := make([]string, 0)
		for g := range s.data {
			if filterGroup == "" || g == filterGroup {
				groups = append(groups, g)
			}
		}
		sort.Strings(groups)

		for _, g := range groups {
			fmt.Printf("\n[%s]\n", g)
			keys := make([]string, 0, len(s.data[g]))
			maxLen := 0
			for k := range s.data[g] {
				keys = append(keys, k)
				if len(k) > maxLen {
					maxLen = len(k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("  %-*s  %s\n", maxLen, k, mask(s.data[g][k]))
			}
		}
		fmt.Println()
	},
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

var deleteCmd = &cobra.Command{
	Use:     "delete <group/key>",
	Aliases: []string{"del"},
	Short:   "Delete a secret",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			die(i18nMsg(MsgUsageDelete))
		}
		group, key := parseRef(args[0])
		s, err := loadKakusu("", false)
		if err != nil {
			die(err.Error())
		}
		if _, ok := getSecret(s.data, group, key); !ok {
			die(i18nMsgf(MsgErrSecretNotFound, group, key))
		}
		if !confirm(i18nMsgf(MsgPromptDeleteConfirm, group, key)) {
			fmt.Fprintln(os.Stderr, i18nMsg(MsgCancelled))
			return
		}
		deleteSecret(s.data, group, key)
		if err := s.save(); err != nil {
			die(err.Error())
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18nMsgf(MsgSecretDeleted, group, key))
	},
}

// ---------------------------------------------------------------------------
// run
// ---------------------------------------------------------------------------

var runCmd = &cobra.Command{
	Use:   "run [--env FILE] -- <command> [args...]",
	Short: "Resolve kks:// refs and run command",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			die(i18nMsg(MsgUsageRun))
		}
		envFile, _ := cmd.Flags().GetString("env")

		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			die(i18nMsgf(MsgErrEnvFileNotFound, envFile))
		}

		s, err := loadKakusu("", false)
		if err != nil {
			die(err.Error())
		}
		injected, err := resolveDotenv(envFile, s.data)
		if err != nil {
			die(err.Error())
		}

		keys := make([]string, 0, len(injected))
		for k := range injected {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18nMsgf(MsgInjected, strings.Join(keys, ", ")))

		env := os.Environ()
		for k, v := range injected {
			env = append(env, k+"="+v)
		}

		bin, err := exec.LookPath(args[0])
		if err != nil {
			die(i18nMsgf(MsgErrCommandNotFound, args[0]))
		}
		if err := execRun(bin, args, env); err != nil {
			die(err.Error())
		}
	},
}

// ---------------------------------------------------------------------------
// export
// ---------------------------------------------------------------------------

var exportCmd = &cobra.Command{
	Use:   "export [--env FILE]",
	Short: "Output export statements for eval",
	Run: func(cmd *cobra.Command, args []string) {
		envFile, _ := cmd.Flags().GetString("env")

		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			die(i18nMsgf(MsgErrEnvFileNotFound, envFile))
		}

		s, err := loadKakusu("", false)
		if err != nil {
			die(err.Error())
		}
		injected, err := resolveDotenv(envFile, s.data)
		if err != nil {
			die(err.Error())
		}

		keys := make([]string, 0, len(injected))
		for k := range injected {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := strings.ReplaceAll(injected[k], "'", "'\"'\"'")
			fmt.Printf("export %s='%s'\n", k, v)
		}
	},
}

// ---------------------------------------------------------------------------
// passwd
// ---------------------------------------------------------------------------

var passwdCmd = &cobra.Command{
	Use:   "passwd",
	Short: "Change master password",
	Run: func(cmd *cobra.Command, args []string) {
		s, err := loadKakusu("", false)
		if err != nil {
			die(err.Error())
		}
		newPw, err := promptNewPassword()
		if err != nil {
			die(err.Error())
		}
		newSalt := make([]byte, saltSize)
		if _, err := io.ReadFull(rand.Reader, newSalt); err != nil {
			die(err.Error())
		}
		s.key = deriveKey(newPw, newSalt)
		s.salt = newSalt
		if err := s.save(); err != nil {
			die(err.Error())
		}
		if !agentDisabled() {
			agentClearKey()
			agentSetKey(s.key, s.salt)
		}
		fmt.Fprintln(os.Stderr, "✓ "+i18nMsg(MsgPasswordChanged))
	},
}

// ---------------------------------------------------------------------------
// agent（サブコマンドが未指定/不明の場合はエラー終了）
// ---------------------------------------------------------------------------

var agentCmd = &cobra.Command{
	Use:   "agent <stop|status>",
	Short: "Manage the key cache agent",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			die(i18nMsgf(MsgErrUnknownSubcommand, args[0]))
		}
		die(i18nMsg(MsgUsageAgent))
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the agent",
	Run: func(cmd *cobra.Command, args []string) {
		pidData, err := os.ReadFile(agentPIDPath())
		if err != nil {
			die(i18nMsg(MsgErrAgentNotRunning))
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil {
			os.Remove(agentPIDPath())
			os.Remove(agentSocketPath())
			die(i18nMsg(MsgErrAgentNotRunning))
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			os.Remove(agentPIDPath())
			os.Remove(agentSocketPath())
			die(i18nMsg(MsgErrAgentNotRunning))
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			os.Remove(agentPIDPath())
			os.Remove(agentSocketPath())
			die(i18nMsg(MsgErrAgentNotRunning))
		}
		// エージェントのクリーンアップを待つ
		for i := 0; i < 10; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, err := os.Stat(agentSocketPath()); os.IsNotExist(err) {
				break
			}
		}
		os.Remove(agentPIDPath())
		os.Remove(agentSocketPath())
		fmt.Fprintln(os.Stderr, "✓ "+i18nMsg(MsgAgentStopped))
	},
}

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show agent status",
	Run: func(cmd *cobra.Command, args []string) {
		resp, err := agentQueryStatus()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18nMsg(MsgErrAgentNotRunning))
			return
		}
		pidData, _ := os.ReadFile(agentPIDPath())
		pid := strings.TrimSpace(string(pidData))
		fmt.Fprintln(os.Stderr, i18nMsgf(MsgAgentStatus, pid))
		fmt.Fprintln(os.Stderr, i18nMsgf(MsgAgentTTL, time.Duration(resp.TTLSeconds)*time.Second))
		if resp.HasKey {
			remaining := time.Duration(resp.RemainingSeconds) * time.Second
			fmt.Fprintln(os.Stderr, i18nMsgf(MsgKeyCachePresent, remaining.Truncate(time.Second)))
		} else {
			fmt.Fprintln(os.Stderr, i18nMsg(MsgKeyCacheNone))
		}
	},
}

// ---------------------------------------------------------------------------
// lock
// ---------------------------------------------------------------------------

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Clear cached key immediately",
	Run: func(cmd *cobra.Command, args []string) {
		if err := agentClearKey(); err != nil {
			// エージェント未起動時はキャッシュなし → 成功扱い
		}
		fmt.Fprintln(os.Stderr, "✓ "+i18nMsg(MsgKeyCacheCleared))
	},
}

// ---------------------------------------------------------------------------
// version
// ---------------------------------------------------------------------------

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(i18nMsgf(MsgVersion, version))
	},
}
