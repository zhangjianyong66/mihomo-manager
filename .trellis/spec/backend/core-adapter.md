# Core adapter、generation 与进程托管契约

## 场景：由 daemon 托管 mihomo core

### 1. 范围 / 触发条件

修改 `internal/core`、`internal/mihomo/adapter*.go`、`process*.go`、`render.go`、`runtime.go`、`internal/daemon/core_manager.go`，或新增 generation/runtime 路径时，必须遵守本规范。目标是保证 daemon 只管理自己启动的单个 core、managed 配置原子发布、external/legacy 配置只读，以及档案切换失败可恢复。

现有 `internal/mihomo/client.go` 是 1.x TUI 兼容层；它的 `pgrep/pkill` 行为不得被新 daemon/CoreManager 调用。

### 2. 签名

核心边界：

```go
type Adapter interface {
    Type() domain.CoreType
    Render(context.Context, ProfileSnapshot) (RenderedConfig, error)
    Validate(context.Context, RuntimeSpec) error
    Start(context.Context, RuntimeSpec) (Process, error)
    Runtime(string) (RuntimeClient, error)
}

type Process interface {
    PID() int
    Done() <-chan error
    Stop(context.Context) error
}

type RoutingRuntime interface {
    Mode(context.Context) (domain.RoutingMode, error)
    SetMode(context.Context, domain.RoutingMode) error
    Reload(context.Context, string) error
    LoadedRules(context.Context) ([]mihomo.RuntimeRule, error)
    ConnectionCount(context.Context) (int, error)
    CloseConnections(context.Context) error
    SelectedProxy(context.Context, string) (string, error)
}
```
文件与状态入口：

```go
func core.NewGenerationStore(core.GenerationStoreOptions) *core.GenerationStore
func (*core.GenerationStore) Prepare(context.Context, core.ProfileSnapshot, core.Adapter) (core.RuntimeSpec, error)
func (*core.GenerationStore) CheckSource(core.RuntimeSpec) error
func (*core.GenerationStore) MarkActive(core.RuntimeSpec) error
func (*daemon.CoreManager) Activate(context.Context, core.ProfileSnapshot) error
func (*daemon.CoreManager) Close(context.Context) error
```

SQLite 活动档案补偿入口：

```go
func (*store.Store) ActiveProfileID(context.Context) (domain.ProfileID, bool, error)
func (*store.Store) RestoreActiveProfile(context.Context, *domain.ProfileID, time.Time) error
```

### 3. 契约

- `ManagerPaths.GenerationsDir`：`${XDG_DATA_HOME}/mihomo-manager/generations`。
- `ManagerPaths.CoreStateDir`：`${XDG_STATE_HOME}/mihomo-manager/core`。
- `CoreLog`/`RuntimeState`：`CoreStateDir/mihomo.log` 与 `CoreStateDir/runtime.json`。
- generation profile 目录名由 profile ID 摘要生成，不能把未约束 ID 直接拼入路径。
- managed generation 目录 `0700`，`config.yaml`、`metadata.json`、current/runtime JSON 为 `0600`；先在同目录临时写、fsync、原生验证，再 rename 发布。
- external/legacy `ConfigPath` 必须为绝对普通文件，不接受符号链接；prepare 与 start 前均核对 SHA-256，绝不写回源文件。
- mihomo 原生验证参数固定为 `-t -d <configDir> -f <configPath>`，默认超时 10 秒。
- managed core 参数固定为 `-d <configDir> -f <configPath>`；Linux 使用 `Setsid`。停止只向所持 `Process` 发 SIGTERM，超时后 SIGKILL。
- controller endpoint 只接受显式 `http://127.0.0.1:<port>` 或 `http://[::1]:<port>`；不接受 `0.0.0.0`、hostname、HTTPS、userinfo 或额外 path。
- Runtime API 单响应上限 1 MiB；配置文件读取上限 16 MiB。
- routing runtime 使用 `GET/PATCH /configs`、`PUT /configs?force=true`、`GET /rules`、`GET/DELETE /connections` 与 `GET /proxies/<group>`；请求/响应必须类型化、context-aware、限制为单个完整 JSON，不透传任意 map 到 daemon DTO。
- Rule runtime 核验必须同时看到 `mm-cn-domain`、`mm-cn-ip` 指向 DIRECT，且所有规则中恰好一个 MATCH、目标为 `🌐 代理`。Global 使用 `GLOBAL` 选择，Direct 的有效节点为 `DIRECT`。
- daemon 启动只装配 CoreManager，状态默认为 `stopped`，绝不自动启动 core。

