//go:build !linux

package platform

import "net"

func ActivatedListener() (net.Listener, bool, error) { return nil, false, ErrUnsupported }
