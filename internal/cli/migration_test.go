package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
)

type fakeMigrationClient struct {
	plan           legacy.Plan
	apply          legacy.ApplyResult
	status         []domain.LegacyMigration
	rollback       legacy.RollbackResult
	err            error
	rollbackID     domain.RestorePointID
	rollbackCalled int
}

func (f *fakeMigrationClient) Plan(context.Context) (legacy.Plan, error) { return f.plan, f.err }
func (f *fakeMigrationClient) Apply(context.Context) (legacy.ApplyResult, error) {
	return f.apply, f.err
}
func (f *fakeMigrationClient) Status(context.Context) ([]domain.LegacyMigration, error) {
	return f.status, f.err
}
func (f *fakeMigrationClient) Rollback(_ context.Context, id domain.RestorePointID) (legacy.RollbackResult, error) {
	f.rollbackCalled++
	f.rollbackID = id
	return f.rollback, f.err
}

func TestMigrationPlanJSONAndStatusTable(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	client := &fakeMigrationClient{
		plan:   legacy.Plan{ConfigDir: "/tmp/legacy", Entries: []legacy.FileObservation{{RelativePath: "subscription.url", Exists: true, Sensitive: true, SHA256: strings.Repeat("a", 64)}}},
		status: []domain.LegacyMigration{{ID: "legacy-1", ProfileID: "legacy-mihomo", SourceDir: "/tmp/legacy", State: domain.LegacyMigrationStateSucceeded, CreatedAt: now, UpdatedAt: now}},
	}
	service := &app.MigrationService{Client: client}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Migrations: service}, []string{"migrate", "plan", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"kind":"MigrationPlan"`) || strings.Contains(stdout.String(), "token=") {
		t.Fatalf("plan code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = Execute(context.Background(), Dependencies{Migrations: service}, []string{"migrate", "status"}, nil, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "legacy-1\tsucceeded\t/tmp/legacy") {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestMigrationRollbackRequiresExplicitRestorePoint(t *testing.T) {
	client := &fakeMigrationClient{}
	service := &app.MigrationService{Client: client}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Migrations: service}, []string{"migrate", "rollback"}, nil, &stdout, &stderr)
	if code != ExitInvalidArgument || client.rollbackCalled != 0 || !strings.Contains(stderr.String(), "--restore-point") {
		t.Fatalf("missing id code=%d calls=%d stderr=%q", code, client.rollbackCalled, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	client.rollback = legacy.RollbackResult{Migration: domain.LegacyMigration{ID: "legacy-1", State: domain.LegacyMigrationStateRolledBack}}
	code = Execute(context.Background(), Dependencies{Migrations: service}, []string{"migrate", "rollback", "--restore-point", "legacy-1", "--output=json"}, nil, &stdout, &stderr)
	if code != 0 || client.rollbackID != "legacy-1" || !strings.Contains(stdout.String(), `"kind":"MigrationRollback"`) {
		t.Fatalf("rollback code=%d id=%s stdout=%q stderr=%q", code, client.rollbackID, stdout.String(), stderr.String())
	}
}

func TestMigrationValidationFailureUsesExitSix(t *testing.T) {
	client := &fakeMigrationClient{err: &app.Error{Category: app.ErrorCategoryValidationFailed, Code: app.ErrorCodeValidationFailed, Message: "旧配置校验失败"}}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Migrations: &app.MigrationService{Client: client}}, []string{"migrate", "apply", "--output=json"}, nil, &stdout, &stderr)
	if code != ExitValidationFailed || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"VALIDATION_FAILED"`) {
		t.Fatalf("validation code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
