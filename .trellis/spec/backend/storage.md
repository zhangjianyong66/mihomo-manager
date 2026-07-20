# SQLite 领域存储契约

## Scenario：扩展领域模型或 SQLite schema

### 1. Scope / Trigger

新增或修改 `internal/domain` 的持久化聚合、`internal/store` API、SQLite 表/索引/约束、迁移、权限或恢复行为时，必须遵循本规范。

A2 只提供存储底座。`internal/store.Open` 接收显式数据库路径，不读取 HOME、XDG 或环境变量；默认路径和 daemon 单写者装配由后续任务实现。CLI/TUI 不得直接访问 SQLite。

### 2. Signatures

打开和生命周期：

```go
type OpenOptions struct {
    BusyTimeout time.Duration
}

type SchemaInfo struct {
    Version    int
    Migrated   bool
    BackupPath string
}

func Open(context.Context, string, OpenOptions) (*Store, error)
func (*Store) Close() error
func (*Store) SchemaInfo() SchemaInfo
```

仓储方法首参均为 `context.Context`，输入输出使用 `internal/domain` 类型。当前聚合行为包括 profile CRUD/活动切换、subscription CRUD、按来源列出/事务替换 nodes、operation CRUD/未完成列表和 global/profile settings。

### 3. Contracts

- 驱动固定为 `modernc.org/sqlite v1.36.1`，配套 `modernc.org/libc v1.61.13`；项目最低 Go 1.22，目标构建为 Linux amd64/arm64、`CGO_ENABLED=0`。
- 单个 store 只保留一个打开连接；每个连接启用 `foreign_keys=ON`、有限 `busy_timeout`、`journal_mode=DELETE`、`synchronous=FULL`。
- 状态目录权限为 `0700`；数据库及现存 `-journal`、`-wal`、`-shm` 为 `0600`；数据库文件符号链接被拒绝。
- 时间以 UTC `time.Time` 进入领域层，以 RFC3339Nano 文本持久化，读取后恢复为 UTC；可选时间使用 SQL NULL。
- JSON 字段写入前经领域校验，schema 再以 `json_valid` 约束；URL、node spec、setting value 和 recovery 内容不得进入错误文本。
- nodes 只保存 `subscription_id`，通过 subscription 唯一归属 profile；同一 subscription 内 `remote_key` 唯一，显示名不是身份。
- 所有嵌入式迁移版本连续且只增不降。已提交的 `internal/store/migrations/*.sql` 不得原地修改；历史名称和 SHA-256 checksum 不匹配时拒绝打开。
- 待执行迁移作为一个事务批次提交。升级已有 schema 前使用 SQLite online backup 在数据库同目录创建 `0600` 恢复点；不实现 schema downgrade。
- `Close` 幂等；关闭后的仓储调用返回 `ErrClosed`，不得自动重开。

### 4. Validation & Error Matrix

| 条件 | 可识别错误 |
|---|---|
| 空路径、非法领域值、非 UTC 时间、无效 JSON/归属 | `ErrInvalid` |
| ID/key 不存在 | `ErrNotFound` |
| 唯一/外键约束、busy/locked、乐观 revision 冲突 | `ErrConflict` |
| store 已关闭 | `ErrClosed` |
| 数据库版本高于二进制 | `ErrSchemaTooNew` |
| 迁移缺失、版本断裂、名称/checksum 漂移 | `ErrMigrationDrift` |
| quick check、foreign key check 或 SQLite 损坏错误 | `ErrCorrupt` |
| 文件符号链接或权限无法收紧 | `ErrPermission` |

底层错误使用 `%w` 保留。动作上下文可包含稳定 ID，但不能包含订阅 URL、节点 JSON、设置值或恢复数据。

### 5. Good/Base/Bad Cases

- Good：旧 v1 schema 打开时先生成一致性恢复点，再原子应用 v2；返回 `SchemaInfo{Version: 2, Migrated: true, BackupPath: ...}`。
- Base：最新 schema 重复打开不执行迁移、不创建备份，仓储 round trip 保留 ID、UTC 时间、可选值和 JSON。
- Bad：来源节点替换中新 ID 与另一来源冲突时，目标来源旧节点回滚，另一来源完全不变。
- Bad：未知数据库、未来 schema、checksum 漂移、损坏文件或数据库符号链接直接拒绝打开，不接管或修复用户数据。

### 6. Tests Required

- 所有 store 测试使用 `t.TempDir()` 和显式路径，不读取真实 HOME、配置、数据库或进程。
- 领域表驱动测试覆盖空 ID、枚举、模式/路径、JSON、UTC、归属和数值边界。
- schema 测试断言真实表、索引、外键、检查约束、单活动档案和级联边界，不只比较 SQL 字符串。
- 迁移测试覆盖空库、重复打开、旧 schema 升级与备份、未来版本、checksum 漂移和失败批次回滚。
- 仓储测试覆盖 CRUD、global/profile setting、operation profile `ON DELETE SET NULL`、节点来源隔离、空集合与失败回滚。
- 完成后运行 `go test ./...`、`go test -race ./...`、`go vet ./...` 和 Go 1.22.12 的 amd64/arm64 无 CGO 构建。

### 7. Wrong vs Correct

#### Wrong

```go
func OpenDefault() (*Store, error) {
    return Open(context.Background(), os.Getenv("HOME")+"/.local/share/mihomo-manager/state.db", OpenOptions{})
}

// 已发布后修改 0001_core_sources.sql。
```

这会让基础设施包偷偷拥有环境装配，并使旧数据库的迁移历史无法验证。

#### Correct

```go
store, err := store.Open(ctx, injectedDatabasePath, store.OpenOptions{})
// schema 修复通过新增 0003_<name>.sql 完成，历史迁移保持字节不变。
```

路径由后续 daemon/config 装配注入，迁移历史保持追加式和可校验。
