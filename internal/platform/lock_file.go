package platform

import (
	"fmt"
	"os"
)

func validateLockFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect daemon lock: %w", err)
	}
	if !info.Mode().IsRegular() || !ownedByCurrentUser(info) {
		return fmt.Errorf("inspect daemon lock: %w", ErrUnsafePath)
	}
	return nil
}
