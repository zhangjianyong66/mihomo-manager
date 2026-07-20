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

### 5. Good / Base / Bad

- Good：候选先渲染、静态/原生验证，旧实例停止后新实例就绪，最后提交 SQLite 和 runtime metadata，operation 为 `succeeded`。
- Base：daemon 正常启动后 core 为 `stopped`；external 配置只读取摘要和执行验证，不改变 mode、mtime 或权限。
- Bad：候选就绪失败且旧实例也无法恢复；必须明确 `failed`，不得把新 profile 标成成功或扫描/误杀宿主其他 mihomo/xray。

### 6. 必需测试

- `internal/core`：generation ID 确定性、`0700/0600`、验证失败无发布、external 只读和摘要变化。
- `internal/mihomo`：golden YAML、静态错误矩阵、`-t -d -f` 参数/超时/退出码、loopback、响应上限、Setsid、SIGTERM/SIGKILL、无关进程存活。
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
