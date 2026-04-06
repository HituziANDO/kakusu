//go:build !windows

package platform

import (
	"os/signal"
	"syscall"
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
