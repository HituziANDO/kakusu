package cli

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/HituziANDO/kakusu/internal/agent"
	"github.com/HituziANDO/kakusu/internal/conceal"
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
	runCmd.Flags().Bool("no-masking", false, "print values in output as is")
	runCmd.Flags().SetInterspersed(false)
	exportCmd.Flags().String("env", ".env", "env file path")

	agentCmd.AddCommand(agentStopCmd, agentStatusCmd)

	RootCmd.AddCommand(
		initCmd, setCmd, getCmd, showCmd, listCmd, deleteCmd,
		runCmd, exportCmd, passwdCmd, agentCmd, lockCmd, versionCmd,
	)
}

// loadVault is a convenience wrapper that exits on error. prompt is shown when
// the master password is needed.
func loadVault(allowCreate bool, prompt string) *vault.State {
	askPw := func() (string, error) { return ui.PromptPassword(prompt) }
	s, err := vault.LoadKakusu("", allowCreate, true, askPw, ui.PromptNewPassword)
	if err != nil {
		ui.Die(err.Error())
	}
	return s
}

// loadVaultAskingPassword is loadVault without the agent cache. Commands that
// print values or change the password use it, so that, while the key is cached,
// these commands print no value and change no password unless the user types
// the password.
func loadVaultAskingPassword(prompt string) *vault.State {
	askPw := func() (string, error) { return ui.PromptPassword(prompt) }
	s, err := vault.LoadKakusu("", false, false, askPw, ui.PromptNewPassword)
	if err != nil {
		ui.Die(err.Error())
	}
	return s
}

// vaultLock keeps the lock file open until the process exits. Without a
// reference to it, the garbage collector could close it and release the lock.
var vaultLock *os.File

