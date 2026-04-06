//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

func agentSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func ignoreHUP() {
	signal.Ignore(syscall.SIGHUP)
}

func execRun(bin string, args []string, env []string) error {
	return syscall.Exec(bin, args, env)
}
