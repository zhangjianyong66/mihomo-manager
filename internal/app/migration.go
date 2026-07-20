package app

import (
	"context"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
)

type MigrationClient interface {
	Plan(context.Context) (legacy.Plan, error)
	Apply(context.Context) (legacy.ApplyResult, error)
	Status(context.Context) ([]domain.LegacyMigration, error)
	Rollback(context.Context, domain.RestorePointID) (legacy.RollbackResult, error)
}

type MigrationService struct {
	Client MigrationClient
}

func (s *MigrationService) Plan(ctx context.Context) (legacy.Plan, error) {
	if s == nil || s.Client == nil {
		return legacy.Plan{}, &Error{Code: ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
	}
	return s.Client.Plan(ctx)
}

func (s *MigrationService) Apply(ctx context.Context) (legacy.ApplyResult, error) {
	if s == nil || s.Client == nil {
		return legacy.ApplyResult{}, &Error{Code: ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
	}
	return s.Client.Apply(ctx)
}

func (s *MigrationService) Status(ctx context.Context) ([]domain.LegacyMigration, error) {
	if s == nil || s.Client == nil {
		return nil, &Error{Code: ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
	}
	return s.Client.Status(ctx)
}

func (s *MigrationService) Rollback(ctx context.Context, id domain.RestorePointID) (legacy.RollbackResult, error) {
	if s == nil || s.Client == nil {
		return legacy.RollbackResult{}, &Error{Code: ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
	}
	if err := id.Validate(); err != nil {
		return legacy.RollbackResult{}, &Error{Code: ErrorCodeInvalidArgument, Message: "恢复点 ID 无效", Err: err}
	}
	return s.Client.Rollback(ctx, id)
}