// lockVault makes the commands that change the vault run one at a time on this
// device. It takes an exclusive lock on a file next to the vault, waiting while
// another process holds it, and keeps it until the process exits. The vault
// itself is not locked, because Save replaces it with a new file. The lock file
// is never deleted: a process could then lock a new file while another still
// held the old one. Call it once, before the command reads the vault.
// With allowCreate false, it creates no directory, and a missing one means
// that there is no vault.
func lockVault(allowCreate bool) {
	path := config.KakusuFile() + ".lock"
	if allowCreate {
		if err := os.MkdirAll(config.KakusuDir(), 0700); err != nil {
			ui.Die(err.Error())
		}
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if !allowCreate && os.IsNotExist(err) {
		ui.Die(i18n.Msg(i18n.MsgErrVaultNotFound))
	}
	if err != nil {
		ui.Die(err.Error())
	}
	locked, err := platform.TryLock(f)
	if err != nil {
		if !platform.LockUnsupported(err) {
			ui.Die(fmt.Sprintf("%s: %v", path, err))
		}
		fmt.Fprintln(os.Stderr, i18n.Msgf(i18n.MsgLockUnsupported, path, err))
	} else if !locked {
		fmt.Fprintln(os.Stderr, i18n.Msgf(i18n.MsgWaitingForLock, path))
		// Another process holds the lock, so this file can be locked: any
		// error here is a real failure, not a filesystem without locks.
		if err := platform.Lock(f); err != nil {
			ui.Die(fmt.Sprintf("%s: %v", path, err))
		}
	}
	vaultLock = f
}

// envFileForPrompt returns the env file path to show in the password prompt:
// absolute, but not cleaned, so that it names the file that is opened. Cleaning
// "link/../x.env" to "x.env" would name another file when link is a symlink.
func envFileForPrompt(envFile string) string {
	if filepath.IsAbs(envFile) {
		return envFile
	}
	wd, err := os.Getwd()
	if err != nil {
		ui.Die(err.Error())
	}
	return wd + string(filepath.Separator) + envFile
}

// ---------------------------------------------------------------------------
// init
// ---------------------------------------------------------------------------

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize vault (set master password)",
	Run: func(cmd *cobra.Command, args []string) {
		lockVault(true)
		path := config.KakusuFile()
		loaded, err := vault.ReadSnapshot()
		if err != nil {
			ui.Die(err.Error())
		}
		if loaded.Exists {
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
		key, err := crypto.DeriveKey(pw, salt)
		if err != nil {
			ui.Die(err.Error())
		}
		s := &vault.State{Data: make(vault.Data), Key: key, Salt: salt, Loaded: loaded}
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
			value, err = ui.PromptPassword(i18n.Msgf(i18n.MsgPromptSecretValue, group+"/"+key))
			if err != nil {
				ui.Die(err.Error())
			}
		}

		lockVault(true)
		s := loadVault(true, i18n.Msgf(i18n.MsgPromptPasswordToStore, group+"/"+key))
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
		s := loadVaultAskingPassword(i18n.Msgf(i18n.MsgPromptPasswordToPrint, group+"/"+key))
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
	Short: "Show the full value",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			ui.Die(i18n.Msg(i18n.MsgUsageShow))
		}
		group, key := vault.ParseRef(args[0])
		s := loadVaultAskingPassword(i18n.Msgf(i18n.MsgPromptPasswordToPrint, group+"/"+key))
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
		s := loadVault(false, i18n.Msg(i18n.MsgPromptPasswordToList))
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
				// No part of the value: list runs on the cached key without a password.
				fmt.Printf("  %-*s  ***\n", maxLen, k)
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
		lockVault(false)
		s := loadVault(false, i18n.Msgf(i18n.MsgPromptPasswordToDelete, group+"/"+key))
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
	Use:   "run [--env FILE] [--no-masking] -- <command> [args...]",
	Short: "Resolve kks:// refs and run command",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			ui.Die(i18n.Msg(i18n.MsgUsageRun))
		}
		envFile, _ := cmd.Flags().GetString("env")
		noMasking, _ := cmd.Flags().GetBool("no-masking")

		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			ui.Die(i18n.Msgf(i18n.MsgErrEnvFileNotFound, envFile))
		}

		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = strconv.Quote(a)
		}
		// Nothing below refers to s after the env file is resolved; only the
		// values from the env file are used while the command runs.
		s := loadVault(false, i18n.Msgf(i18n.MsgPromptPasswordToRun, strings.Join(quoted, " "), envFileForPrompt(envFile)))
		injected, secrets, err := vault.ResolveDotenv(envFile, s.Data)
		if err != nil {
			ui.Die(err.Error())
		}

		keys := make([]string, 0, len(injected))
		for k := range injected {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(os.Stderr, "✓ %s\n", i18n.Msgf(i18n.MsgInjected, strings.Join(keys, ", ")))

		// Drop the inherited variables that the env file sets. With both,
		// the value the command sees would depend on the mode: on Unix,
		// ExecRun passes both and getenv returns the first, while os/exec
		// keeps the last.
		var env []string
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if _, ok := injected[k]; !ok {
				env = append(env, kv)
			}
		}
		for k, v := range injected {
			env = append(env, k+"="+v)
		}

		bin, err := exec.LookPath(args[0])
		if err != nil {
			ui.Die(i18n.Msgf(i18n.MsgErrCommandNotFound, args[0]))
		}

		stdoutTerminal := term.IsTerminal(int(os.Stdout.Fd()))
		stderrTerminal := term.IsTerminal(int(os.Stderr.Fd()))
		mode := chooseRunMode(noMasking, stdoutTerminal, stderrTerminal, platform.SameFile(os.Stdout, os.Stderr))
		if mode == runExec {
			if err := platform.ExecRun(bin, args, env); err != nil {
				ui.Die(err.Error())
			}
			return
		}

		c := &exec.Cmd{Path: bin, Args: platform.ChildArgs(bin, args), Env: env, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		var relays []*relay
		pipe := func(out *os.File) *os.File {
			r, err := newRelay(out, secrets)
			if err != nil {
				ui.Die(err.Error())
			}
			relays = append(relays, r)
			return r.w
		}
		if mode == runConcealShared {
			w := pipe(os.Stdout)
			c.Stdout, c.Stderr = w, w
		} else {
			if !stdoutTerminal {
				c.Stdout = pipe(os.Stdout)
			}
			if !stderrTerminal {
				c.Stderr = pipe(os.Stderr)
			}
		}
		var wg sync.WaitGroup
		var outputs []*os.File
		for _, r := range relays {
			outputs = append(outputs, r.w)
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.copy()
			}()
		}
		drained := make(chan struct{})
		go func() {
			wg.Wait()
			close(drained)
		}()
		err = platform.RunChild(c, outputs, drained)
		if c.ProcessState == nil {
			ui.Die(err.Error())
		}
		// Output that could not be written for a reason other than a reader
		// that has gone makes run fail with that error, whatever the command
		// did: once its pipe is closed, the command's next write fails by
		// SIGPIPE, which would hide the cause. Otherwise a command that
		// failed decides the exit status, and a command that succeeded but
		// lost its reader makes run exit 128+SIGPIPE, as a shell reports a
		// death by SIGPIPE.
		for _, r := range relays {
			if r.err != nil && !errors.Is(r.err, syscall.EPIPE) {
				ui.Die(r.err.Error())
			}
		}
		if c.ProcessState.Success() {
			for _, r := range relays {
				if errors.Is(r.err, syscall.EPIPE) {
					os.Exit(128 + int(syscall.SIGPIPE))
				}
			}
		}
		platform.ExitWithStatus(c.ProcessState)
	},
}

// runMode is how run connects the command's stdout and stderr.
type runMode int

const (
	// runExec runs the command through platform.ExecRun, which gives it
	// kakusu's stdout and stderr, so nothing is concealed.
	runExec runMode = iota
	// runConceal passes a stream that is a terminal to the command as is and
	// conceals the values in each other stream through its own pipe.
	runConceal
	// runConcealShared conceals stdout and stderr through one pipe. They are
	// the same file (2>&1), and one pipe keeps the order of the writes.
	runConcealShared
)

