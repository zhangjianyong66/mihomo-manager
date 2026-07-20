//go:build !linux

package platform

import "net"

func ListenUnix(string, uint32) (net.Listener, error) { return nil, ErrUnsupported }
func EnforcePeerCredentials(listener net.Listener, _ uint32) net.Listener {
	return listener
}
