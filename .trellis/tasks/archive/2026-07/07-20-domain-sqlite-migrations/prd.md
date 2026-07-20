# 领域模型、SQLite 与迁移框架

## Goal

为 2.0 Alpha 建立独立于 mihomo YAML、CLI 和 TUI 的规范化领域模型与持久化基础，使后续 daemon 能作为唯一写者可靠管理档案、订阅来源、节点、长操作和设置。数据库必须在 Ubuntu/Debian amd64、arm64 上以 `CGO_ENABLED=0` 构建，并为后续 schema 演进提供可重复、失败不破坏数据的迁移机制。

本任务只交付领域与存储基础设施，不迁移真实 1.x 配置、不接入 daemon/CLI/TUI，也不生成或启动 mihomo 配置。

## Background

- 父任务已决定使用 `database/sql` 与无 CGO SQLite，默认数据库目标为 `~/.local/share/mihomo-manager/state.db`；daemon 将在后续 A3 成为唯一写者。
- A1 已建立 `internal/domain` 的稳定 ID、core 类型/状态，以及 `internal/app` 的应用 ports；A2 依赖这些边界，不得让 SQLite DTO 或 mihomo 原始字段泄漏到应用契约。
- 2.x 档案模式为 `managed`、`external`、`legacy`；`legacy` 只能由后续迁移流程创建，`external` 永不写回源文件。
- 订阅和节点必须使用稳定 ID。一次订阅更新只事务替换自身来源节点，失败时保留上一成功版本，其他来源不受影响。
- 状态目录必须为 `0700`，数据库及其 SQLite 辅助文件必须为 `0600`；自动化测试只能使用 `t.TempDir()`。
- SQLite 驱动选择 `modernc.org/sqlite v1.36.1`：模块声明 Go 1.21，主许可证为 BSD-3-Clause；上游示例已通过 Go 1.22.12 的 amd64/arm64 无 CGO 探针。项目集成后仍需复验最终依赖闭包和成品体积，用户接受的 amd64 未压缩成品上限为 25 MiB。

## Requirements

### 领域模型

- R1：扩展 `internal/domain`，定义档案模式、档案、订阅、节点、操作和设置所需的框架无关类型、枚举及校验；领域层不得导入 SQLite、mihomo、Cobra 或 Bubble Tea 类型。
- R2：所有持久化聚合使用稳定字符串 ID；展示名称不得作为身份或外键。
- R3：档案模式严格限制为 `managed`、`external`、`legacy`，core 类型继续复用 A1 的 `CoreType`；存储层不负责决定谁可以创建 `legacy`，该产品规则由后续迁移/应用服务执行。
- R4：节点必须归属一个档案和一个订阅来源；同一来源内的远端身份键必须唯一，来源之间允许显示名和远端身份键重复。
- R5：时间统一以 UTC 持久化并恢复为 UTC；可选时间使用显式空值语义，不使用零字符串代表缺失。

### SQLite 与 schema

- R6：使用 `database/sql` 和固定版本的无 CGO SQLite 驱动，项目在 Go 1.22、Linux amd64/arm64、`CGO_ENABLED=0` 下可构建；记录驱动和直接/间接依赖许可证审计结果。用户已接受引入 SQLite 的体积成本，amd64 未压缩 `mm` 成品上限为 25 MiB。
- R7：新增 `internal/store`，由显式路径打开数据库；包本身不得读取真实 HOME 或环境变量。默认路径接入留给后续 daemon/config 装配任务。
- R8：首个 schema 只包含 `schema_migrations`、`profiles`、`subscriptions`、`nodes`、`operations` 和 `settings`，并建立必要主键、外键、检查约束、唯一约束和索引。routes、DNS、覆盖层、流量统计和业务日志通过后续迁移增加。
- R9：每个档案最多一个活动标记；删除档案级联其订阅、节点和档案设置，删除订阅只级联自身节点，不影响其他来源。
- R10：启用并验证 SQLite 外键；配置有限 busy timeout。连接池按未来 daemon 单写者模型限制为一个打开连接，避免 PRAGMA 只作用于部分连接。
- R11：默认使用 rollback journal，而不是 WAL，减少敏感辅助文件及权限面；如后续性能证据要求 WAL，必须另行审计 `-wal`/`-shm` 权限、备份和崩溃恢复语义。

