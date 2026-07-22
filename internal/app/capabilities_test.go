package app

import (
	"errors"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/daemon"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
)

func TestPortConflictIPCErrorKeepsCategoryCodeAndDetails(t *testing.T) {
	details := map[string]any{
		"conflicts": []any{
			map[string]any{"field": "mixed-port", "network": "tcp", "host": "127.0.0.1", "port": float64(17890)},
			map[string]any{"field": "external-controller", "network": "tcp", "host": "127.0.0.1", "port": float64(19090)},
		},
	}
	mapped := mapIPCError(&ipc.Error{Body: ipc.ErrorBody{Code: "PORT_CONFLICT", Message: "监听端口冲突", Details: details}})
	var appErr *Error
	if !errors.As(mapped, &appErr) {
		t.Fatalf("expected app error, got %T", mapped)
	}
	if appErr.Category != ErrorCategoryConflict || appErr.Code != ErrorCode("PORT_CONFLICT") || appErr.Details == nil {
		t.Fatalf("unexpected mapped error: %+v", appErr)
	}
	conflicts, ok := appErr.Details["conflicts"].([]any)
	if !ok || len(conflicts) != 2 {
		t.Fatalf("port conflict details were not preserved: %#v", appErr.Details)
	}
}

func TestDecodeListenerPortStatusErrorKeepsPartialStatus(t *testing.T) {
	err := &ipc.Error{Body: ipc.ErrorBody{
		Code: "PORT_CONFLICT",
		Details: map[string]any{"status": map[string]any{
			"profileId": "legacy-mihomo",
			"coreState": "running",
			"ports": []any{
				map[string]any{"field": "mixed-port", "host": "127.0.0.1", "port": float64(17890), "enabled": true, "networks": []any{"tcp", "udp"}},
			},
			"portConflicts": []any{
				map[string]any{"field": "mixed-port", "network": "tcp", "host": "127.0.0.1", "port": float64(17890)},
			},
		}},
	}}
	var value daemon.ListenerPortStatus
	decodeListenerPortStatusError(err, &value)
	status := convertListenerPortStatus(value)
	if status.ProfileID != "legacy-mihomo" || len(status.Ports) != 1 || len(status.PortConflicts) != 1 {
		t.Fatalf("partial listener port status was not preserved: %+v", status)
	}
}
