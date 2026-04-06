package cli

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

	"github.com/HituziANDO/kakusu/internal/agent"
	"github.com/HituziANDO/kakusu/internal/config"
	"github.com/HituziANDO/kakusu/internal/crypto"
	"github.com/HituziANDO/kakusu/internal/i18n"
	"github.com/HituziANDO/kakusu/internal/platform"
	"github.com/HituziANDO/kakusu/internal/ui"
	"github.com/HituziANDO/kakusu/internal/vault"
)

// RootCmd is the top-level cobra command.
var RootCmd = &cobra.Command{
	Use:           "kakusu",
	Short:         "local secrets manager",
	Version:       config.Version,
	SilenceErrors: true,
	SilenceUsage:  true,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(i18n.Msg(i18n.MsgHelp))
	},
}

func init() {
	defaultHelp := RootCmd.HelpFunc()
	RootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == RootCmd {
			fmt.Print(i18n.Msg(i18n.MsgHelp))
		} else {
			defaultHelp(cmd, args)
		}
	})

	setCmd.Flags().SetInterspersed(false)
	runCmd.Flags().String("env", ".env", "env file path")
	runCmd.Flags().SetInterspersed(false)
	exportCmd.Flags().String("env", ".env", "env file path")

	agentCmd.AddCommand(agentStopCmd, agentStatusCmd)

	RootCmd.AddCommand(
		initCmd, setCmd, getCmd, showCmd, listCmd, deleteCmd,
		runCmd, exportCmd, passwdCmd, agentCmd, lockCmd, versionCmd,
	)
}

// loadVault is a convenience wrapper that exits on error.
func loadVault(allowCreate bool) *vault.State {
	s, err := vault.LoadKakusu("", allowCreate, ui.PromptPassword, ui.PromptNewPassword)
	if err != nil {
		ui.Die(err.Error())
	}
	return s
}

// ---------------------------------------------------------------------------
// init
// ---------------------------------------------------------------------------

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize vault (set master password)",
	Run: func(cmd *cobra.Command, args []string) {
		path := config.KakusuFile()
		if _, err := os.Stat(path); err == nil {
			if !ui.Confirm(i18n.Msg(i18n.MsgPromptOverwrite)) {
				fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgCancelled))
				return
			}
		}
		pw, err := ui.PromptNewPassword()
		if err != nil {
			ui.Die(err.Error())
		}
		salt := make([]byte, crypto.SaltSize)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			ui.Die(err.Error())
		}
		key := crypto.DeriveKey(pw, salt)
		s := &vault.State{Data: make(vault.Data), Key: key, Salt: salt}
		if err := s.Save(); err != nil {
			ui.Die(err.Error())
		}
		if !config.AgentDisabled() {
			if err := agent.EnsureAgent(); err == nil {
				agent.SetKey(key, salt)
			}
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18n.Msgf(i18n.MsgInitDone, path))
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
			ui.Die(i18n.Msg(i18n.MsgUsageSet))
		}
		group, key := vault.ParseRef(args[0])

		var value string
		if len(args) >= 2 {
			value = strings.Join(args[1:], " ")
		} else {
			var err error
			value, err = ui.PromptPassword(i18n.Msgf(i18n.MsgPromptSecretValue, group, key))
			if err != nil {
				ui.Die(err.Error())
			}
		}

		s := loadVault(true)
		vault.SetSecret(s.Data, group, key, value)
		if err := s.Save(); err != nil {
			ui.Die(err.Error())
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18n.Msgf(i18n.MsgSecretSaved, group, key))
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
			ui.Die(i18n.Msg(i18n.MsgUsageGet))
		}
		group, key := vault.ParseRef(args[0])
		s := loadVault(false)
		v, ok := vault.GetSecret(s.Data, group, key)
		if !ok {
			ui.Die(i18n.Msgf(i18n.MsgErrSecretNotFound, group, key))
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
			ui.Die(i18n.Msg(i18n.MsgUsageShow))
		}
		group, key := vault.ParseRef(args[0])
		s := loadVault(false)
		v, ok := vault.GetSecret(s.Data, group, key)
		if !ok {
			ui.Die(i18n.Msgf(i18n.MsgErrSecretNotFound, group, key))
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
		s := loadVault(false)
		if len(s.Data) == 0 {
			fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgNoSecrets))
			return
		}

		filterGroup := ""
		if len(args) > 0 {
			filterGroup = args[0]
			if _, ok := s.Data[filterGroup]; !ok {
				ui.Die(i18n.Msgf(i18n.MsgErrGroupNotFound, filterGroup))
			}
		}

		groups := make([]string, 0)
		for g := range s.Data {
			if filterGroup == "" || g == filterGroup {
				groups = append(groups, g)
			}
		}
		sort.Strings(groups)

		for _, g := range groups {
			fmt.Printf("\n[%s]\n", g)
			keys := make([]string, 0, len(s.Data[g]))
			maxLen := 0
			for k := range s.Data[g] {
				keys = append(keys, k)
				if len(k) > maxLen {
					maxLen = len(k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("  %-*s  %s\n", maxLen, k, ui.Mask(s.Data[g][k]))
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
			ui.Die(i18n.Msg(i18n.MsgUsageDelete))
		}
		group, key := vault.ParseRef(args[0])
		s := loadVault(false)
		if _, ok := vault.GetSecret(s.Data, group, key); !ok {
			ui.Die(i18n.Msgf(i18n.MsgErrSecretNotFound, group, key))
		}
		if !ui.Confirm(i18n.Msgf(i18n.MsgPromptDeleteConfirm, group, key)) {
			fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgCancelled))
			return
		}
		vault.DeleteSecret(s.Data, group, key)
		if err := s.Save(); err != nil {
			ui.Die(err.Error())
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18n.Msgf(i18n.MsgSecretDeleted, group, key))
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
			ui.Die(i18n.Msg(i18n.MsgUsageRun))
		}
		envFile, _ := cmd.Flags().GetString("env")

		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			ui.Die(i18n.Msgf(i18n.MsgErrEnvFileNotFound, envFile))
		}

		s := loadVault(false)
		injected, err := vault.ResolveDotenv(envFile, s.Data)
		if err != nil {
			ui.Die(err.Error())
		}

		keys := make([]string, 0, len(injected))
		for k := range injected {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18n.Msgf(i18n.MsgInjected, strings.Join(keys, ", ")))

		env := os.Environ()
		for k, v := range injected {
			env = append(env, k+"="+v)
		}

		bin, err := exec.LookPath(args[0])
		if err != nil {
			ui.Die(i18n.Msgf(i18n.MsgErrCommandNotFound, args[0]))
		}
		if err := platform.ExecRun(bin, args, env); err != nil {
			ui.Die(err.Error())
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
			ui.Die(i18n.Msgf(i18n.MsgErrEnvFileNotFound, envFile))
		}

		s := loadVault(false)
		injected, err := vault.ResolveDotenv(envFile, s.Data)
		if err != nil {
			ui.Die(err.Error())
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
		s := loadVault(false)
		newPw, err := ui.PromptNewPassword()
		if err != nil {
			ui.Die(err.Error())
		}
		newSalt := make([]byte, crypto.SaltSize)
		if _, err := io.ReadFull(rand.Reader, newSalt); err != nil {
			ui.Die(err.Error())
		}
		s.Key = crypto.DeriveKey(newPw, newSalt)
		s.Salt = newSalt
		if err := s.Save(); err != nil {
			ui.Die(err.Error())
		}
		if !config.AgentDisabled() {
			agent.ClearKey()
			agent.SetKey(s.Key, s.Salt)
		}
		fmt.Fprintln(os.Stderr, "✓ "+i18n.Msg(i18n.MsgPasswordChanged))
	},
}

