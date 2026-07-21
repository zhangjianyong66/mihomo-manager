package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestIdentifiersValidate(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		validate func() error
	}{
		{name: "profile", value: "profile-1", validate: func() error { return ProfileID("profile-1").Validate() }},
		{name: "subscription", value: "subscription-1", validate: func() error { return SubscriptionID("subscription-1").Validate() }},
		{name: "node", value: "node-1", validate: func() error { return NodeID("node-1").Validate() }},
		{name: "group", value: "group-1", validate: func() error { return GroupID("group-1").Validate() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.validate(); err != nil {
				t.Fatalf("validate %s: %v", tt.value, err)
			}
		})
	}
}

func TestIdentifiersRejectEmptyValues(t *testing.T) {
	tests := []struct {
		name     string
		validate func() error
	}{
		{name: "profile", validate: func() error { return ProfileID(" ").Validate() }},
		{name: "subscription", validate: func() error { return SubscriptionID("").Validate() }},
		{name: "node", validate: func() error { return NodeID("\t").Validate() }},
		{name: "group", validate: func() error { return GroupID("\n").Validate() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.validate(); !errors.Is(err, ErrEmptyID) {
				t.Fatalf("expected ErrEmptyID, got %v", err)
			}
		})
	}
}

func TestCoreTypeValidate(t *testing.T) {
	if err := CoreTypeMihomo.Validate(); err != nil {
		t.Fatalf("validate mihomo: %v", err)
	}
	if got := CoreTypeMihomo.String(); got != "mihomo" {
		t.Fatalf("unexpected string: %q", got)
	}
	if err := CoreType("xray").Validate(); err == nil {
		t.Fatal("expected unsupported core type error")
	}
}

func TestCoreStateValidate(t *testing.T) {
	states := []CoreState{
		CoreStateStopped,
		CoreStateStarting,
		CoreStateRunning,
		CoreStateStopping,
		CoreStateDegraded,
		CoreStateFailed,
	}
	for _, state := range states {
		if err := state.Validate(); err != nil {
			t.Fatalf("validate %q: %v", state, err)
		}
	}
	if err := CoreState("unknown").Validate(); err == nil {
		t.Fatal("expected unsupported core state error")
	}
}

func TestRoutingModeValidate(t *testing.T) {
	for _, mode := range []RoutingMode{RoutingModeGlobal, RoutingModeRule, RoutingModeDirect} {
		if err := mode.Validate(); err != nil {
			t.Fatalf("validate %q: %v", mode, err)
		}
		if mode.String() != string(mode) {
			t.Fatalf("unexpected string for %q", mode)
		}
	}
	if err := RoutingMode("invalid").Validate(); err == nil {
		t.Fatal("expected unsupported routing mode error")
	}
}

func TestRoutingModeJSONRoundTripAndRejectsUnknownValue(t *testing.T) {
	for _, want := range []RoutingMode{RoutingModeGlobal, RoutingModeRule, RoutingModeDirect} {
		encoded, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		var got RoutingMode
		if err := json.Unmarshal(encoded, &got); err != nil || got != want {
			t.Fatalf("round trip %q => %q err=%v", want, got, err)
		}
	}
	var mode RoutingMode
	if err := json.Unmarshal([]byte(`"invalid"`), &mode); err == nil {
		t.Fatal("expected invalid JSON routing mode to fail")
	}
}
