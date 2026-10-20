//go:build windows

package platform

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func AgentSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func IgnoreHUP() {
	// SIGHUP does not exist on Windows.
}

func ExecRun(bin string, args []string, env []string) error {
	cmd := exec.Command(bin, args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	os.Exit(0)
	return nil
}

// RunChild runs cmd. kakusu catches Ctrl+C without passing it on, because the
// console sends it to cmd too, and so keeps relaying cmd's output until cmd
// exits.
func RunChild(cmd *exec.Cmd) error {
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	return cmd.Run()
}

// ExitWithStatus exits with the exit code of the command described by state.
func ExitWithStatus(state *os.ProcessState) {
	os.Exit(state.ExitCode())
}

// SameFile reports false: os.SameFile reports any two pipes as the same file
// on Windows, where Stat returns no file ID for pipes and consoles.
func SameFile(a, b *os.File) bool {
	return false
}
