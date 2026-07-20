# A5 技术设计：legacy 迁移、兼容层与回滚

## 1. 边界与原则

A5 在现有 A2 SQLite、A3 daemon/IPC、A4 core/generation 之上增加一个 legacy 纵向切片。所有 `migrate` 读写和兼容写操作均在 daemon 内执行；CLI 只负责参数解析、IPC 调用和 table/json 展示。旧 `CONFIG_DIR` 是事实源，A5 不解析后重排 YAML，也不把 legacy 内容写入 managed generation。

当文件归属或进程归属无法可靠判断时，结果只能是 `legacy`/`external` 引用或 `unknown` 风险，不得转换为 `managed`。A5 不主动替换旧 core；新的 supervisor 仍只管理自己启动的进程。

## 2. 包与职责

- `internal/legacy`：发现器、计划模型、恢复点快照、manifest 摘要校验，以及 legacy 兼容 service。通过窄接口依赖 `mihomo` 验证/API，避免向 domain 泄漏 `map[string]any`。
- `internal/store`：新增 `legacy_migrations`（恢复点/迁移 operation）和 `legacy_files`（文件 manifest/current digest）追加迁移及仓储方法；所有 profile/operation 变更使用同一事务。
- `internal/daemon`：装配 legacy service，使用现有 `Coordinator`/request-id 幂等缓存；增加 `/v1/migrations/plan`、`/v1/migrations/apply`、`/v1/migrations/status`、`/v1/migrations/rollback` 路由。
- `internal/app`：增加 `MigrationService` port、结果 DTO 和 daemon IPC adapter。兼容 service 的配置/订阅/白名单端口在本阶段只定义可复用接口，具体 CLI 业务由 A6 接入。
- `internal/cli`：增加 `migrate plan|apply|status|rollback`，rollback 强制要求 `--restore-point <id>`；所有命令支持现有 `--output table|json`、结构化错误和退出码。
- `internal/config`：复用现有 `Load()` 的 `CONFIG_DIR`/`MIHOMO_BIN`/`MIHOMO_API_PORT` 解析；不要把 `CONFIG_DIR` 改成 manager 路径。

## 3. 数据模型与恢复点

新增恢复点记录字段：`id`、`state`、`source_dir`、`profile_id`、`manifest_json`、`created_at`、`updated_at`、`completed_at`、`rolled_back_at`、`error_code`。文件 manifest 对每个允许的相对路径保存 `before_exists`、`before_mode`、`before_size`、`before_sha256`、`expected_exists`、`expected_sha256`、`snapshot_path`。快照内容写入 `manager backups/<restore-point-id>/files/`，目录 `0700`、文件 `0600`；JSON manifest 不包含 URL 内容或配置正文。

apply 流程：

1. 通过 coordinator 获取 `migration.apply` 锁，重新发现并校验路径位于 `CONFIG_DIR` 下。
2. 在 manager 状态库之外先创建快照临时目录，复制允许文件并 fsync，再原子重命名为恢复点目录；记录 before/expected 摘要。
3. 在一个 SQLite 事务中插入恢复点、`legacy` profile（稳定 ID，例如 `legacy-mihomo`，已存在时检查同源路径）和 operation 元数据；若已有成功迁移则返回幂等结果，不创建第二个 profile。
4. 对 `config.yaml` 调用受控 `MIHOMO_BIN -t -d <CONFIG_DIR> -f <CONFIG_FILE>`；空/不存在配置不伪造内容，验证失败使 operation 为 failed、返回 `VALIDATION_FAILED`（退出码 6）并保持 profile 非活动，同时保留快照和旧文件。
5. 成功时将恢复点标记 succeeded；仅当数据库没有活动档案时激活该 legacy profile。不启动或停止旧 core，运行状态只作为脱敏 metadata 记录。

兼容 service 每次写入遵循：读取当前摘要并与 expected 比对 -> 在同目录创建时间戳备份 -> 写临时文件并 fsync/原子替换 -> 验证 -> 更新 `expected_*`。如果当前摘要不等于 expected，返回冲突而不覆盖；若写入失败，恢复该次备份并保持 manifest。订阅 URL、白名单等小文件同样走 manifest 跟踪，秘密只存在受保护快照，不进入日志。

rollback 流程：

1. CLI 必须传 `--restore-point`；daemon 检查恢复点属于当前用户、状态为 succeeded/failed 且未回滚。
2. 对每个 manifest 文件核对当前摘要是否等于 `expected_*`；发现外部修改、路径越界、快照缺失或权限不安全时返回 conflict/permission，不做部分恢复。
3. 在同目录创建 rollback 临时备份后，按 manifest 恢复 before 内容；before 不存在的文件仅在当前与 expected 匹配时删除。
4. 事务更新恢复点为 rolled_back、legacy profile 标记为 inactive/保留引用，记录 operation；不删除用户迁移前已有的文件和 1.x 二进制。

## 4. IPC 与 CLI 契约

请求体仅包含 restore point ID、confirm/request ID 等非秘密字段。响应统一使用 `mm/v1` envelope：plan 返回路径动作、存在性、摘要和风险；apply/status/rollback 返回 restore point、profile、operation 状态。`DAEMON_UNAVAILABLE`、`CONFLICT`、`VALIDATION_FAILED`、`PERMISSION_DENIED` 分别映射现有退出码。敏感字段在 presenter 层再次脱敏，table 不显示 URL/token，json 只输出 `redacted: true` 或摘要。

## 5. 兼容行为与环境变量

legacy service 复用当前 `mihomo.Client` 的订阅解析、白名单规范化和配置备份语义，但进程控制必须通过 A4 所持 process handle；不得把旧 `pgrep/pkill` 逻辑带入 daemon 新路径。`CONFIG_DIR` 是源目录，`MIHOMO_BIN` 只用于验证和显式受控启动，`MIHOMO_API_PORT` 只用于 loopback API 探测，`EDITOR` 只在显式 edit 调用中读取。

## 6. 失败、幂等与回滚边界

- 空目录：plan 成功；apply 创建可查询的非活动 legacy 引用和恢复点，operation failed 并返回退出码 6。
- 无效配置：保留原文件及摘要，profile 非活动、operation failed，允许 status/rollback。
- 中断：阶段写入使用 pending/running/failed 状态；恢复点目录采用临时目录加原子发布，重启后不会把半成品当作可回滚快照。
- 重复迁移：同一 `source_dir` 仅一个活动 legacy profile；返回已有恢复点或明确冲突。
- 外部配置：只创建 external/legacy 引用，validate/read-only 可用，永不自动写回源文件。

## 7. 测试设计

使用 `t.TempDir()` 构造 HOME/XDG/CONFIG_DIR、fake mihomo 可执行文件和临时 Unix socket。覆盖 Good/Base/Bad 矩阵、权限/摘要冲突、路径穿越、取消、中断重启、重复 request ID、秘密脱敏、真实 mihomo 不启动断言；补充 SQLite 迁移/事务回滚和 amd64/arm64 `CGO_ENABLED=0` 构建。
