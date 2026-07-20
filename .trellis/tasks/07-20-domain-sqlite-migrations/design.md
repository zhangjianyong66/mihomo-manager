# 技术设计：领域模型、SQLite 与迁移框架

## 1. 设计目标与边界

A2 建立后续 daemon、配置生成和 legacy 迁移共同依赖的数据底座。它不直接改变用户可见命令或当前 mihomo/TUI 行为。

依赖方向固定为：

```text
internal/app（后续调用） -> internal/domain <- internal/store
                                              |
                                              v
                                      database/sql + SQLite
```

- `internal/domain` 只依赖 Go 标准库，拥有领域值和校验。
- `internal/store` 依赖领域模型与 SQLite 驱动，拥有 SQL、迁移和持久化映射。
- `internal/app` 不导入 `internal/store` 的行类型或驱动错误；A3/A4 按用例装配具体 store。
- `internal/mihomo`、`internal/tui` 和 `internal/cli` 在 A2 不接入数据库。

## 2. 驱动与运行参数

固定 `modernc.org/sqlite v1.36.1`。详细证据见 `research/sqlite-driver.md`。

store 打开后只保留一个连接：

```go
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
```

在该连接上设置并读回验证：

```text
PRAGMA foreign_keys = ON
PRAGMA busy_timeout = 5000
PRAGMA journal_mode = DELETE
PRAGMA synchronous = FULL
```

`busy_timeout` 可通过 `OpenOptions` 在测试中缩短，但生产默认 5 秒。A2 不使用 WAL；单用户 daemon 的写负载不需要先承担 WAL 辅助文件、权限和备份复杂度。

## 3. 领域模型

### 3.1 值类型

复用 A1 的 `ProfileID`、`SubscriptionID`、`NodeID` 和 `CoreType`，新增：

- `OperationID`
- `ProfileMode`：`managed`、`external`、`legacy`
- `OperationState`：`pending`、`running`、`succeeded`、`failed`、`rolling_back`、`rolled_back`

所有 ID 继续只验证非空；ID 生成和 legacy 的确定性映射由调用方负责。A2 不强制 UUID，以便 A5 用内容摘要生成可重复迁移 ID。

### 3.2 聚合

```text
Profile
  ID, Name, Mode, CoreType, ConfigPath?, Active, Revision, CreatedAt, UpdatedAt

Subscription
  ID, ProfileID, Name, URL, Enabled, ETag?, LastModified?,
  LastAttemptAt?, LastSuccessAt?, CreatedAt, UpdatedAt

Node
  ID, SubscriptionID, RemoteKey, Name, Protocol, Spec, Position,
  CreatedAt, UpdatedAt

Operation
  ID, ProfileID?, Kind, State, Phase, Attempt, Recovery, ErrorCode?,
  CreatedAt, UpdatedAt

Setting
  ProfileID?, Key, Value, UpdatedAt
```

- `ConfigPath` 仅用于 `external`/`legacy`，`managed` 必须为空。
- `Subscription.URL` 与 `Node.Spec` 可能含秘密，store 错误不得格式化它们。
- `RemoteKey` 是来源内部稳定身份，由订阅解析器提供；展示名可重复。
- `Spec` 和设置/恢复元数据使用有效 JSON 保存，但不保存 mihomo 原始 YAML DTO。
- `ProfileID == nil` 的设置为全局设置；operations 的空 ProfileID 表示全局操作。
- 所有时间在领域入口规范为 UTC，SQLite 以 RFC3339Nano 文本保存。

## 4. Schema

首个可用 schema 由两个真实迁移组成，以便从一开始验证升级路径。

### 4.1 迁移元数据

`schema_migrations`：

| 字段 | 约束 |
|---|---|
| `version` | INTEGER PRIMARY KEY，正数 |
| `name` | TEXT NOT NULL |
| `checksum` | TEXT NOT NULL，内嵌 SQL 的 SHA-256 |
| `applied_at` | TEXT NOT NULL，UTC RFC3339Nano |

