//go:build linux

package mihomo

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func terminateProcess(cmd *exec.Cmd) error { return cmd.Process.Signal(syscall.SIGTERM) }

func killProcess(cmd *exec.Cmd) error { return cmd.Process.Kill() }
