//go:build !linux && !darwin

package platform

import "os"

func ownedByCurrentUser(os.FileInfo) bool { return true }