runner 把“表不存在且无用户表”识别为版本 0；已有用户表但没有迁移元数据视为未知数据库并拒绝接管。

### 4.2 迁移 0001：核心来源模型

`profiles`：

- `id` 主键，`name` 非空，`mode`/`core_type` 检查约束。
- `config_path` 与模式联动：managed 为空，external/legacy 非空。
- `active` 为 0/1；partial unique index 保证全库最多一个 active profile。
- `revision >= 1`，为后续乐观并发控制保留。

`subscriptions`：

- `id` 主键，`profile_id` 外键引用 profile 并 `ON DELETE CASCADE`。
- 名称、URL 非空，enabled 为 0/1。
- ETag/Last-Modified/最后尝试与最后成功时间允许为空。
- 索引覆盖 `profile_id` 和常用排序。

`nodes`：

- `id` 主键，`subscription_id` 外键并 `ON DELETE CASCADE`。
- `remote_key`、名称、协议、JSON spec 非空，position 非负。
- `UNIQUE(subscription_id, remote_key)`；不对展示名设唯一约束。
- 索引覆盖 `(subscription_id, position, id)`，保证确定性读取顺序。

节点无需重复保存 profile_id；它通过 subscription 唯一归属 profile，避免两个外键字段漂移。

### 4.3 迁移 0002：操作与设置

`operations`：

- `id` 主键，可选 `profile_id` 使用 `ON DELETE SET NULL`，保留故障/恢复记录。
- kind、state、phase 非空，state 有检查约束，attempt 非负。
- recovery JSON 非空且有效，error_code 可空。
- `(state, updated_at)` 索引支持 daemon 恢复未完成操作。

`settings`：

- 可选 `profile_id` 使用 `ON DELETE CASCADE`；key 非空，value JSON 有效。
- 全局设置用 `profile_id IS NULL` 表达。
- 两个 partial unique index 分别约束全局 key 和 `(profile_id, key)`，避免 SQLite 的 NULL 唯一语义产生重复全局设置。

A2 明确不创建 routes、DNS、overrides、traffic 或 log 表。

## 5. Store API

公开入口接收显式数据库路径：

```go
type OpenOptions struct {
    BusyTimeout time.Duration
}

func Open(ctx context.Context, path string, opts OpenOptions) (*Store, error)
func (s *Store) Close() error
func (s *Store) SchemaInfo() SchemaInfo
```

`SchemaInfo` 只报告当前版本、是否发生迁移和本次恢复点路径；不包含数据库内容。

仓储按聚合拆文件，但挂在同一个 `Store` 上，避免为小项目引入 ORM 或泛型 repository：

- profile：create/get/list/update/set-active/delete
- subscription：create/get/list/update/delete
- node：list-by-subscription、replace-subscription-nodes
- operation：create/get/update/list-unfinished
- setting：set/get/delete/list-profile

所有方法首参是 `context.Context`，输入输出是 `domain` 类型。`sql.Row`、nullable SQL 类型和 driver error 只存在于 store 内部。

## 6. 打开与权限流程

```text
验证显式路径
  -> 创建/收紧父目录为 0700
  -> Lstat 拒绝数据库文件符号链接
  -> 以 O_CREATE|O_EXCL 预建新数据库为 0600
  -> database/sql 打开并限制单连接
  -> 设置并验证 PRAGMA
  -> quick_check
  -> 读取/验证迁移历史与 checksum
  -> 需要升级时创建恢复点
  -> 单事务应用全部待执行迁移
  -> foreign_key_check + quick_check
  -> 收紧并审计数据库/现存辅助文件为 0600
```

目录或文件已有更宽权限时先尝试 `chmod` 收紧，再读回验证；无法收紧才返回权限错误。不会读取 HOME、XDG 或环境变量，也不会触碰用户默认路径。

## 7. 迁移与恢复点

