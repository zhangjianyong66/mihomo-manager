//go:build darwin

package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func ListenUnix(path string, expectedUID uint32) (net.Listener, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("listen unix socket: %w", ErrUnsafePath)
	}
	if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := prepareSocketPath(path, expectedUID); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen unix socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("secure unix socket: %w", err)
	}
	return &peerListener{Listener: listener, expectedUID: expectedUID}, nil
}

func prepareSocketPath(path string, expectedUID uint32) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect unix socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("inspect unix socket: %w", ErrUnsafePath)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID {
		return fmt.Errorf("inspect unix socket owner: %w", ErrUnsafePath)
	}
	conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return ErrAlreadyLocked
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("probe unix socket: %w", dialErr)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale unix socket: %w", err)
	}
	return nil
}

type peerListener struct {
	net.Listener
	expectedUID uint32
}

func EnforcePeerCredentials(listener net.Listener, expectedUID uint32) net.Listener {
	if listener == nil {
		return nil
	}
	if _, ok := listener.Addr().(*net.UnixAddr); !ok {
		return listener
	}
	return &peerListener{Listener: listener, expectedUID: expectedUID}
}

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uid, err := peerUID(conn)
		if err == nil && uid == l.expectedUID {
			return conn, nil
		}
		_ = conn.Close()
	}
}

func peerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, ErrUnsupported
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credential *unix.Xucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, controlErr
	}
	return credential.Uid, nil
}
