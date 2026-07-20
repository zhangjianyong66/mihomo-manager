package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestLegacyMigrationRepositoryLifecycleAndConflict(t *testing.T) {
	stateStore := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	profileID := domain.ProfileID("legacy-mihomo")
	profile := domain.Profile{ID: profileID, Name: "Legacy mihomo", Mode: domain.ProfileModeLegacy, CoreType: domain.CoreTypeMihomo, ConfigPath: "/tmp/legacy/config.yaml", Revision: 1, CreatedAt: now, UpdatedAt: now}
	migration := domain.LegacyMigration{
		ID: "legacy-1", ProfileID: profileID, SourceDir: "/tmp/legacy", State: domain.LegacyMigrationStatePending,
		Files:     []domain.LegacyFileSnapshot{{RelativePath: "config.yaml", BeforeExists: true, BeforeMode: 0o600, BeforeSize: 4, BeforeSHA256: string64("a"), ExpectedExists: true, ExpectedSHA256: string64("a"), SnapshotPath: filepath.Join(t.TempDir(), "config.yaml")}},
		CreatedAt: now, UpdatedAt: now,
	}
	operation := domain.Operation{ID: "migration-1", ProfileID: &profileID, Kind: "migration.apply", State: domain.OperationStateRunning, Phase: "snapshot_published", Attempt: 1, Recovery: json.RawMessage(`{"restorePoint":"legacy-1"}`), CreatedAt: now, UpdatedAt: now}
	if err := stateStore.CreateLegacyMigration(ctx, migration, profile, operation); err != nil {
		t.Fatal(err)
	}
	got, err := stateStore.GetLegacyMigration(ctx, migration.ID)
	if err != nil || len(got.Files) != 1 || got.Files[0].ExpectedSHA256 != string64("a") {
		t.Fatalf("get migration = %+v, %v", got, err)
	}
	conflicting := migration
	conflicting.ID = "legacy-2"
	conflicting.CreatedAt = now.Add(time.Second)
	conflicting.UpdatedAt = conflicting.CreatedAt
	conflictingOperation := operation
	conflictingOperation.ID = "migration-2"
	conflictingOperation.CreatedAt = conflicting.CreatedAt
	conflictingOperation.UpdatedAt = conflicting.CreatedAt
	if err := stateStore.CreateLegacyMigration(ctx, conflicting, profile, conflictingOperation); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate source error = %v", err)
	}
	completed := now.Add(2 * time.Second)
	migration.State = domain.LegacyMigrationStateSucceeded
	migration.UpdatedAt = completed
	migration.CompletedAt = &completed
	operation.State = domain.OperationStateSucceeded
	operation.Phase = "ready"
	operation.UpdatedAt = completed
	active, err := stateStore.UpdateLegacyMigration(ctx, migration, operation, true)
	if err != nil || !active {
		t.Fatalf("complete migration active=%v err=%v", active, err)
	}
	rollbackAt := now.Add(3 * time.Second)
	migration.State = domain.LegacyMigrationStateRolledBack
	migration.UpdatedAt = rollbackAt
	migration.RolledBackAt = &rollbackAt
	rollback := domain.Operation{ID: "rollback-1", ProfileID: &profileID, Kind: "migration.rollback", State: domain.OperationStateSucceeded, Phase: "restored", Attempt: 1, Recovery: json.RawMessage(`{"restorePoint":"legacy-1"}`), CreatedAt: rollbackAt, UpdatedAt: rollbackAt}
	if err := stateStore.RollbackLegacyMigration(ctx, migration, rollback); err != nil {
		t.Fatal(err)
	}
	profileAfter, err := stateStore.GetProfile(ctx, profileID)
	if err != nil || profileAfter.Active {
		t.Fatalf("profile after rollback = %+v, %v", profileAfter, err)
	}
	list, err := stateStore.ListLegacyMigrations(ctx)
	if err != nil || len(list) != 1 || list[0].State != domain.LegacyMigrationStateRolledBack {
		t.Fatalf("migration list = %+v, %v", list, err)
	}
}

func string64(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}
