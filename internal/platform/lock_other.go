//go:build !linux

package platform

type FileLock struct{}

func AcquireFileLock(string) (*FileLock, error) { return nil, ErrUnsupported }
func (*FileLock) Close() error                  { return nil }