// ---------------------------------------------------------------------------
// agent
// ---------------------------------------------------------------------------

var agentCmd = &cobra.Command{
	Use:   "agent <stop|status>",
	Short: "Manage the key cache agent",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			ui.Die(i18n.Msgf(i18n.MsgErrUnknownSubcommand, args[0]))
		}
		ui.Die(i18n.Msg(i18n.MsgUsageAgent))
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the agent",
	Run: func(cmd *cobra.Command, args []string) {
		pidData, err := os.ReadFile(config.AgentPIDPath())
		if err != nil {
			ui.Die(i18n.Msg(i18n.MsgErrAgentNotRunning))
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil {
			os.Remove(config.AgentPIDPath())
			os.Remove(config.AgentSocketPath())
			ui.Die(i18n.Msg(i18n.MsgErrAgentNotRunning))
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			os.Remove(config.AgentPIDPath())
			os.Remove(config.AgentSocketPath())
			ui.Die(i18n.Msg(i18n.MsgErrAgentNotRunning))
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			os.Remove(config.AgentPIDPath())
			os.Remove(config.AgentSocketPath())
			ui.Die(i18n.Msg(i18n.MsgErrAgentNotRunning))
		}
		for i := 0; i < 10; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, err := os.Stat(config.AgentSocketPath()); os.IsNotExist(err) {
				break
			}
		}
		os.Remove(config.AgentPIDPath())
		os.Remove(config.AgentSocketPath())
		fmt.Fprintln(os.Stderr, "✓ "+i18n.Msg(i18n.MsgAgentStopped))
	},
}

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show agent status",
	Run: func(cmd *cobra.Command, args []string) {
		resp, err := agent.QueryStatus()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgErrAgentNotRunning))
			return
		}
		pidData, _ := os.ReadFile(config.AgentPIDPath())
		pid := strings.TrimSpace(string(pidData))
		fmt.Fprintln(os.Stderr, i18n.Msgf(i18n.MsgAgentStatus, pid))
		fmt.Fprintln(os.Stderr, i18n.Msgf(i18n.MsgAgentTTL, time.Duration(resp.TTLSeconds)*time.Second))
		if resp.HasKey {
			remaining := time.Duration(resp.RemainingSeconds) * time.Second
			fmt.Fprintln(os.Stderr, i18n.Msgf(i18n.MsgKeyCachePresent, remaining.Truncate(time.Second)))
		} else {
			fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgKeyCacheNone))
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
		if err := agent.ClearKey(); err != nil {
			// Agent not running — no cache to clear — success.
		}
		fmt.Fprintln(os.Stderr, "✓ "+i18n.Msg(i18n.MsgKeyCacheCleared))
	},
}

// ---------------------------------------------------------------------------
// version
// ---------------------------------------------------------------------------

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(i18n.Msgf(i18n.MsgVersion, config.Version))
	},
}
