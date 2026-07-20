//go:build !linux

package platform

import "os"

func ownedByCurrentUser(os.FileInfo) bool { return true }