### 迁移、事务与恢复

- R12：迁移内嵌在二进制中，按单调整数版本执行；每次迁移与版本记录位于同一事务，重复执行幂等地到达同一最新 schema。
- R13：数据库 schema 比当前程序更新时拒绝打开并返回可识别错误，不尝试降级或忽略未知版本。
- R14：迁移前对已有数据库创建同目录一致性备份；新建空库不要求备份。迁移失败保留原数据库可重新打开，并清理未完成的临时备份。
- R15：打开数据库时执行快速完整性检查；检测到损坏、权限不合规或无法启用外键时失败关闭，不继续提供仓储服务。
- R16：提供受控事务入口或等价的工作单元，使订阅元数据与其节点集合能原子替换；校验/写入任一步失败必须整体回滚。
- R17：`operations` 只记录数据库/文件/进程跨边界操作的阶段、状态、重试与恢复元数据，不假装 SQLite 事务能覆盖外部副作用。

### 仓储行为

- R18：实现 profile、subscription、node、operation 和 settings 的最小仓储行为，并使用领域模型作为输入输出；SQL 错误需包装动作上下文，不能包含订阅 URL 或节点凭据。
- R19：订阅来源替换必须校验所有节点归属同一档案/订阅，稳定 ID 和远端身份键冲突时失败回滚；空集合表示成功清空该来源。
- R20：并发写入在 busy timeout 内串行化或返回明确错误，不产生部分提交；关闭后的 store 不得静默重开。

## Acceptance Criteria

- AC1（R1-R5）：领域模型表驱动测试覆盖合法/非法档案模式、空 ID、归属关系、UTC 时间和稳定身份约束，且 `internal/domain` 不依赖基础设施包。
- AC2（R6）：固定 SQLite 驱动版本；`CGO_ENABLED=0` 的 Linux amd64、arm64 构建通过，并记录加入驱动前后的二进制体积；amd64 未压缩 `mm` 不超过 25 MiB；许可证审计不存在与 MIT 二进制分发冲突的依赖。
- AC3（R7-R11）：在 `t.TempDir()` 中创建数据库后，目录/数据库/现存 SQLite 辅助文件权限分别为 `0700`/`0600`，外键处于启用状态，连接数和 journal 模式符合设计。
- AC4（R8-R10）：schema 契约测试验证首批表、关键索引、外键、检查约束、单活动档案约束和级联删除边界。
- AC5（R12-R15）：测试覆盖空库初始化、重复打开、旧 schema 升级、未知新 schema 拒绝、迁移中途失败回滚、迁移备份和损坏数据库拒绝打开。
- AC6（R16/R18-R20）：仓储测试覆盖各聚合的最小 CRUD、事务提交/回滚、关闭行为和受控并发写入。
- AC7（R4/R9/R16/R19）：订阅节点集合替换测试证明成功时只替换目标来源，失败时目标来源保留旧数据，其他来源始终不变，删除订阅只删除自身节点。
- AC8（全部）：`gofmt`、`go test ./...`、`go test -race ./...`、`go vet ./...` 和双架构无 CGO 构建全部通过；测试未读取或修改真实用户数据库、配置或进程。

## Out Of Scope

- 真实 `~/.config/mihomo` 到 SQLite 的发现、备份、迁移、转换和回滚；这些属于 A5。
- daemon、Unix socket、systemd user service、请求幂等和多客户端协议；这些属于 A3。
- mihomo 配置生成、运行目录、core 验证和进程生命周期；这些属于 A4。
- CLI/TUI 业务命令接线，以及修改现有 `internal/mihomo.Client` 行为。
- routes、DNS、core overrides、流量统计、日志、备份包和远端同步的完整 schema/实现。
- schema 降级迁移；回退依靠迁移前数据库备份和旧版不读取新数据库。

## Dependencies

- 依赖已完成并归档的 A1「CLI 与应用服务契约」。
- A3 可并行开发无状态 IPC，但持久状态接入依赖 A2 的仓储契约。
- A4、A5 依赖 A2 的领域模型、迁移器和事务仓储。
