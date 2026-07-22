package daemon

import (
	"fmt"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
)

func TestCapabilityErrorDetailsPreservesAllPortConflicts(t *testing.T) {
	conflicts := []core.PortConflict{
		{Field: "mixed-port", Network: "tcp", Host: "127.0.0.1", Port: 17890},
		{Field: "mixed-port", Network: "udp", Host: "127.0.0.1", Port: 17890},
		{Field: "external-controller", Network: "tcp", Host: "127.0.0.1", Port: 19090},
	}
	err := fmt.Errorf("start mihomo: %w", &core.PortConflictError{Conflicts: conflicts})
	details := capabilityErrorDetails(err)
	got, ok := details["conflicts"].([]core.PortConflict)
	if !ok || len(got) != len(conflicts) {
		t.Fatalf("port conflicts were not preserved: %#v", details)
	}
	for index := range conflicts {
		if got[index] != conflicts[index] {
			t.Fatalf("conflict %d = %+v, want %+v", index, got[index], conflicts[index])
		}
	}
}
