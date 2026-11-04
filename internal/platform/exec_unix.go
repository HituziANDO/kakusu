//go:build !windows

package platform

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func AgentSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func IgnoreHUP() {
	signal.Ignore(syscall.SIGHUP)
}

func ExecRun(bin string, args []string, env []string) error {
	return syscall.Exec(bin, args, env)
}

// ChildArgs returns the argument list for running bin, which was found for
// args[0], as a child: args as typed, as ExecRun passes them.
func ChildArgs(bin string, args []string) []string {
	return args
}

// forwardedSignals are the signals that RunChild passes on to the command.
// SIGTSTP and SIGWINCH are not caught: the command gets them itself from the
// terminal. RunChild catches SIGPIPE without passing it on.
var forwardedSignals = []os.Signal{
	syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2,
}

// RunChild runs cmd in kakusu's process group and passes on to it the signals
// that kakusu receives meanwhile, so that cmd gets them as it would if
// ExecRun had replaced kakusu with it, except that a signal sent to the whole
// process group reaches cmd twice, and SIGINT or SIGQUIT sent to kakusu alone
// while it is in the terminal's foreground does not reach cmd. outputs are
// kakusu's copies of the pipe ends given to cmd; RunChild closes them once cmd
// has started. It returns after cmd has exited and drained is closed, which
// happens when every pipe has been read to its end. A signal that kakusu
// receives after cmd has exited, while a process that cmd started keeps a
// pipe open, ends kakusu as ExitWithStatus would if that signal had killed
// cmd.
func RunChild(cmd *exec.Cmd, outputs []*os.File, drained <-chan struct{}) error {
	sigs := make(chan os.Signal, len(forwardedSignals))
	for _, sig := range forwardedSignals {
		// cmd starts with the default action for every signal kakusu
		// catches, so a signal that was ignored when kakusu started (SIGHUP
		// under nohup) stays ignored in cmd only if kakusu does not catch it.
		if !signal.Ignored(sig) {
			signal.Notify(sigs, sig)
		}
	}
	defer signal.Stop(sigs)
	// With SIGPIPE caught, a write to a closed stdout or stderr returns EPIPE
	// instead of killing kakusu, so that kakusu stays until cmd ends. cmd
	// still starts with the default action for SIGPIPE.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	err := cmd.Start()
	for _, f := range outputs {
		f.Close()
	}
	if err != nil {
		return err
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	running := true
	for running || drained != nil {
		select {
		case sig := <-sigs:
			if running && forward(cmd.Process, sig) {
				continue
			}
			exitBySignal(sig.(syscall.Signal))
		case err = <-waited:
			running = false
		case <-drained:
			drained = nil
		}
	}
	return err
}

// forward passes sig on to p and reports whether p may still be running.
func forward(p *os.Process, sig os.Signal) bool {
	// A terminal sends SIGINT and SIGQUIT to its whole foreground process
	// group, which includes p.
	if (sig == syscall.SIGINT || sig == syscall.SIGQUIT) && inForeground() {
		return true
	}
	return !errors.Is(p.Signal(sig), os.ErrProcessDone)
}

// inForeground reports whether kakusu is in the foreground process group of
// its controlling terminal. Without a controlling terminal it reports false.
func inForeground() bool {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	defer tty.Close()
	// TIOCGPGRP stores a 32-bit pid_t, which IoctlGetInt reads correctly on
	// the little-endian architectures that kakusu supports.
	pgrp, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	return err == nil && pgrp == syscall.Getpgrp()
}

// ExitWithStatus ends kakusu the way the command described by state ended:
// with its exit code, or as exitBySignal does with the signal that killed it.
func ExitWithStatus(state *os.ProcessState) {
	status := state.Sys().(syscall.WaitStatus)
	if !status.Signaled() {
		os.Exit(status.ExitStatus())
	}
	exitBySignal(status.Signal())
}

// exitBySignal ends kakusu by sig if it is SIGHUP, SIGINT, or SIGTERM and
// kakusu does not ignore it, and with exit code 128+N, as a shell reports a
// death by signal N, otherwise.
func exitBySignal(sig syscall.Signal) {
	switch sig {
	case syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM:
		// Go's default action for these three ends the process by the
		// signal. Sending kakusu other signals would not always end it that
		// way: Go dumps goroutines on SIGQUIT and ignores SIGUSR1.
		if !signal.Ignored(sig) {
			signal.Reset(sig)
			syscall.Kill(os.Getpid(), sig)
			// Delivery is asynchronous. Fall through only if kakusu survives.
			time.Sleep(time.Second)
		}
	}
	os.Exit(128 + int(sig))
}

// SameFile reports whether a and b refer to the same file, as stdout and
// stderr do after 2>&1.
func SameFile(a, b *os.File) bool {
	ai, err := a.Stat()
	if err != nil {
		return false
	}
	bi, err := b.Stat()
	return err == nil && os.SameFile(ai, bi)
}
