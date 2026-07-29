//go:build darwin

package platform

import "net"

// launchd does not pass a systemd-style socket activation descriptor.
func ActivatedListener() (net.Listener, bool, error) { return nil, false, nil }