### 4. 校验与错误矩阵

| 条件 | 行为 |
|------|------|
| managed 无代理、端口冲突、未知 group/rule 目标 | `core.ErrInvalidConfig`，不发布 generation |
| external 是相对路径、符号链接或非普通文件 | 路径安全错误，不执行 mihomo |
| validate 与 start 之间摘要变化 | `core.ErrConfigChanged`，旧实例保持或恢复 |
| `mihomo -t` 非零退出 | `core.ErrValidationFailed`，只报告退出码，不回显可能含秘密的 stderr |
| `mihomo -t` 超时 | `core.ErrValidationTimeout` |
| core 就绪前退出 | `core.ErrProcessExited`，状态 `failed` |
| controller 就绪超时 | `core.ErrReadinessTimeout`，停止新进程并恢复旧实例 |
| runtime 非 2xx、超大或 JSON 不完整 | 就绪失败，不提交活动档案 |
| runtime metadata 写入失败 | SQLite 活动档案补偿到切换前状态 |
| 旧实例恢复失败 | operation 为 `failed/RESTORE_FAILED`，core 状态为 `failed` |
| runtime mode 或 Rule rules 不一致 | `MODE_RUNTIME_MISMATCH`，模式事务恢复文件与旧 runtime |
| 连接关闭失败 | 模式保持成功，返回 `CONNECTION_CLOSE_FAILED` 和 partial status |

### 5. Good / Base / Bad

- Good：候选先渲染、静态/原生验证，旧实例停止后新实例就绪，最后提交 SQLite 和 runtime metadata，operation 为 `succeeded`。
- Base：daemon 正常启动后 core 为 `stopped`；external 配置只读取摘要和执行验证，不改变 mode、mtime 或权限。
- Bad：候选就绪失败且旧实例也无法恢复；必须明确 `failed`，不得把新 profile 标成成功或扫描/误杀宿主其他 mihomo/xray。

### 6. 必需测试

- `internal/core`：generation ID 确定性、`0700/0600`、验证失败无发布、external 只读和摘要变化。
- `internal/mihomo`：golden YAML、静态错误矩阵、`-t -d -f` 参数/超时/退出码、loopback、响应上限、Setsid、SIGTERM/SIGKILL、无关进程存活。
- `internal/mihomo` routing runtime：HTTP method/path/body、三模式解析、rules/count/selection/close、非 2xx、超大和多 JSON 响应。
- `internal/daemon`：成功切换、验证前旧实例不变、就绪失败恢复、恢复失败、metadata 提交补偿、Close 只停止所持进程。
- 全量：`go test ./...`、`go test -race ./...`、`go vet ./...`、Linux amd64/arm64 `CGO_ENABLED=0` build。

### 7. 错误与正确示例

错误：

```go
exec.Command("pkill", "-f", "mihomo.*config.yaml").Run()
os.WriteFile(externalPath, generated, 0o644)
```

正确：

```go
process, err := adapter.Start(ctx, runtimeSpec)
// daemon 持有该句柄；停止时只调用同一 process。
err = process.Stop(ctx)

spec, err := generations.Prepare(ctx, snapshot, adapter)
err = generations.CheckSource(spec)
```

## 场景：监听端口预检与安全重配置

### 1. 范围 / 触发条件

修改 mihomo listener 解析、`Adapter.Start`、结构化端口 API 或 CoreManager 配置重启事务时适用。目标是在创建进程前报告全部可诊断冲突，并保证运行中修改失败不会留下新配置、重复 core 或伪造 running 状态。

