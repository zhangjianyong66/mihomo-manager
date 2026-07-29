package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrAlreadyLocked = errors.New("daemon already running")
	ErrUnsafePath    = errors.New("unsafe path")
	ErrUnsupported   = errors.New("platform feature unsupported")
)

func EnsurePrivateDir(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("secure directory: %w", ErrUnsafePath)
	}
	path = filepath.Clean(path)
	if err := RejectExistingSymlinkComponents(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("inspect private directory: %w", ErrUnsafePath)
	}
	if !ownedByCurrentUser(info) {
		return fmt.Errorf("inspect private directory owner: %w", ErrUnsafePath)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure private directory: %w", err)
	}
	return nil
}

// RejectExistingSymlinkComponents rejects absolute paths whose existing prefix contains a symlink.
func RejectExistingSymlinkComponents(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("inspect path component: %w", ErrUnsafePath)
	}
	volume := filepath.VolumeName(path)
	current := string(os.PathSeparator)
	if volume != "" {
		current = volume + string(os.PathSeparator)
	}
	relative := path[len(current):]
	for _, part := range splitPath(relative) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect path component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("inspect path component: %w", ErrUnsafePath)
		}
	}
	return nil
}

func splitPath(path string) []string {
	var parts []string
	for path != "." && path != "" {
		dir, file := filepath.Split(path)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		path = filepath.Clean(dir)
	}
	return parts
}
