package domain

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	ID         ProfileID   `json:"id"`
	Name       string      `json:"name"`
	Mode       ProfileMode `json:"mode"`
	CoreType   CoreType    `json:"coreType"`
	ConfigPath string      `json:"configPath,omitempty"`
	Active     bool        `json:"active"`
	Revision   int64       `json:"revision"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
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

type LegacyMigrationState string

const (
	LegacyMigrationStatePending    LegacyMigrationState = "pending"
	LegacyMigrationStateSucceeded  LegacyMigrationState = "succeeded"
	LegacyMigrationStateFailed     LegacyMigrationState = "failed"
	LegacyMigrationStateRolledBack LegacyMigrationState = "rolled_back"
)

func (s LegacyMigrationState) String() string { return string(s) }

func (s LegacyMigrationState) Validate() error {
	switch s {
	case LegacyMigrationStatePending, LegacyMigrationStateSucceeded, LegacyMigrationStateFailed, LegacyMigrationStateRolledBack:
		return nil
	default:
		return fmt.Errorf("unsupported legacy migration state %q", s)
	}
}

type LegacyFileSnapshot struct {
	RelativePath   string `json:"relativePath"`
	BeforeExists   bool   `json:"beforeExists"`
	BeforeMode     uint32 `json:"beforeMode,omitempty"`
	BeforeSize     int64  `json:"beforeSize,omitempty"`
	BeforeSHA256   string `json:"beforeSha256,omitempty"`
	ExpectedExists bool   `json:"expectedExists"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
	SnapshotPath   string `json:"-"`
}

func (f LegacyFileSnapshot) Validate() error {
	if err := requireText("legacy file relative path", f.RelativePath); err != nil {
		return err
	}
	if filepath.IsAbs(f.RelativePath) || filepath.Clean(f.RelativePath) != f.RelativePath || f.RelativePath == "." || strings.HasPrefix(f.RelativePath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("legacy file relative path must stay within source directory")
	}
	if f.BeforeSize < 0 {
		return fmt.Errorf("legacy file size must not be negative")
	}
	if f.BeforeExists {
		if len(f.BeforeSHA256) != 64 || !filepath.IsAbs(f.SnapshotPath) {
			return fmt.Errorf("existing legacy file requires digest and absolute snapshot path")
		}
	} else if f.BeforeSHA256 != "" || f.SnapshotPath != "" || f.BeforeMode != 0 || f.BeforeSize != 0 {
		return fmt.Errorf("absent legacy file must not have snapshot metadata")
	}
	if f.ExpectedExists && len(f.ExpectedSHA256) != 64 {
		return fmt.Errorf("expected legacy file requires digest")
	}
	if !f.ExpectedExists && f.ExpectedSHA256 != "" {
		return fmt.Errorf("absent expected legacy file must not have digest")
	}
	return nil
}

type LegacyMigration struct {
	ID           RestorePointID       `json:"restorePoint"`
	ProfileID    ProfileID            `json:"profileId"`
	SourceDir    string               `json:"sourceDir"`
	State        LegacyMigrationState `json:"state"`
	Files        []LegacyFileSnapshot `json:"files"`
	ErrorCode    string               `json:"errorCode,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
	CompletedAt  *time.Time           `json:"completedAt,omitempty"`
	RolledBackAt *time.Time           `json:"rolledBackAt,omitempty"`
}

func (m LegacyMigration) Validate() error {
	if err := m.ID.Validate(); err != nil {
		return err
	}
	if err := m.ProfileID.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(m.SourceDir) {
		return fmt.Errorf("legacy source directory must be absolute")
	}
	if err := m.State.Validate(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(m.Files))
	for _, file := range m.Files {
		if err := file.Validate(); err != nil {
			return err
		}
		if _, exists := seen[file.RelativePath]; exists {
			return fmt.Errorf("duplicate legacy file %q", file.RelativePath)
		}
		seen[file.RelativePath] = struct{}{}
	}
	if err := validateTimes(m.CreatedAt, m.UpdatedAt); err != nil {
		return err
	}
	if err := validateOptionalUTC("legacy migration completed time", m.CompletedAt); err != nil {
		return err
	}
	if err := validateOptionalUTC("legacy migration rolled back time", m.RolledBackAt); err != nil {
		return err
	}
	return nil
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
