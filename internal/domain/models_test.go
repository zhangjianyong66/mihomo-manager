package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOperationIDValidate(t *testing.T) {
	if err := OperationID("operation-1").Validate(); err != nil {
		t.Fatalf("validate operation id: %v", err)
	}
	if err := OperationID(" ").Validate(); err == nil {
		t.Fatal("expected empty operation id to fail")
	}
}

func TestProfileModeValidate(t *testing.T) {
	for _, mode := range []ProfileMode{ProfileModeManaged, ProfileModeExternal, ProfileModeLegacy} {
		if err := mode.Validate(); err != nil {
			t.Fatalf("validate %q: %v", mode, err)
		}
	}
	if err := ProfileMode("unknown").Validate(); err == nil {
		t.Fatal("expected unsupported mode to fail")
	}
}

func TestOperationStateValidate(t *testing.T) {
	states := []OperationState{
		OperationStatePending,
		OperationStateRunning,
		OperationStateSucceeded,
		OperationStateFailed,
		OperationStateRollingBack,
		OperationStateRolledBack,
	}
	for _, state := range states {
		if err := state.Validate(); err != nil {
			t.Fatalf("validate %q: %v", state, err)
		}
	}
	if err := OperationState("unknown").Validate(); err == nil {
		t.Fatal("expected unsupported state to fail")
	}
}

func TestProfileValidate_ModeAndPath(t *testing.T) {
	now := time.Now().UTC()
	valid := Profile{
		ID: "profile-1", Name: "default", Mode: ProfileModeManaged,
		CoreType: CoreTypeMihomo, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("validate managed profile: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Profile)
	}{
		{name: "managed path", mutate: func(p *Profile) { p.ConfigPath = "/tmp/config.yaml" }},
		{name: "external without path", mutate: func(p *Profile) { p.Mode = ProfileModeExternal }},
		{name: "revision", mutate: func(p *Profile) { p.Revision = 0 }},
		{name: "non UTC", mutate: func(p *Profile) { p.UpdatedAt = p.UpdatedAt.In(time.FixedZone("offset", 3600)) }},
		{name: "reversed time", mutate: func(p *Profile) { p.UpdatedAt = p.CreatedAt.Add(-time.Second) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := valid
			tt.mutate(&profile)
			if err := profile.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAggregateValidate_JSONOwnershipAndUTC(t *testing.T) {
	now := time.Now().UTC()
	profileID := ProfileID("profile-1")
	lastAttempt := now

	values := []struct {
		name     string
		validate func() error
	}{
		{
			name: "subscription",
			validate: func() error {
				return (Subscription{
					ID: "subscription-1", ProfileID: profileID, Name: "source", URL: "https://example.invalid/token",
					Enabled: true, LastAttemptAt: &lastAttempt, CreatedAt: now, UpdatedAt: now,
				}).Validate()
			},
		},
		{
			name: "node",
			validate: func() error {
				return (Node{
					ID: "node-1", SubscriptionID: "subscription-1", RemoteKey: "remote-1", Name: "node",
					Protocol: "vless", Spec: json.RawMessage(`{"server":"example.invalid"}`), CreatedAt: now, UpdatedAt: now,
				}).Validate()
			},
		},
		{
			name: "operation",
			validate: func() error {
				return (Operation{
					ID: "operation-1", ProfileID: &profileID, Kind: "subscription.refresh", State: OperationStatePending,
					Phase: "queued", Recovery: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
				}).Validate()
			},
		},
		{
			name: "setting",
			validate: func() error {
				return (Setting{ProfileID: &profileID, Key: "routing.mode", Value: json.RawMessage(`"rule"`), UpdatedAt: now}).Validate()
			},
		},
	}

	for _, value := range values {
		t.Run(value.name, func(t *testing.T) {
			if err := value.validate(); err != nil {
				t.Fatalf("validate: %v", err)
			}
		})
	}

	invalidNode := Node{
		ID: "node-1", SubscriptionID: "subscription-1", RemoteKey: "remote-1", Name: "node",
		Protocol: "vless", Spec: json.RawMessage(`{`), CreatedAt: now, UpdatedAt: now,
	}
	if err := invalidNode.Validate(); err == nil {
		t.Fatal("expected invalid node JSON to fail")
	}

	invalidSetting := Setting{Key: "key", Value: json.RawMessage(`null`), UpdatedAt: now.In(time.Local)}
	if err := invalidSetting.Validate(); err == nil {
		t.Fatal("expected non-UTC setting time to fail")
	}
}