迁移 SQL 使用 `go:embed`，文件名按 `0001_name.sql` 排序。启动时验证版本连续、名称唯一并计算 SHA-256；已应用记录的名称或 checksum 与二进制不一致时拒绝继续，防止历史迁移被改写。

所有待执行迁移在一个 SQLite 事务中应用：

```text
BEGIN
  migration N SQL
  INSERT schema_migrations(N, name, checksum, time)
  ...
COMMIT
```

任一步失败则整个批次回滚，数据库仍保持升级前 schema。schema 高于二进制最新版本时返回 `ErrSchemaTooNew`，绝不自动降级。

已有非空数据库升级前，使用 modernc online backup API 创建同目录临时文件：

1. 以 `0600` 独占预建临时目标。
2. 在当前 `database/sql.Conn.Raw` 上逐页复制。
3. 对恢复点执行 quick check。
4. `fsync` 文件后原子 rename 为 `.pre-v<old>-<UTC>.bak`，再同步目录。
5. 任一步失败删除未完成临时文件，不开始迁移。

成功恢复点保留，不由 A2 自动删除。未来 A5/更新任务负责保留策略和用户可见回滚命令。

## 8. 关键事务

### 8.1 活动档案切换

单事务先确认目标存在，再清除旧 active 并设置目标 active；任一步失败回滚。partial unique index是最终并发保护。A2 只修改持久标记，不启动/停止 core；真正“先验证新配置、失败恢复旧实例”属于 A4 daemon 编排。

### 8.2 订阅节点替换

```text
校验 subscription 与所有 node
  -> BEGIN
  -> 再读取 subscription，确认目标仍存在
  -> DELETE nodes WHERE subscription_id = ?
  -> 按 position/ID 插入新集合
  -> 更新 subscription 的成功元数据
  -> COMMIT
```

任何空 ID、归属不一致、重复 remote key、全局 node ID 冲突或 SQL 错误都回滚；传入空集合表示明确清空该来源。其他 subscription 的节点没有写路径，因此保持不变。

### 8.3 跨系统操作记录

operations 只持久化 daemon 将来执行外部步骤所需的状态。store 允许更新 phase/state/attempt/recovery，但不实现进程或文件补偿；状态转换规则由应用服务集中拥有，避免 SQL 与 daemon 各自维护一套状态机。

## 9. 错误与关闭语义

store 提供可用 `errors.Is` 判断的哨兵：

- `ErrInvalid`
- `ErrNotFound`
- `ErrConflict`
- `ErrClosed`
- `ErrSchemaTooNew`
- `ErrMigrationDrift`
- `ErrCorrupt`
- `ErrPermission`

底层错误使用 `%w` 保留，但上层消息只描述动作和稳定 ID，不包含订阅 URL、node spec 或设置值。A3/A6 再把 store 错误转换为 A1 的 `app.Error`。

`Close` 幂等；关闭后的所有仓储方法返回 `ErrClosed`，不自动重开。

## 10. 测试与兼容

- 所有 store 测试使用 `t.TempDir()` 和显式路径。
- schema 测试通过 `sqlite_master`、`PRAGMA foreign_key_list/index_list` 和实际约束写入验证，不只比较 SQL 文本。
- 迁移测试使用包内可注入 migration 列表制造失败，不向生产 API 暴露任意 SQL。
- 并发测试用 goroutine 同时写入，验证单连接串行化、唯一约束和 context timeout。
- 崩溃近似测试在未提交事务后关闭连接并重开，验证无部分数据；不通过杀死测试进程制造不稳定用例。
- 完整测试包含 race、vet、Go 1.22.12 双架构无 CGO 构建和 25 MiB 体积门。

A2 新数据库尚未被现有 1.x 读取，因此回滚代码只需停止使用并保留数据库；schema 升级回滚使用自动恢复点。A2 不改 `~/.config/mihomo`，所以当前用户行为可直接回到上一提交。
