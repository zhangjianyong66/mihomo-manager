# A5 实施计划：legacy 迁移、兼容层与回滚

## 依赖与执行规则

- 依赖已归档的 A2（SQLite/领域）、A3（daemon/IPC）和 A4（mihomo adapter/generation）。
- 当前为 Codex inline 模式，直接在主会话实现；不创建实现/检查子代理。
- 编码前加载 `trellis-before-dev`，先阅读 backend directory、code-style、CLI、storage、daemon-ipc、core-adapter 规范。
- 每个阶段保持可构建；不删除旧 `mihomo.Client` 或旧 Shell 兼容入口。
- 所有测试隔离 HOME/XDG/CONFIG_DIR/database/socket，不触碰真实用户配置，不启动真实 mihomo。

## 阶段清单

### P1：持久化与领域契约

- [x] 新增 `0003_legacy_migrations.sql`，追加恢复点和 manifest 表，确保迁移历史只前进。
- [x] 在 `internal/domain` 增加恢复点、文件摘要、发现项和迁移状态类型及校验。
- [x] 在 `internal/store` 增加事务仓储：创建/查询恢复点、更新 expected digest、标记 operation、创建稳定 legacy profile；覆盖重复和外键冲突。
- [x] 测试 schema 升级、权限 `0700/0600`、事务失败不留半条记录、JSON manifest 合法性和敏感字段不落日志。

### P2：legacy 发现、快照和兼容 service

- [x] 新建 `internal/legacy`，实现受控相对路径白名单、文件发现、权限/大小/SHA-256 摘要和脱敏计划。
- [x] 实现恢复点临时目录、fsync、原子发布、before/expected manifest；拒绝符号链接、路径穿越和非安全权限。
- [x] 封装配置 validate/backup/restore 的迁移前置能力；写入前摘要冲突检查，失败恢复并更新 expected digest。
- [x] 对 legacy core 仅支持 daemon/A4 所持进程的控制；不在新路径调用 `pgrep`/`pkill`，不可判断归属时只返回观察状态。
- [x] 建立 Good/Base/Bad 单元测试和 fake mihomo 命令契约测试。

### P3：daemon 生命周期与 IPC

- [x] 在 daemon 装配 legacy service，复用 request-id 缓存和 operation 状态记录边界。
- [x] 增加 `GET /v1/migrations/plan`、`POST /v1/migrations/apply`、`GET /v1/migrations/status`、`POST /v1/migrations/rollback`，校验协议版本、请求体上限和 context 取消。
- [x] apply/rollback 使用恢复点冲突保护；异常时确保旧配置摘要不变，恢复点临时目录清理，状态可由 status 识别。
- [x] IPC handler 测试覆盖方法、错误状态、显式 restore point 和恢复点冲突。

### P4：应用服务与 CLI

- [x] 增加 `MigrationService` port、daemon IPC adapter、结果 presenter 和结构化错误映射。
- [x] 注册 `migrate plan|apply|status|rollback`；rollback 强制 `--restore-point`，所有命令支持 table/json。
- [x] 命令帮助明确 `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT`、`EDITOR` 的 legacy 作用；输出只显示脱敏路径/摘要/风险。
- [x] CLI 契约测试覆盖 stdout/stderr、退出码、参数错误、JSON envelope 和秘密脱敏。

### P5：文档、spec 与回归

- [x] 更新 README/命令参考/迁移回滚文档、能力矩阵和父任务状态；记录 legacy 与 external 不误分类约束。
- [x] 如实现产生新的跨任务约定，运行 `trellis-update-spec` 更新 backend spec，并同步根 `AGENTS.md` 的可复用环境事实。
- [x] 运行完整质量门；发现设计缺陷时回到 P1-P4 修改规划后再继续，不以测试删减规避风险。

## 验证命令

```bash
test -z "$(gofmt -l cmd internal)"
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/mm-a5-amd64 ./cmd/mm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/mm-a5-arm64 ./cmd/mm
git diff --check
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
```

专项验收需使用隔离环境验证：空环境 plan/apply、有效/无效配置、重复迁移、中断后 status、显式恢复点 rollback、摘要冲突拒绝、权限/路径越界拒绝、旧文件 before/after 摘要和 1.x 读取结果。

## 风险与回滚点

- P1 迁移 SQL 或仓储错误：停止在数据库层，删除未发布的源码迁移并重新运行临时数据库测试；不得改写已有迁移文件。
- P2 快照/原子写入错误：停止接入 daemon，保留旧 `CONFIG_DIR`，仅清理 manager 临时恢复点目录。
- P3 IPC 行为错误：移除新路由注册即可恢复现有 daemon health/status；不改变 socket 路径和协议版本。
- P4 CLI 回归：移除 `migrate` 命令工厂注册，现有无参数 TUI 和 daemon 命令保持可用。
- 任一阶段发现源文件摘要变化或无法识别归属：拒绝写入并创建 `legacy`/`external` 引用，不降级猜测为 managed。

## 开始执行前门禁

- [x] PRD 已完成收敛，显式恢复点 ID、无效配置退出码和活动档案规则已写入设计。
- [x] `design.md` 与本 `implement.md` 已由用户审阅批准。
- [x] 执行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/07-20-legacy-migration-rollback` 后才可编辑 Go 代码。
