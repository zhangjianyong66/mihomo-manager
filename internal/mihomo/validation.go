package mihomo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
)

const maxAdapterConfigBytes = 16 << 20

func readConfig(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var buffer bytes.Buffer
	n, err := io.Copy(&buffer, io.LimitReader(file, maxAdapterConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if n > maxAdapterConfigBytes {
		return nil, fmt.Errorf("config exceeds %d bytes", maxAdapterConfigBytes)
	}
	return buffer.Bytes(), nil
}

func validateNative(parent context.Context, binary string, spec core.RuntimeSpec, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-t", "-d", spec.ConfigDir, "-f", spec.ConfigPath)
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return core.ErrValidationTimeout
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("mihomo config validation exited with status %d: %w", exitErr.ExitCode(), core.ErrValidationFailed)
		}
		return fmt.Errorf("run mihomo config validation: %w", err)
	}
	return nil
}
