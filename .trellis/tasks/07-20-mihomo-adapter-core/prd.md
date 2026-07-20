# 实现 mihomo adapter 与 core 生命周期

## Goal

在 A1 应用契约、A2 SQLite 领域存储和 A3 daemon/IPC 基础上，建立由 daemon 独占的 mihomo core 运行闭环：生成或引用配置、验证候选、启动并确认就绪、停止准确的受管进程，以及在档案切换失败时恢复上一可用实例。

本任务只建立内部 adapter、generation 和 daemon 生命周期能力。profile/subscription 业务 CLI、legacy 自动迁移和 TUI 接入由后续 A5-A7 交付。

## Background

- 当前 `internal/mihomo/client.go` 同时承担进程查找、`pkill`、HTTP API、YAML 变换、订阅和日志；`Stop()` 按命令行模式全局杀进程，不能作为 daemon 的受管进程语义。
- A3 daemon 已拥有单实例锁、SQLite、Unix socket、operation coordinator 和 graceful shutdown，但明确不启动 core。
- A2 已有 profile、node、operation 和 settings 仓储。managed profile 不保存源配置路径；external/legacy profile 必须保存配置路径。
- 父任务要求生成配置不得覆盖 `~/.config/mihomo/config.yaml`，external 配置只读，core 使用独立 session，失败启动必须恢复旧实例或进入明确 failed 状态。

## Requirements

### R1：最小 core 边界

- 新建不依赖 mihomo DTO 的 `internal/core` 契约，定义由当前需求驱动的 `Adapter`、`RuntimeClient`、`Process`、配置快照和运行状态。
- `internal/core` 可依赖 `internal/domain`，不得导入 `internal/mihomo`、SQLite、CLI 或 TUI。
- 领域层和 daemon 接口不得暴露 mihomo 的 `map[string]any` 或原始 external-controller 响应。

### R2：mihomo 子组件拆分

- `internal/mihomo` 分离配置渲染/验证、进程启动/停止和 external-controller 客户端；保留现有 `Client` 供后续兼容迁移使用。
- 新进程必须以显式二进制、配置目录、配置文件、日志路径和 controller endpoint 启动，并保持 Linux `Setsid` 行为。
- 停止只作用于 adapter 返回并由 daemon 持有的 `Process`，先发送 `SIGTERM`，超时后才 `SIGKILL`；不得使用 `pkill`/`pgrep` 识别或终止外部进程。

### R3：配置 generation

- manager 路径新增独立 generation 根目录和 core runtime 路径，全部由 `internal/config` 解析和注入。
- managed profile 从类型化快照确定性渲染最小 mihomo YAML；同一输入必须产生相同内容摘要。
- 每个 generation 使用不可变目录，至少包含 `config.yaml` 和不含秘密值的 `metadata.json`；目录权限 `0700`、文件权限 `0600`。
- 候选先写入同一文件系统临时目录，完成静态验证和 mihomo 原生 `-t` 验证后再原子发布；失败不得产生可激活 generation。
- external/legacy 配置只读取并计算 SHA-256 摘要，原生验证与启动前均检查摘要；绝不写入、备份或改名源文件。

### R4：验证和就绪

- 静态验证拒绝空配置、非本地 external-controller、缺失可用代理/分组目标和明显端口冲突；错误不得包含节点秘密。
- mihomo 原生验证使用受控命令、上下文超时和明确错误类别，保持 `-t -d <dir> -f <file>` 参数契约。
- 启动后通过类型化 `RuntimeClient` 查询 `/version` 与 `/configs`（或等价健康端点）确认 controller 就绪，并验证启动进程未提前退出。
- API 客户端统一超时、非 2xx 状态码、响应体上限和 JSON 解码，不允许 endpoint 指向非 loopback 地址。

### R5：daemon 生命周期与恢复

- daemon 拥有单个 core supervisor；启动、停止和切换均受 A3 operation coordinator 串行化。
- 每次切换写入 operation 记录和恢复信息，阶段至少覆盖 prepare、validate、stop-old、start-new、ready、commit、rollback。
- 新档案就绪后才提交活动 profile/runtime 元数据。启动或就绪失败时尝试重启旧 generation；恢复成功记录 `rolled_back`，恢复失败记录 `failed` 并暴露明确 core failed 状态。
- daemon 正常退出时只停止本次持有的受管 core；daemon 启动不自动启动 core。本阶段不实现崩溃后的 PID 重新附着或自动重启退避。

### R6：兼容、安全与测试

- 保留旧 `mihomo.Client` 的 TUI 行为，不在本任务迁移现有业务方法。
- 所有自动化测试使用 `t.TempDir()`、假 core 可执行文件或当前测试进程、`httptest.Server` 和显式 manager 路径；不得读取/改写真实用户配置或启动真实 mihomo。
- 受控真实 mihomo `-t` 仅作为可跳过的契约测试或手工验证，不成为普通 `go test ./...` 对宿主安装的隐式依赖。
- 保持 Go 1.22、Linux amd64/arm64 和 `CGO_ENABLED=0` 构建。

## Acceptance Criteria

- [x] AC1（R1/R2）：`internal/core` 契约和 mihomo adapter 编译通过，契约外没有 mihomo `map[string]any`；现有 TUI 测试保持通过。
- [x] AC2（R2）：假 core 验证启动参数、独立 session、日志重定向、SIGTERM 优先和超时 SIGKILL；测试证明不会终止无关进程。
- [x] AC3（R3）：managed golden 配置确定性一致，generation 原子发布且权限为 `0700/0600`；验证失败时活动 generation 和旧文件摘要不变。
- [x] AC4（R3）：external/legacy 测试证明源文件只读，验证与启动之间摘要变化会停止操作。
- [x] AC5（R4）：配置静态验证、原生验证超时/退出码、controller 非 2xx/超大响应/非 loopback endpoint 和就绪超时均有测试。
- [x] AC6（R5）：切换成功只保留新实例；新实例启动或就绪失败时恢复旧实例；恢复失败进入 `failed`，operation 阶段、状态和恢复信息可审计。
- [x] AC7（R5/R6）：daemon 启动不自动启动 core，正常退出仅清理其持有进程；无 daemon 时不存在直接控制 core 的新回退路径。
- [x] AC8（R6）：`go test ./...`、`go test -race ./...`、`go vet ./...`、amd64/arm64 无 CGO 构建和 `git diff --check` 通过。

## Out Of Scope

- 不新增 `mm core`、`mm profile`、`mm config` 等业务命令或 IPC 路由。
- 不迁移现有用户的 `~/.config/mihomo`，不修改 legacy 配置，不自动创建 profile。
- 不实现订阅抓取/解析、完整多来源节点生成、路由/DNS 覆盖层或 managed 转换。
- 不实现 TUI 应用服务迁移、systemd core service、TUN、系统代理、多 core 或远程 TCP API。
- 不实现 daemon 崩溃后的外部进程重新附着、指数退避重启或安装器默认启动 core。
