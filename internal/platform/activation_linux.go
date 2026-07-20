//go:build linux

package platform

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

func ActivatedListener() (net.Listener, bool, error) {
	pid, _ := strconv.Atoi(os.Getenv("LISTEN_PID"))
	fds, _ := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if pid == 0 && fds == 0 {
		return nil, false, nil
	}
	if pid != os.Getpid() || fds != 1 {
		return nil, false, fmt.Errorf("validate socket activation: %w", ErrUnsafePath)
	}
	file := os.NewFile(uintptr(3), "systemd-mm.socket")
	if file == nil {
		return nil, false, fmt.Errorf("open activated socket: %w", ErrUnsafePath)
	}
	listener, err := net.FileListener(file)
	_ = file.Close()
	if err != nil {
		return nil, false, fmt.Errorf("open activated socket: %w", err)
	}
	if _, ok := listener.Addr().(*net.UnixAddr); !ok {
		_ = listener.Close()
		return nil, false, fmt.Errorf("validate activated socket: %w", ErrUnsafePath)
	}
	return listener, true, nil
}
