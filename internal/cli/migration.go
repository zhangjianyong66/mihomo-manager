package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func newMigrationCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "migrate",
		Short: "迁移现有 1.x legacy 配置",
		Long:  "迁移现有 1.x legacy 配置。CONFIG_DIR 选择旧配置目录；MIHOMO_BIN 用于验证；MIHOMO_API_PORT 只用于 loopback 探测；EDITOR 仅由显式配置编辑使用。",
		Args:  noArgs,
	}
	command.SetFlagErrorFunc(invalidFlagError)

	plan := &cobra.Command{Use: "plan", Short: "预览将读取、备份和保持不变的 legacy 文件", Args: noArgs}
	addDaemonOutput(plan, func(cmd *cobra.Command) error {
		if deps.Migrations == nil {
			return &app.Error{Code: app.ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
		}
		value, err := deps.Migrations.Plan(cmd.Context())
		if err != nil {
			return err
		}
		return daemonPresenter(cmd).WriteResult(Result{
			Kind: "MigrationPlan",
			Data: func(bool) any { return value },
			Table: func(w io.Writer, _ bool) error {
				if _, err := fmt.Fprintf(w, "legacy 配置目录: %s\n", value.ConfigDir); err != nil {
					return err
				}
				for _, entry := range value.Entries {
					status := "不存在"
					if entry.Exists {
						status = fmt.Sprintf("保留 size=%d mode=%s sha256=%s", entry.Size, formatMode(entry.Mode), entry.SHA256)
					}
					if _, err := fmt.Fprintf(w, "%s: %s\n", entry.RelativePath, status); err != nil {
						return err
					}
				}
				return nil
			},
		})
	})

	apply := &cobra.Command{Use: "apply", Short: "创建恢复点并注册 legacy 档案", Args: noArgs}
	addDaemonOutput(apply, func(cmd *cobra.Command) error {
		if deps.Migrations == nil {
			return &app.Error{Code: app.ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
		}
		value, err := deps.Migrations.Apply(cmd.Context())
		if err != nil {
			return err
		}
		return daemonPresenter(cmd).WriteResult(Result{
			Kind: "MigrationApply",
			Data: func(bool) any { return value },
			Table: func(w io.Writer, _ bool) error {
				_, err := fmt.Fprintf(w, "恢复点: %s\n状态: %s\n档案: %s\n", value.Migration.ID, value.Migration.State, value.Migration.ProfileID)
				return err
			},
		})
	})

	status := &cobra.Command{Use: "status", Short: "查询 legacy 迁移和恢复点状态", Args: noArgs}
	addDaemonOutput(status, func(cmd *cobra.Command) error {
		if deps.Migrations == nil {
			return &app.Error{Code: app.ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
		}
		value, err := deps.Migrations.Status(cmd.Context())
		if err != nil {
			return err
		}
		return daemonPresenter(cmd).WriteResult(Result{
			Kind: "MigrationStatus",
			Data: func(bool) any { return value },
			Table: func(w io.Writer, _ bool) error {
				if len(value) == 0 {
					_, err := fmt.Fprintln(w, "暂无 legacy 迁移记录")
					return err
				}
				for _, migration := range value {
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", migration.ID, migration.State, migration.SourceDir); err != nil {
						return err
					}
				}
				return nil
			},
		})
	})

	var restorePoint string
	rollback := &cobra.Command{Use: "rollback", Short: "显式恢复指定 legacy 恢复点", Args: noArgs}
	rollback.Flags().StringVar(&restorePoint, "restore-point", "", "要恢复的恢复点 ID（必填）")
	addDaemonOutput(rollback, func(cmd *cobra.Command) error {
		if restorePoint == "" {
			return &app.Error{Code: app.ErrorCodeInvalidArgument, Message: "必须显式指定 --restore-point"}
		}
		if deps.Migrations == nil {
			return &app.Error{Code: app.ErrorCodeDaemonUnavailable, Message: "迁移 daemon 客户端未配置"}
		}
		value, err := deps.Migrations.Rollback(cmd.Context(), domain.RestorePointID(restorePoint))
		if err != nil {
			return err
		}
		return daemonPresenter(cmd).WriteResult(Result{
			Kind: "MigrationRollback",
			Data: func(bool) any { return value },
			Table: func(w io.Writer, _ bool) error {
				_, err := fmt.Fprintf(w, "恢复点 %s 已回滚\n", value.Migration.ID)
				return err
			},
		})
	})

	command.AddCommand(plan, apply, status, rollback)
	return command
}

func formatMode(mode uint32) string { return "0" + strconv.FormatUint(uint64(mode), 8) }
