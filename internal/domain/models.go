package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type OperationID string

func (id OperationID) String() string { return string(id) }

func (id OperationID) Validate() error { return validateID("operation", string(id)) }

type ProfileMode string

const (
	ProfileModeManaged  ProfileMode = "managed"
	ProfileModeExternal ProfileMode = "external"
	ProfileModeLegacy   ProfileMode = "legacy"
)

func (m ProfileMode) String() string { return string(m) }

func (m ProfileMode) Validate() error {
	switch m {
	case ProfileModeManaged, ProfileModeExternal, ProfileModeLegacy:
		return nil
	default:
		return fmt.Errorf("unsupported profile mode %q", m)
	}
}

type OperationState string

const (
	OperationStatePending     OperationState = "pending"
	OperationStateRunning     OperationState = "running"
	OperationStateSucceeded   OperationState = "succeeded"
	OperationStateFailed      OperationState = "failed"
	OperationStateRollingBack OperationState = "rolling_back"
	OperationStateRolledBack  OperationState = "rolled_back"
)

func (s OperationState) String() string { return string(s) }

func (s OperationState) Validate() error {
	switch s {
	case OperationStatePending, OperationStateRunning, OperationStateSucceeded,
		OperationStateFailed, OperationStateRollingBack, OperationStateRolledBack:
		return nil
	default:
		return fmt.Errorf("unsupported operation state %q", s)
	}
}

type Profile struct {
	ID         ProfileID
	Name       string
	Mode       ProfileMode
	CoreType   CoreType
	ConfigPath string
	Active     bool
	Revision   int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (p Profile) Validate() error {
	if err := p.ID.Validate(); err != nil {
		return err
	}
	if err := requireText("profile name", p.Name); err != nil {
		return err
	}
	if err := p.Mode.Validate(); err != nil {
		return err
	}
	if err := p.CoreType.Validate(); err != nil {
		return err
	}
	if p.Mode == ProfileModeManaged && strings.TrimSpace(p.ConfigPath) != "" {
		return fmt.Errorf("managed profile config path must be empty")
	}
	if p.Mode != ProfileModeManaged && strings.TrimSpace(p.ConfigPath) == "" {
		return fmt.Errorf("%s profile config path must not be empty", p.Mode)
	}
	if p.Revision < 1 {
		return fmt.Errorf("profile revision must be at least 1")
	}
	return validateTimes(p.CreatedAt, p.UpdatedAt)
}

type Subscription struct {
	ID            SubscriptionID
	ProfileID     ProfileID
	Name          string
	URL           string
	Enabled       bool
	ETag          string
	LastModified  string
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s Subscription) Validate() error {
	if err := s.ID.Validate(); err != nil {
		return err
	}
	if err := s.ProfileID.Validate(); err != nil {
		return err
	}
	if err := requireText("subscription name", s.Name); err != nil {
		return err
	}
	if err := requireText("subscription URL", s.URL); err != nil {
		return err
	}
	if err := validateOptionalUTC("last attempt", s.LastAttemptAt); err != nil {
		return err
	}
	if err := validateOptionalUTC("last success", s.LastSuccessAt); err != nil {
		return err
	}
	return validateTimes(s.CreatedAt, s.UpdatedAt)
}

type Node struct {
	ID             NodeID
	SubscriptionID SubscriptionID
	RemoteKey      string
	Name           string
	Protocol       string
	Spec           json.RawMessage
	Position       int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (n Node) Validate() error {
	if err := n.ID.Validate(); err != nil {
		return err
	}
	if err := n.SubscriptionID.Validate(); err != nil {
		return err
	}
	if err := requireText("node remote key", n.RemoteKey); err != nil {
		return err
	}
	if err := requireText("node name", n.Name); err != nil {
		return err
	}
	if err := requireText("node protocol", n.Protocol); err != nil {
		return err
	}
	if !json.Valid(n.Spec) {
		return fmt.Errorf("node spec must be valid JSON")
	}
	if n.Position < 0 {
		return fmt.Errorf("node position must not be negative")
	}
	return validateTimes(n.CreatedAt, n.UpdatedAt)
}

type Operation struct {
	ID        OperationID
	ProfileID *ProfileID
	Kind      string
	State     OperationState
	Phase     string
	Attempt   int
	Recovery  json.RawMessage
	ErrorCode string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (o Operation) Validate() error {
	if err := o.ID.Validate(); err != nil {
		return err
	}
	if o.ProfileID != nil {
		if err := o.ProfileID.Validate(); err != nil {
			return err
		}
	}
	if err := requireText("operation kind", o.Kind); err != nil {
		return err
	}
	if err := o.State.Validate(); err != nil {
		return err
	}
	if err := requireText("operation phase", o.Phase); err != nil {
		return err
	}
	if o.Attempt < 0 {
		return fmt.Errorf("operation attempt must not be negative")
	}
	if !json.Valid(o.Recovery) {
		return fmt.Errorf("operation recovery must be valid JSON")
	}
	return validateTimes(o.CreatedAt, o.UpdatedAt)
}

type Setting struct {
	ProfileID *ProfileID
	Key       string
	Value     json.RawMessage
	UpdatedAt time.Time
}

func (s Setting) Validate() error {
	if s.ProfileID != nil {
		if err := s.ProfileID.Validate(); err != nil {
			return err
		}
	}
	if err := requireText("setting key", s.Key); err != nil {
		return err
	}
	if !json.Valid(s.Value) {
		return fmt.Errorf("setting value must be valid JSON")
	}
	return validateUTC("setting updated time", s.UpdatedAt)
}

func requireText(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

func validateTimes(createdAt, updatedAt time.Time) error {
	if err := validateUTC("created time", createdAt); err != nil {
		return err
	}
	if err := validateUTC("updated time", updatedAt); err != nil {
		return err
	}
	if updatedAt.Before(createdAt) {
		return fmt.Errorf("updated time must not precede created time")
	}
	return nil
}

func validateOptionalUTC(name string, value *time.Time) error {
	if value == nil {
		return nil
	}
	return validateUTC(name, *value)
}

func validateUTC(name string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s must not be zero", name)
	}
	if value.Location() != time.UTC {
		return fmt.Errorf("%s must use UTC", name)
	}
	return nil
}
