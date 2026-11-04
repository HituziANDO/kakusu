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

// RunChild runs cmd. outputs are kakusu's copies of the pipe ends given to
// cmd; RunChild closes them once cmd has started. It returns after cmd has
// exited and drained is closed, which happens when every pipe has been read
// to its end. Until cmd exits, kakusu catches Ctrl+C without passing it on,
// because the console sends it to cmd too. After that, while a process that
// cmd started keeps a pipe open, Ctrl+C ends kakusu as it ends any console
// program that does not catch it.
func RunChild(cmd *exec.Cmd, outputs []*os.File, drained <-chan struct{}) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	err := cmd.Start()
	for _, f := range outputs {
		f.Close()
	}
	if err != nil {
		return err
	}
	err = cmd.Wait()
	signal.Stop(sigs)
	<-drained
	return err
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
