//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func agentSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func ignoreHUP() {
	// SIGHUP は Windows に存在しない — no-op
}

func execRun(bin string, args []string, env []string) error {
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
