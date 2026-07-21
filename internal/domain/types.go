package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrEmptyID = errors.New("identifier must not be empty")

type ProfileID string

func (id ProfileID) String() string { return string(id) }

func (id ProfileID) Validate() error { return validateID("profile", string(id)) }

type SubscriptionID string

func (id SubscriptionID) String() string { return string(id) }

func (id SubscriptionID) Validate() error { return validateID("subscription", string(id)) }

type NodeID string

func (id NodeID) String() string { return string(id) }

func (id NodeID) Validate() error { return validateID("node", string(id)) }

type GroupID string

func (id GroupID) String() string { return string(id) }

func (id GroupID) Validate() error { return validateID("group", string(id)) }

type RestorePointID string

func (id RestorePointID) String() string { return string(id) }

func (id RestorePointID) Validate() error { return validateID("restore point", string(id)) }

func validateID(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s id: %w", kind, ErrEmptyID)
	}
	return nil
}

type CoreType string

const CoreTypeMihomo CoreType = "mihomo"

func (t CoreType) String() string { return string(t) }

func (t CoreType) Validate() error {
	switch t {
	case CoreTypeMihomo:
		return nil
	default:
		return fmt.Errorf("unsupported core type %q", t)
	}
}

type CoreState string

const (
	CoreStateStopped  CoreState = "stopped"
	CoreStateStarting CoreState = "starting"
	CoreStateRunning  CoreState = "running"
	CoreStateStopping CoreState = "stopping"
	CoreStateDegraded CoreState = "degraded"
	CoreStateFailed   CoreState = "failed"
)

func (s CoreState) String() string { return string(s) }

func (s CoreState) Validate() error {
	switch s {
	case CoreStateStopped, CoreStateStarting, CoreStateRunning, CoreStateStopping, CoreStateDegraded, CoreStateFailed:
		return nil
	default:
		return fmt.Errorf("unsupported core state %q", s)
	}
}

type RoutingMode string

const (
	RoutingModeGlobal RoutingMode = "global"
	RoutingModeRule   RoutingMode = "rule"
	RoutingModeDirect RoutingMode = "direct"
)

func (m RoutingMode) String() string { return string(m) }

func (m RoutingMode) Validate() error {
	switch m {
	case RoutingModeGlobal, RoutingModeRule, RoutingModeDirect:
		return nil
	default:
		return fmt.Errorf("unsupported routing mode %q", m)
	}
}

// UnmarshalJSON keeps configuration/API decoding strict: an unknown mode must
// not silently become a zero value and continue through a mutation path.
func (m *RoutingMode) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("routing mode receiver must not be nil")
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode routing mode: %w", err)
	}
	value = strings.ToLower(strings.TrimSpace(value))
	mode := RoutingMode(value)
	if err := mode.Validate(); err != nil {
		return err
	}
	*m = mode
	return nil
}
