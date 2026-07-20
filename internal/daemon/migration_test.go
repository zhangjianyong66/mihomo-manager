package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
)

type fakeMigrationService struct {
	plan legacy.Plan
	err  error
}

func (f fakeMigrationService) Plan(context.Context) (legacy.Plan, error) { return f.plan, f.err }
func (f fakeMigrationService) Apply(context.Context) (legacy.ApplyResult, error) {
	return legacy.ApplyResult{}, f.err
}
func (f fakeMigrationService) Status(context.Context) ([]domain.LegacyMigration, error) {
	return nil, f.err
}
func (f fakeMigrationService) Rollback(context.Context, domain.RestorePointID) (legacy.RollbackResult, error) {
	return legacy.RollbackResult{}, f.err
}

func TestMigrationHandlersValidateMethodsAndRollbackID(t *testing.T) {
	server := &Server{migrations: fakeMigrationService{plan: legacy.Plan{ConfigDir: "/tmp/legacy"}}}
	request := httptest.NewRequest(http.MethodGet, "/v1/migrations/plan", nil)
	recorder := httptest.NewRecorder()
	server.handleMigrationPlan(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "MigrationPlan") {
		t.Fatalf("plan response: %d %s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/v1/migrations/rollback", bytes.NewReader([]byte(`{}`)))
	recorder = httptest.NewRecorder()
	server.handleMigrationRollback(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "restorePoint") {
		t.Fatalf("rollback validation: %d %s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/migrations/apply", nil)
	recorder = httptest.NewRecorder()
	server.handleMigrationApply(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("method validation: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMigrationHandlerMapsValidationAndConflict(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "validation", err: legacy.ErrValidation, status: http.StatusUnprocessableEntity, code: "VALIDATION_FAILED"},
		{name: "conflict", err: legacy.ErrConflict, status: http.StatusConflict, code: "CONFLICT"},
		{name: "not found", err: legacy.ErrNotFound, status: http.StatusNotFound, code: "NOT_FOUND"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &Server{migrations: fakeMigrationService{err: test.err}}
			recorder := httptest.NewRecorder()
			server.handleMigrationPlan(recorder, httptest.NewRequest(http.MethodGet, "/v1/migrations/plan", nil))
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.status, recorder.Body.String())
			}
			var response struct {
				Error *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error == nil || response.Error.Code != test.code {
				t.Fatalf("error = %+v, want %s", response.Error, test.code)
			}
		})
	}
	server := &Server{migrations: fakeMigrationService{err: errors.New("internal")}}
	recorder := httptest.NewRecorder()
	server.handleMigrationPlan(recorder, httptest.NewRequest(http.MethodGet, "/v1/migrations/plan", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("internal status = %d", recorder.Code)
	}
}
