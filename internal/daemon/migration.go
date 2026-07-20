package daemon

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/store"
)

type MigrationService interface {
	Plan(context.Context) (legacy.Plan, error)
	Apply(context.Context) (legacy.ApplyResult, error)
	Status(context.Context) ([]domain.LegacyMigration, error)
	Rollback(context.Context, domain.RestorePointID) (legacy.RollbackResult, error)
}

func (s *Server) handleMigrationPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "迁移计划只支持 GET", false, nil)
		return
	}
	if s.migrations == nil {
		writeMigrationError(w, errors.New("migration service unavailable"))
		return
	}
	plan, err := s.migrations.Plan(r.Context())
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	_ = ipc.WriteJSON(w, http.StatusOK, "MigrationPlan", plan)
}

func (s *Server) handleMigrationApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "迁移应用只支持 POST", false, nil)
		return
	}
	if s.migrations == nil {
		writeMigrationError(w, errors.New("migration service unavailable"))
		return
	}
	result, err := s.migrations.Apply(r.Context())
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	_ = ipc.WriteJSON(w, http.StatusOK, "MigrationApply", result)
}

func (s *Server) handleMigrationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "迁移状态只支持 GET", false, nil)
		return
	}
	if s.migrations == nil {
		writeMigrationError(w, errors.New("migration service unavailable"))
		return
	}
	result, err := s.migrations.Status(r.Context())
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	_ = ipc.WriteJSON(w, http.StatusOK, "MigrationStatus", result)
}

func (s *Server) handleMigrationRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "迁移回滚只支持 POST", false, nil)
		return
	}
	if s.migrations == nil {
		writeMigrationError(w, errors.New("migration service unavailable"))
		return
	}
	var request struct {
		RestorePoint string `json:"restorePoint"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	if strings.TrimSpace(request.RestorePoint) == "" {
		_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "必须显式指定 restorePoint", false, nil)
		return
	}
	result, err := s.migrations.Rollback(r.Context(), domain.RestorePointID(request.RestorePoint))
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	_ = ipc.WriteJSON(w, http.StatusOK, "MigrationRollback", result)
}

func writeMigrationError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL"
	retryable := false
	message := "迁移操作失败"
	switch {
	case errors.Is(err, legacy.ErrValidation):
		status, code, message = http.StatusUnprocessableEntity, "VALIDATION_FAILED", "旧配置校验失败"
	case errors.Is(err, legacy.ErrConflict), errors.Is(err, store.ErrConflict):
		status, code, message = http.StatusConflict, "CONFLICT", "迁移状态冲突"
	case errors.Is(err, legacy.ErrUnsafePath), errors.Is(err, store.ErrPermission):
		status, code, message = http.StatusForbidden, "PERMISSION_DENIED", "迁移路径或权限不安全"
	case errors.Is(err, legacy.ErrNotFound), errors.Is(err, store.ErrNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "恢复点不存在"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message, retryable = http.StatusRequestTimeout, "REQUEST_CANCELLED", "迁移请求已取消", true
	}
	_ = ipc.WriteError(w, status, code, message, retryable, nil)
}
