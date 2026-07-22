package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestDaemonUnitEnvironmentUsesExplicitRuntimePaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CONFIG_DIR", filepath.Join(root, "配置 path"))
	t.Setenv("MIHOMO_BIN", filepath.Join(root, "bin", "mihomo"))
	t.Setenv("MIHOMO_API_PORT", "19090")

	got := daemonUnitEnvironment()
	if got["CONFIG_DIR"] != filepath.Join(root, "配置 path") || got["MIHOMO_BIN"] != filepath.Join(root, "bin", "mihomo") || got["MIHOMO_API_PORT"] != "19090" {
		t.Fatalf("unexpected daemon environment: %#v", got)
	}
}

type fakeServices struct{}

var (
	_ CoreService       = fakeServices{}
	_ ProfileService    = fakeServices{}
	_ RouteService      = fakeServices{}
	_ ConnectionService = fakeServices{}
	_ ModeService       = fakeServices{}
	_ ConfigService     = fakeServices{}
	_ LogService        = fakeServices{}
)

func (fakeServices) ModeStatus(context.Context, domain.ProfileID) (RoutingModeStatus, error) {
	return RoutingModeStatus{}, nil
}
func (fakeServices) SetMode(context.Context, SetRoutingModeRequest) (RoutingModeStatus, error) {
	return RoutingModeStatus{}, nil
}

func (fakeServices) Status(context.Context, domain.ProfileID) (CoreStatus, error) {
	return CoreStatus{}, nil
}
func (fakeServices) Start(context.Context, domain.ProfileID) error   { return nil }
func (fakeServices) Stop(context.Context, domain.ProfileID) error    { return nil }
func (fakeServices) Restart(context.Context, domain.ProfileID) error { return nil }
func (fakeServices) Reload(context.Context, domain.ProfileID) error  { return nil }

func (fakeServices) List(context.Context) ([]Profile, error) { return nil, nil }
func (fakeServices) Show(context.Context, domain.ProfileID) (Profile, error) {
	return Profile{}, nil
}
func (fakeServices) Use(context.Context, domain.ProfileID) error { return nil }

func (fakeServices) Test(context.Context, NodeTestRequest) <-chan NodeTestEvent {
	return nil
}

func (fakeServices) Select(context.Context, domain.ProfileID, domain.GroupID, domain.NodeID) error {
	return nil
}

func (fakeServices) SetURL(context.Context, domain.ProfileID, domain.SubscriptionID, string) error {
	return nil
}
func (fakeServices) Update(context.Context, domain.ProfileID, domain.SubscriptionID) error {
	return nil
}

func (fakeServices) Diagnose(context.Context, domain.ProfileID, string) (RouteDiagnosis, error) {
	return RouteDiagnosis{}, nil
}
func (fakeServices) Connections(context.Context, ConnectionRequest) ([]Connection, error) {
	return nil, nil
}
func (fakeServices) FollowConnections(context.Context, ConnectionRequest) <-chan ConnectionEvent {
	return nil
}
func (fakeServices) ListWhitelist(context.Context, domain.ProfileID) ([]string, error) {
	return nil, nil
}
func (fakeServices) AddWhitelist(context.Context, domain.ProfileID, string) error { return nil }
func (fakeServices) RemoveWhitelist(context.Context, domain.ProfileID, string) error {
	return nil
}
func (fakeServices) ApplyPreset(context.Context, domain.ProfileID, string) error { return nil }

func (fakeServices) Render(context.Context, domain.ProfileID) (RenderedConfig, error) {
	return RenderedConfig{}, nil
}
func (fakeServices) Diff(context.Context, domain.ProfileID) (ConfigDiff, error) {
	return ConfigDiff{}, nil
}
func (fakeServices) Validate(context.Context, domain.ProfileID) error { return nil }
func (fakeServices) Backup(context.Context, domain.ProfileID) error   { return nil }
func (fakeServices) Restore(context.Context, domain.ProfileID) error  { return nil }
func (fakeServices) Edit(context.Context, domain.ProfileID) error     { return nil }

func (fakeServices) Tail(context.Context, LogRequest) ([]LogLine, error) { return nil, nil }
func (fakeServices) Follow(context.Context, LogRequest) <-chan LogEvent  { return nil }

type fakeNodeService struct{ fakeServices }

func (fakeNodeService) List(context.Context, domain.ProfileID, domain.GroupID) ([]Node, error) {
	return nil, nil
}

type fakeGroupService struct{ fakeServices }

func (fakeGroupService) List(context.Context, domain.ProfileID) ([]Group, error) {
	return nil, nil
}
func (fakeGroupService) Show(context.Context, domain.ProfileID, domain.GroupID) (Group, error) {
	return Group{}, nil
}

type fakeSubscriptionService struct{ fakeServices }

func (fakeSubscriptionService) List(context.Context, domain.ProfileID) ([]Subscription, error) {
	return nil, nil
}
func (fakeSubscriptionService) Show(context.Context, domain.ProfileID, domain.SubscriptionID) (Subscription, error) {
	return Subscription{}, nil
}

var (
	_ NodeService         = fakeNodeService{}
	_ GroupService        = fakeGroupService{}
	_ SubscriptionService = fakeSubscriptionService{}
)

func TestErrorPreservesMessageAndCause(t *testing.T) {
	cause := errors.New("socket closed")
	err := &Error{
		Code:    ErrorCodeDaemonUnavailable,
		Message: "daemon 不可用",
		Err:     cause,
	}
	wrapped := fmt.Errorf("status: %w", err)

	if got := err.Error(); got != "daemon 不可用" {
		t.Fatalf("unexpected message: %q", got)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("expected wrapped cause to remain discoverable")
	}
	var appErr *Error
	if !errors.As(wrapped, &appErr) || appErr.Code != ErrorCodeDaemonUnavailable {
		t.Fatalf("unexpected application error: %#v", appErr)
	}
}

func TestErrorFallbacks(t *testing.T) {
	cause := errors.New("cause")
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{name: "cause", err: &Error{Code: ErrorCodeInternal, Err: cause}, want: "cause"},
		{name: "code", err: &Error{Code: ErrorCodeNotFound}, want: "NOT_FOUND"},
		{name: "nil", err: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestErrorClassificationAllowsSpecificMachineCodes(t *testing.T) {
	err := &Error{
		Category: ErrorCategoryConflict,
		Code:     ErrorCode("PROFILE_CONFLICT"),
	}
	if got := err.Classification(); got != ErrorCategoryConflict {
		t.Fatalf("got %q want %q", got, ErrorCategoryConflict)
	}

	legacy := &Error{Code: ErrorCodePermissionDenied}
	if got := legacy.Classification(); got != ErrorCategoryPermissionDenied {
		t.Fatalf("got %q want %q", got, ErrorCategoryPermissionDenied)
	}

	var nilError *Error
	if got := nilError.Classification(); got != ErrorCategoryInternal {
		t.Fatalf("got %q want %q", got, ErrorCategoryInternal)
	}
}
