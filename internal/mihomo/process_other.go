//go:build !linux

package mihomo

import (
	"os"
	"os/exec"
)

func configureProcess(_ *exec.Cmd) {}

func terminateProcess(cmd *exec.Cmd) error { return cmd.Process.Signal(os.Interrupt) }

func killProcess(cmd *exec.Cmd) error { return cmd.Process.Kill() }
