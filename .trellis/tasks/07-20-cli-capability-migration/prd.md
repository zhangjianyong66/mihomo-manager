# A6：现有能力 CLI 等价迁移

## Goal

在不改写 legacy 用户配置语义的前提下，把 1.x TUI 已提供的 mihomo 管理能力迁移为稳定、可脚本化的 CLI。CLI 通过用户级 daemon IPC 调用应用服务，TUI 后续复用同一服务；用户可以在本地终端、SSH 和脚本中完成服务控制、配置校验、节点/代理组操作、订阅维护、白名单/路由操作、配置备份恢复、日志查看和测速。

## Background and confirmed facts

- A1 已建立 `internal/app` 服务端口、CLI 输出/错误/退出码契约和 `--output table|json` 基础能力。
- A2-A4 已提供 SQLite/daemon/Unix socket/core adapter/config generation 基础；A5 已提供 legacy 发现、迁移恢复点、兼容 service 和回滚。
- 当前 CLI 只有 `daemon` 与 `migrate` 稳定命令；`internal/daemon/server.go` 目前只暴露 `/v1/status` 与迁移 API。
- 既有业务能力位于 `internal/mihomo.Client`：服务启停/重启/重载、配置测试、代理组/节点查询与选择、节点测速流、订阅 URL 读写与更新、配置备份/恢复/编辑、日志 tail/follow、白名单读写、CN 路由预设和路由诊断。
- `internal/app/ports.go` 已定义对应的 Core、Node、Group、Subscription、Route、Config、Log service 形状，但尚未接入 daemon IPC 和 Cobra 命令。
- A6 只对齐当前已有/部分能力；managed 多档案 CRUD、完整导入导出、新 DNS/TUN/连接/流量/更新/同步能力属于后续里程碑。

## Requirements

### R1：core 与配置

- 提供 `mm core status|start|stop|restart|reload|logs`，以及 `mm config validate`。
- 所有写操作通过 daemon；daemon 不可用时返回既定 `DAEMON_UNAVAILABLE`/退出码 5，不直接写文件或控制 core。
- `core logs` 支持有限行数查询和 follow；follow 可被 SIGINT/上下文取消，正常结束不产生错误。

### R2：组、节点与测速

- 提供 `mm group list|show|select` 和 `mm node list|test|select`。
- 保留 legacy mihomo 代理组语义；输出必须包含来源/组标识，节点选择失败不得改变当前选择。
- `node test` 支持并发、节点数限制、取消和超时，流式输出支持 `text|ndjson`；NDJSON 不混入人类进度文本。

### R3：legacy 订阅

- 提供 `mm subscription show|set|update`，命令帮助明确这是单来源 legacy 兼容语义。
- `set`/`update` 沿用 A5 兼容 service 的摘要检查、临时备份、失败恢复和配置校验；更新失败保留上一成功配置。
- URL、令牌、UUID、密码和完整节点 URI 默认脱敏；只有显式 `--show-secrets` 才允许展示敏感值。

### R4：legacy 白名单与路由

- 提供 `mm route whitelist list|add|edit|remove`、CN 路由预设和 `route diagnose <target>`。
- 白名单写操作具有幂等/去重语义，失败不得损坏 `config.yaml` 或白名单文件。
- 路由诊断结果至少包含输入、匹配规则、目标策略和当前节点；未知/无效输入使用结构化错误。

### R5：legacy 配置文件

- 提供 `mm config backup|restore|edit`，明确作用于已迁移的 legacy 配置。
- restore/edit 前后执行配置验证和权限检查；编辑器退出失败或验证失败时恢复临时备份，不自动覆盖外部/managed 配置。

### R6：统一接口与兼容性

- 查询命令支持稳定 `--output table|json`；流式命令支持 `--output text|ndjson`。
- JSON 使用 `apiVersion/kind/data/warnings` envelope；错误使用既有结构化错误对象，退出码遵循 A1 契约。
- daemon API 使用 `/v1/` 版本路径、Unix socket 和 request ID；不新增 TCP 管理接口。
- CLI、TUI 和 legacy 兼容层不得各自实现写入逻辑，业务行为必须复用同一应用服务。
- 命令默认作用于唯一活动 legacy profile；在未来存在多个可选 profile 时通过可选 `--profile` 消除歧义，A6 不要求所有命令强制显式指定。

## Acceptance Criteria

- [x] AC1：`mm core status|start|stop|restart|reload|logs`、`mm config validate` 在隔离 HOME、fake mihomo 和真实 daemon/Unix socket 下完成成功、失败、取消和 daemon 不可用测试。
- [x] AC2：组/节点 list/show/select 与 node test 能完成原 TUI 的查询、选择和测速；测速 text/NDJSON 输出可被脚本逐行消费，取消不会泄漏 goroutine 或改变未完成结果。
- [x] AC3：legacy subscription、whitelist、route diagnose/preset、config backup/restore/edit 命令覆盖成功、上游失败、校验失败和回滚；迁移前后配置摘要和权限断言保留。
- [x] AC4：所有查询均有 table/json 契约测试，流式命令有 text/ndjson 契约测试；错误只写 stderr，JSON 模式 stdout 不混入日志或进度文本。
- [x] AC5：敏感信息脱敏测试覆盖订阅 URL、URI、密码、UUID、日志和结构化错误；`--show-secrets` 未显式提供时不得泄露。
- [x] AC6：daemon 路由、应用服务 adapter 和 Cobra 命令均有单元测试；`go test ./...`、`go test -race ./...`、`go vet ./...`、无 CGO 构建和 `git diff --check` 通过。
- [x] AC7：README、CLI contract、能力矩阵、根 `AGENTS.md` 和 Trellis spec 更新 A6 已实现命令、legacy 限制、环境变量和验证方式；不声称 Beta/排除能力已完成。

## Out of scope

- 不实现 managed 多档案/多订阅 CRUD、legacy 到 managed 转换、完整节点导入导出或多 core。
- 不实现 TUN、系统代理/PAC、连接/流量、应用/core/规则更新、备份加密、WebDAV 同步和二维码。
- 不重写 v2rayN 源码，不改变 `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT`、`EDITOR` 的既有含义。
- 不让 CLI 绕过 daemon 直接写 SQLite、配置文件或调用 mihomo API。
