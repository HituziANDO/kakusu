//go:build !windows

package platform

import (
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

// forwardedSignals are the signals that RunChild passes on to the command.
// SIGPIPE, SIGTSTP, and SIGWINCH are not caught: the command gets them
// itself, from its own pipe or from the terminal.
var forwardedSignals = []os.Signal{
	syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2,
}

// RunChild runs cmd in kakusu's process group and passes on to it the signals
// that kakusu receives meanwhile, so that cmd gets them as it would if
// ExecRun had replaced kakusu with it.
func RunChild(cmd *exec.Cmd) error {
	sigs := make(chan os.Signal, len(forwardedSignals))
	for _, sig := range forwardedSignals {
		// cmd starts with the default action for every signal kakusu
		// catches, so a signal that was ignored when kakusu started (SIGHUP
		// under nohup) stays ignored in cmd only if kakusu does not catch it.
		if !signal.Ignored(sig) {
			signal.Notify(sigs, sig)
		}
	}
	if err := cmd.Start(); err != nil {
		signal.Stop(sigs)
		return err
	}
	go func() {
		for sig := range sigs {
			// A terminal sends SIGINT and SIGQUIT to its whole foreground
			// process group, which includes cmd.
			if (sig == syscall.SIGINT || sig == syscall.SIGQUIT) && inForeground() {
				continue
			}
			cmd.Process.Signal(sig)
		}
	}()
	err := cmd.Wait()
	signal.Stop(sigs)
	close(sigs)
	return err
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
// with its exit code, by the same signal if SIGHUP, SIGINT, or SIGTERM killed
// it, and with exit code 128+N, as a shell reports it, if another signal N
// killed it.
func ExitWithStatus(state *os.ProcessState) {
	status := state.Sys().(syscall.WaitStatus)
	if !status.Signaled() {
		os.Exit(status.ExitStatus())
	}
	sig := status.Signal()
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
