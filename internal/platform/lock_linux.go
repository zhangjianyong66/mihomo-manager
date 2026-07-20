//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

type FileLock struct {
	mu   sync.Mutex
	file *os.File
}

func AcquireFileLock(path string) (*FileLock, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("open daemon lock: %w", ErrUnsafePath)
	}
	if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_CLOEXEC|unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrAlreadyLocked
		}
		return nil, fmt.Errorf("lock daemon: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = file.Close()
		return nil, fmt.Errorf("secure daemon lock: %w", err)
	}
	return &FileLock{file: file}, nil
}

func (l *FileLock) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