// relay copies what the command writes to the pipe end w into out with the
// values concealed. The command gets w as an *os.File, so that waiting for
// the command does not wait for a process it started that keeps w open.
type relay struct {
	r, w *os.File
	cw   *conceal.Writer
	err  error // why copy stopped before the end of the output, or nil
}

func newRelay(out io.Writer, secrets []string) (*relay, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &relay{r: r, w: w, cw: conceal.NewWriter(out, secrets)}, nil
}

// copy copies until every process holding w has closed it, and then writes
// the bytes that the conceal.Writer holds. If writing to out fails, it stops
// and closes r, so that the command's next write fails by EPIPE or SIGPIPE,
// whatever error writing to out returned.
func (r *relay) copy() {
	_, r.err = io.Copy(r.cw, r.r)
	if r.err == nil {
		r.err = r.cw.Flush()
	}
	r.r.Close()
}

// chooseRunMode conceals output unless noMasking is set or both stdout and
// stderr are terminals.
func chooseRunMode(noMasking, stdoutTerminal, stderrTerminal, sameFile bool) runMode {
	switch {
	case noMasking || stdoutTerminal && stderrTerminal:
		return runExec
	case sameFile:
		return runConcealShared
	default:
		return runConceal
	}
}

// ---------------------------------------------------------------------------
// export
// ---------------------------------------------------------------------------

var exportCmd = &cobra.Command{
	Use:   "export [--env FILE] [group[/key]]",
	Short: "Output export statements for eval",
	Run: func(cmd *cobra.Command, args []string) {
		// If a group or group/key argument is given, export directly from vault.
		if len(args) > 0 {
			result := make(map[string]string)

			if strings.Contains(args[0], "/") {
				// group/key form — export a single secret.
				group, key := vault.ParseRef(args[0])
				s := loadVaultAskingPassword(i18n.Msgf(i18n.MsgPromptPasswordToPrint, group+"/"+key))
				v, ok := vault.GetSecret(s.Data, group, key)
				if !ok {
					ui.Die(i18n.Msgf(i18n.MsgErrSecretNotFound, group, key))
				}
				result[key] = v
			} else {
				// group only — export all keys in the group.
				group := args[0]
				s := loadVaultAskingPassword(i18n.Msgf(i18n.MsgPromptPasswordToPrintGroup, group))
				g, ok := s.Data[group]
				if !ok {
					ui.Die(i18n.Msgf(i18n.MsgErrGroupNotFound, group))
				}
				for k, v := range g {
					result[k] = v
				}
			}

			printExports(result)
			return
		}

		// No positional args — resolve a .env file.
		envFile, _ := cmd.Flags().GetString("env")
		envFlagChanged := cmd.Flags().Changed("env")

		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			if envFlagChanged {
				// User explicitly specified --env but file not found.
				ui.Die(i18n.Msgf(i18n.MsgErrEnvFileNotFound, envFile))
			}
			// No args, no .env file — show help.
			cmd.Help()
			return
		}

		s := loadVaultAskingPassword(i18n.Msgf(i18n.MsgPromptPasswordToPrintEnv, envFileForPrompt(envFile)))
		injected, _, err := vault.ResolveDotenv(envFile, s.Data)
		if err != nil {
			ui.Die(err.Error())
		}

		printExports(injected)
	},
}

// exportNameRe matches a POSIX shell variable name. printExports writes names
// into shell statements unquoted, so any other character could end the
// statement and start another one.
var exportNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// printExports prints nothing unless every name matches exportNameRe.
func printExports(m map[string]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var invalid []string
	for _, k := range keys {
		if !exportNameRe.MatchString(k) {
			// Quoted: a name can hold newlines and terminal escape sequences.
			invalid = append(invalid, strconv.Quote(k))
		}
	}
	if len(invalid) > 0 {
		ui.Die(i18n.Msgf(i18n.MsgErrInvalidExportNames, strings.Join(invalid, ", ")))
	}
	for _, k := range keys {
		v := strings.ReplaceAll(m[k], "'", "'\"'\"'")
		fmt.Printf("export %s='%s'\n", k, v)
	}
}

// ---------------------------------------------------------------------------
// passwd
// ---------------------------------------------------------------------------

var passwdCmd = &cobra.Command{
	Use:   "passwd",
	Short: "Change master password",
	Run: func(cmd *cobra.Command, args []string) {
		lockVault(false)
		s := loadVaultAskingPassword(i18n.Msg(i18n.MsgPromptCurrentPassword))
		newPw, err := ui.PromptNewPassword()
		if err != nil {
			ui.Die(err.Error())
		}
		newSalt := make([]byte, crypto.SaltSize)
		if _, err := io.ReadFull(rand.Reader, newSalt); err != nil {
			ui.Die(err.Error())
		}
		s.Key, err = crypto.DeriveKey(newPw, newSalt)
		if err != nil {
			ui.Die(err.Error())
		}
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
