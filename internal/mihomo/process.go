package mihomo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

type managedProcess struct {
	cmd         *exec.Cmd
	done        chan error
	stopTimeout time.Duration
	stopOnce    sync.Once
	stopErr     error
}

func startProcess(binary string, spec core.RuntimeSpec, stopTimeout time.Duration) (*managedProcess, error) {
	if err := platform.EnsurePrivateDir(filepath.Dir(spec.LogPath)); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(spec.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open mihomo log: %w", err)
	}
	if err := logFile.Chmod(0o600); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("secure mihomo log: %w", err)
	}
	cmd := exec.Command(binary, "-d", spec.ConfigDir, "-f", spec.ConfigPath)
	configureProcess(cmd)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start mihomo: %w", err)
	}
	if err := logFile.Close(); err != nil {
		_ = killProcess(cmd)
		_ = cmd.Wait()
		return nil, fmt.Errorf("close mihomo log: %w", err)
	}
	process := &managedProcess{cmd: cmd, done: make(chan error, 1), stopTimeout: stopTimeout}
	go func() {
		process.done <- cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

func (p *managedProcess) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *managedProcess) Done() <-chan error { return p.done }

func (p *managedProcess) Stop(ctx context.Context) error {
	p.stopOnce.Do(func() { p.stopErr = p.stop(ctx) })
	return p.stopErr
}

func (p *managedProcess) stop(ctx context.Context) error {
	select {
	case <-p.done:
		return nil
	default:
	}
	if err := terminateProcess(p.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("terminate mihomo process: %w", err)
	}
	timer := time.NewTimer(p.stopTimeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		if err := killProcess(p.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return errors.Join(ctx.Err(), fmt.Errorf("kill mihomo process: %w", err))
		}
		return ctx.Err()
	case <-timer.C:
		if err := killProcess(p.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill mihomo process: %w", err)
		}
		select {
		case <-p.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