### 2. 签名

```go
func mihomo.ParseListenerPorts([]byte) ([]mihomo.ListenerPort, error)
func mihomo.UpdateListenerPort([]byte, string, int) ([]byte, error)
func mihomo.ReadControllerEndpoint(string) (string, error)
func (*mihomo.Adapter) Start(context.Context, core.RuntimeSpec) (core.Process, error)

type ConfigMutation func(context.Context) (restore func(context.Context) error, err error)
func (*daemon.CoreManager) Reconfigure(context.Context, core.ProfileSnapshot, ConfigMutation) (restarted bool, err error)
```

### 3. 契约

- 字段固定为 `mixed-port`、`port`、`socks-port`、`redir-port`、`tproxy-port`、`external-controller`；前五项 `0` 表示禁用，controller 必须为 loopback 且端口非零。
- mixed/socks/tproxy 临时 bind TCP+UDP，HTTP/redir/controller bind TCP；一次收集所有 `EADDRINUSE` 为 `core.PortConflictError`，每项包含 field/network/host/port，不读取 PID 或进程名。
- `Adapter.Start` 必须在 `startProcess` 前执行 preflight；探测只提供启动前诊断，不能消除检查与真实 bind 间的竞争窗口。
- running 修改在同一 Coordinator 内写入候选、prepare、停止旧实例、启动新实例；失败使用独立 15 秒 context 恢复配置和旧 spec。
- 任一 Stop 返回错误时保留精确进程句柄，视运行态为不确定，禁止再启动旧 spec；恢复配置后返回 `RESTORE_FAILED` 并标记 core failed。
- 候选冲突后旧 core 恢复成功时，running 状态仍保留最近 `PortConflicts`，供 `/v1/config/ports` 与 TUI 展示。

### 4. 校验与错误矩阵

| 条件 | 必须行为 |
|---|---|
| 可选端口 `<0` 或 `>65535`；controller 为 `0` | `core.ErrInvalidConfig`，不写配置 |
| 配置内端口重复或 controller 与代理端口重复 | `core.ErrInvalidConfig`，不停止旧 core |
| 一个或多个 socket 已占用 | `PORT_CONFLICT`，不创建候选进程，保留全部 conflicts |
| stopped 修改成功 | 只保存配置，`restarted=false`、`nextStart=true` |
| running 候选启动失败且旧 spec 恢复成功 | 返回原始错误，core 恢复 running，配置恢复 |
| 停止旧/候选 core 失败，或配置/旧 spec 恢复失败 | `RESTORE_FAILED`，core failed，不再启动第二个进程 |

### 5. Good / Base / Bad

- Good：running core 的 mixed 端口修改后只存在一个新受管进程，配置摘要、active generation 和 controller endpoint 全部指向新配置。
- Base：可选端口设为 `0` 后列表显示禁用；stopped core 不探测 controller、不创建进程。
- Bad：旧进程 Stop 返回错误后仍清空句柄并启动旧 spec，可能产生两个 core；此路径必须被测试禁止。

### 6. 必需测试

- `internal/mihomo`：六字段解析/替换、0/范围/重复、TCP+UDP 多冲突、冲突时 `startProcess` 未调用。
- `internal/daemon`：stopped 保存、running 重启、候选冲突恢复、普通 Activate 冲突保留、两类 Stop 失败均不新增进程、恢复失败状态/operation code。
- 集成：真实临时 Unix IPC 的 GET/PUT、request ID、未知字段、配置内容与 `0600`，不得连接真实用户 core。

### 7. 错误与正确示例

错误：Stop 已报错、进程是否退出未知时继续恢复启动。

```go
_ = supervisor.Stop(ctx)
_ = supervisor.Start(ctx, oldSpec)
```

正确：恢复配置但把运行态标记为不确定，保留句柄并阻止重复进程。

```go
if err := supervisor.Stop(ctx); err != nil {
    return failReconfigurationAfterStopError(restore, err)
}
```
