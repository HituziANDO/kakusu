//go:build windows

package platform

import (
	"os"
	"os/exec"
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
