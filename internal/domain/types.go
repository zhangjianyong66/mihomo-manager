package domain

import (
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
