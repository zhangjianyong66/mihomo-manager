# daemon、Unix IPC 与 systemd user 契约

## 1. Scope / Trigger

修改 `internal/config.ManagerPaths`、`internal/ipc`、`internal/daemon`、`internal/platform`、daemon CLI 或 systemd user unit 时必须遵守本规范。目标是保持 daemon 单写者、同用户 IPC、无 TCP/root/sudo/linger，以及 systemd 不可用时的可诊断降级。

## 2. Signatures

```go
func config.LoadManagerPaths() (config.ManagerPaths, error)
func config.ResolveManagerPaths(config.ManagerEnvironment) (config.ManagerPaths, error)

func ipc.NewClient(socketPath string) *ipc.Client
func (*ipc.Client) Do(context.Context, method, path, requestID string, request, result any) error
func ipc.NewServer(http.Handler) *ipc.Server

func daemon.New(daemon.Options) *daemon.Server
func (*daemon.Server) Run(context.Context) error

func systemd.New(unitDir string) *systemd.Controller
func (*systemd.Controller) Enable|Disable|Start|Stop|Restart|Status(context.Context) (systemd.Result, error)
```

CLI 签名：`mm daemon run|status|start|stop|restart|enable|disable`；除 `run` 外均支持 `--output table|json`。

## 3. Contracts

- XDG：data 为 `${XDG_DATA_HOME:-$HOME/.local/share}/mihomo-manager`，state 为 `${XDG_STATE_HOME:-$HOME/.local/state}/mihomo-manager`，runtime 优先 `$XDG_RUNTIME_DIR/mihomo-manager`、否则 `<state>/run`，unit 为 `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user`。所有注入路径必须是绝对路径。
- 权限：runtime/data/state 相关创建目录收紧为 `0700`；database/lock/socket/unit 为 `0600`；拒绝已有符号链接、非预期文件类型和非当前用户目录。lock 使用 `O_NOFOLLOW` 和非阻塞 `flock`。
- IPC：只监听 Unix stream，路径以 `/v1/` 开头；请求头 `MM-Protocol-Min`、`MM-Protocol-Max` 是严格正整数范围，响应返回 `MM-Protocol-Version`，修改请求可带 `MM-Request-ID`。当前协议版本为 `1`，JSON/NDJSON 单体上限 1 MiB。
- 状态 DTO：`protocolVersion`、`state`、`pid`、`startedAt`、`schemaVersion`、类型化 `core` 状态；daemon 初始 core 为真实 `stopped`，不得伪造 running。
- NDJSON：`seq` 从 1 单调递增，`kind` 只能是 `event|done|error`，且必须以 `done` 或 `error` 终止；终止后禁止继续写。
- 安全：Linux listener 在 HTTP 前以 `SO_PEERCRED` 校验 peer UID；root daemon 拒绝启动；daemon 不监听 TCP、不调用 sudo、不启用 linger。启动 daemon 不自动启动 mihomo core，显式 CoreManager 操作才可托管。
- 幂等：完成且非 5xx/非流式的 JSON 响应按 request ID 缓存 5 分钟、最多 1024 条；同 ID 不同 method/path/body 返回 `REQUEST_ID_CONFLICT`。带 request ID 的 handler 一旦调用 `Flush()`，缓存 writer 必须立即提交已写 header/status/body 并切换为直通，后续内容逐次 flush；该响应不得写入 RequestCache，相同 ID 再次请求时重新执行 handler。
- systemd：`mm.socket` 使用 `%t/mihomo-manager/mm.sock`、`0600/0700`、`Accept=no`、`RemoveOnStop=yes`；service 只执行 `%h/.local/bin/mm daemon run`，设置 `Restart=on-failure`、退避、`NoNewPrivileges=yes`、`UMask=0077`。unit 模板版本为 2，带 owner/version/content checksum marker，以临时文件 fsync+rename 安装。
- 显式执行 `CONFIG_DIR=... MIHOMO_BIN=... MIHOMO_API_PORT=... mm daemon enable` 时，controller 将三个白名单变量校验、转义并写入受管 `Environment=` 块；后续未显式传环境的重复 enable 保留该块，确保 systemd 重启后 legacy 迁移与兼容操作仍使用同一路径。环境值不得包含凭据或控制字符，两个路径必须绝对，端口必须为 1-65535。
- 监听端口 IPC 为 `GET /v1/config/ports?profileId=...` 与 `PUT /v1/config/ports`；PUT body 固定为 `profileId`、`field`、`port` 且必须带 `MM-Request-ID`。响应包含六项 typed ports、core state、`restarted`、`nextStart` 和最近 `portConflicts`，错误 details 保留 `conflicts` 及可用的 partial `status`。

## 4. Validation & Error Matrix

| 条件 | IPC/app 分类 | CLI 退出码 |
|---|---|---:|
| socket 不存在、peer 拒绝后 EOF、协议无交集 | `daemon_unavailable` | 5 |
| Unix dial `EACCES`/`EPERM`、root、路径安全失败 | `permission_denied` | 7 |
| daemon lock 已持有、operation/request ID 冲突 | `conflict` | 4 |
| 非 `/v1/`、非法版本范围、超大/非法 JSON | `invalid_argument` | 2 |
| systemd user 不可用的 `enable` | 成功安装但 `enabled=false`，附前台提示 | 0 |
| systemd user 不可用的 `start/stop` | `daemon_unavailable` | 5 |
| daemon-reload/enable 失败 | 恢复原 unit，`internal` | 1 |
| 监听字段/范围非法或 PUT 缺 request ID | `INVALID_REQUEST` / `REQUEST_ID_REQUIRED` | 2 |
| listener bind 冲突 | HTTP 409 `PORT_CONFLICT`，details 含全部 conflicts | 4 |
| 端口重配置恢复失败 | HTTP 500 `RESTORE_FAILED`，core failed | 1 |

服务端 peer UID 拒绝发生在 HTTP 前，客户端只能分类为 daemon unavailable，不能根据 EOF 猜测权限原因。

## 5. Good / Base / Bad Cases

- Good：隔离 XDG 下前台 daemon 打开 schema v2 database，同 UID 两个 client 获得相同 PID/status；SIGTERM 后 socket 和 lock 释放，未启动 mihomo。
- Base：无 systemd user 会话执行 `daemon enable`，两个 unit 经校验后保留，结果明确为“已安装未启用”并提示 `mm daemon run`。
- Bad：socket 目标是普通文件/符号链接、peer UID 不同、协议范围无交集、request ID 对应不同 body 或 unit enable 失败时，拒绝继续且不覆盖未知资源；unit 失败恢复原内容。

## 6. Tests Required

- `internal/config`：XDG 与 fallback、所有相对路径拒绝。
- `internal/platform`：目录/lock/socket mode、符号链接、普通文件目标、flock 竞争、同 UID 成功和错误 UID 断连。
- `internal/ipc`：协议交集/无交集、request ID、超大 body/line、JSON envelope、NDJSON 序号/终止/取消。
- `internal/daemon`：真实临时 Unix socket、两个 client、schema status、operation conflict、幂等重放/冲突/过期、POST NDJSON 首条事件在 handler 完成前可读且流响应不缓存、graceful cleanup；不得启动真实 core。
- `internal/daemon` 端口路由：GET 六字段、PUT request ID/非法字段、stopped `nextStart`、running `restarted`、配置内容/权限和多冲突 details。
- `internal/platform/systemd`：模板结构/checksum、受管环境 round-trip/转义/保留、无 systemd 降级、fake systemctl 调用范围、失败恢复和未知 unit 备份。
- 完成门：`go test ./...`、`go test -race ./...`、`go vet ./...`、Linux amd64/arm64 `CGO_ENABLED=0` 构建、隔离 XDG 前台 daemon/status/SIGTERM 冒烟。

## 7. Wrong vs Correct

错误：daemon 不可用时在 CLI 进程内打开默认 database，或为了方便改为 TCP listener。

```go
store.Open(ctx, os.Getenv("HOME")+"/.local/share/mihomo-manager/state.db", store.OpenOptions{})
net.Listen("tcp", "127.0.0.1:9091")
```

正确：CLI 只经注入的 app service 调用 Unix IPC；显式路径只在 daemon 装配后传给 store。

```go
paths, _ := config.LoadManagerPaths()
client := ipc.NewClient(paths.Socket)
err := client.Do(ctx, http.MethodGet, "/v1/status", "", nil, &status)
```

## Scenario：带 request ID 的流式响应直通

### 1. Scope / Trigger

修改 `RequestCache.Middleware`、任何带 `MM-Request-ID` 的流式写路由，或包装 daemon `http.ResponseWriter` 时必须遵守本节。目标是在保留普通 JSON 幂等重放的同时，不破坏 NDJSON 的逐事件传输时序。

### 2. Signatures

```go
func (*RequestCache) Middleware(http.Handler) http.Handler

type responseRecorder struct {
	// 内部持有最终 http.ResponseWriter，并实现 http.ResponseWriter 与 http.Flusher。
}
```

该行为通用于所有带 request ID 的非 GET/HEAD 请求，不按 `/v1/nodes/test*`、日志或连接路由硬编码。

### 3. Contracts

- recorder 初始处于 buffered 模式；普通 JSON 的 header/status/body 在 handler 返回后一次性提交，并沿用五分钟、1024 条、非 5xx 缓存语义。
- handler 首次调用 `Flush()` 时，recorder 按 header、status、已缓冲 body 的顺序提交到底层 writer，再切换为单向 passthrough；后续 `Write` 与 `Flush` 直接作用于底层 writer。
- 一旦进入 passthrough，middleware 在 handler 返回后不得二次写 header/body，也不得调用 RequestCache store；相同 request ID 再次请求时重新执行 handler。
- 底层 writer 不实现 `http.Flusher` 时，首次 `Flush()` 仍须提交已有响应并进入 passthrough；真实 HTTP server 的底层 writer支持 flush。
- 未主动 flush 的 `application/x-ndjson` 响应仍不得缓存，作为异常 handler 的防御；NDJSON 的 `seq`、终止事件和单行上限保持不变。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|---|---|
| 无 request ID，或 GET/HEAD | 绕过 RequestCache wrapper，直接执行 handler |
| 同 ID、同签名、普通非 5xx JSON | 重放缓存响应，不重复执行 handler |
| 同 ID、不同 method/path/body | HTTP 409 `REQUEST_ID_CONFLICT` |
| handler 已调用 `Flush()` | 实时直通且不缓存，相同 ID 后续请求重新执行 |
| status >= 500 或未 flush 的 NDJSON | 返回响应但不缓存 |
| 请求体读取失败或超过 1 MiB | HTTP 400 `INVALID_REQUEST`，不执行 handler |

### 5. Good / Base / Bad Cases

- Good：节点批量测速写出首条 event 并 flush 后继续探测，客户端在 terminal 前读到 event，随后逐条更新进度。
- Base：普通 JSON 修改请求不调用 flush，仍原子返回并可按相同 request ID 重放。
- Bad：用只在内存记录 body、未实现 `http.Flusher` 的 recorder 包装流 handler，导致客户端直到 handler 完成才一次性收到全部事件。

### 6. Tests Required

- 使用真实 `httptest.Server`：POST 带 request ID，handler 写首条 NDJSON event、flush 后阻塞；断言 `client.Do` 和首行读取均在解除阻塞前完成。
- 解除阻塞后断言只收到一个 terminal event，HTTP status/content type 正确且无重复 body。
- 相同 request ID 再请求一次并断言 handler 调用次数增加；现有普通 JSON 重放、签名冲突和过期测试必须继续通过。

### 7. Wrong vs Correct

错误：只在 handler 返回后识别 content type；此时实时事件已经全部被内存缓冲。

```go
recorder := httptest.NewRecorder()
next.ServeHTTP(recorder, request)
if recorder.Header().Get("Content-Type") == "application/x-ndjson" {
	// 已经来不及恢复逐事件时序。
}
```

正确：wrapper 本身实现 `http.Flusher`，首次 flush 即提交已缓冲内容并永久切换直通；middleware 看到直通状态后直接返回且不缓存。

```go
next.ServeHTTP(recorder, request)
if recorder.passthrough {
	return
}
storeCompletedJSON(recorder)
```

## Scenario：三模式查询与事务切换

### 1. Scope / Trigger

修改 `RoutingMode`、legacy 配置模式、mihomo runtime mode API 或 `/v1/mode` 时必须遵守本节。目标是让 daemon 作为单写者可靠切换 `global|rule|direct`，并让配置、运行态和恢复结果可机器判断。

### 2. Signatures

```go
func (*CapabilityService) ModeStatus(context.Context, string) (ModeStatus, error)
func (*CapabilityService) SetMode(context.Context, string, domain.RoutingMode, bool) (ModeStatus, error)

type ModeService interface {
    ModeStatus(context.Context, domain.ProfileID) (RoutingModeStatus, error)
    SetMode(context.Context, SetRoutingModeRequest) (RoutingModeStatus, error)
}
```

IPC 为 `GET /v1/mode?profileId=...` 与 `PUT /v1/mode`；PUT body 固定为 `profileId`、`mode`、`closeConnections`。

### 3. Contracts

- PUT 必须携带非空 `MM-Request-ID`；相同 ID 与相同 method/path/body 在 5 分钟缓存内返回同一响应，不重复切换，不同 body 返回 `REQUEST_ID_CONFLICT`。
- 只接受活动 legacy profile；external/managed 返回 `PROFILE_MODE_UNSUPPORTED`。core stopped 时只发布已原生验证的配置，`runtimeAvailable=false`、`nextStart=true`，不得探测或启动 controller。
- 完整状态包含 config/runtime mode、core state、effective group/node、两个 CN ruleset health、连接数、operation ID/phase 和 warnings；错误响应在已有状态时通过 `details.status` 保留它。
- mode、core、subscription、config 和 route 写操作复用 CoreManager 的同一 `Coordinator`。协调器占用时返回冲突，不得绕过到 legacy `pkill`、配置直写或原始 HTTP。
- 默认保留连接；只有 `closeConnections=true` 才 DELETE mihomo `/connections`。关闭失败不回滚已成功模式，返回 `CONNECTION_CLOSE_FAILED` 及 `connections_close_failed` 状态。

### 4. Validation & Error Matrix

| 条件 | HTTP / code | app 分类 |
|---|---|---|
| mode 非 `global|rule|direct` | 400 `INVALID_ROUTING_MODE` | `invalid_argument` |
| 缺少 request ID | 400 `REQUEST_ID_REQUIRED` | `invalid_argument` |
| 非活动或非 legacy profile | 409 `PROFILE_MODE_UNSUPPORTED` | `conflict` |
| expected SHA 改变 | 409 `CONFIG_CHANGED` | `conflict` |
| runtime mode/rules 不一致 | 502 `MODE_RUNTIME_MISMATCH` | `upstream_failure` |
| 文件或 runtime 恢复失败 | 500 `RESTORE_FAILED` | `internal`，core `failed` |
| 模式成功但关闭连接失败 | 424 `CONNECTION_CLOSE_FAILED` | `upstream_failure`，details 带成功状态 |

### 5. Good / Base / Bad Cases

- Good：running core 切到 Rule 后 `/configs.mode=rule`，两个 manager RULE-SET 和唯一 `MATCH,🌐 代理` 已加载，连接默认保留。
- Base：stopped core 切换只改配置并报告下次启动生效；GET 不产生 operation。
- Bad：发布后 runtime 更新失败，恢复旧文件、权限、expected 摘要和旧 mode；任何恢复侧失败都返回 `RESTORE_FAILED`，不得伪报旧 runtime 正常。

### 6. Tests Required

- domain JSON 三枚举 round-trip 与非法值无状态变化。
- legacy 覆盖原生校验、发布、runtime update/verify、expected refresh、恢复失败和 close partial-success 注入点。
- daemon 使用真实临时 Unix socket覆盖 GET/PUT、严格单 JSON、1 MiB、必填 request ID、重放/冲突和共享协调器。
- app client 断言成功状态及 `details.status` 错误状态均完整映射。

### 7. Wrong vs Correct

错误：core stopped 时尝试请求 `127.0.0.1:9090`，或 mode 失败后只恢复 YAML、不恢复 runtime/expected 摘要。

正确：候选临时文件先 `mihomo -t`，原子发布后仅在 CoreManager 明确 running 时使用 typed runtime；失败进入同一补偿流程并核验恢复结果。

## Scenario：客户端组合重启 daemon 与 Core

### 1. Scope / Trigger

修改 `app.DaemonService.Restart`、`systemd.Controller.Restart/Status`、`mm daemon restart` 或 TUI“重启全部”时适用。目标是让已安装的新 `mm` 在用户明确触发后安全替换旧 daemon，同时保持 Core 的目标状态；daemon 不得通过新 IPC 重启自身。

### 2. Signatures

```go
func (*DaemonService) Restart(context.Context, DaemonRestartProgressFunc) (DaemonRestartResult, error)
func (*systemd.Controller) Restart(context.Context) (systemd.Result, error)

type DaemonRestartResult struct {
    PreviousDaemonPID int
    DaemonPID int
    PreviousStartedAt time.Time
    StartedAt time.Time
    PreviousCoreState domain.CoreState
    CoreState domain.CoreState
    DaemonRestarted bool
    CoreRestored bool
    RecoveryAttempted bool
    RecoverySucceeded bool
    FailurePhase DaemonRestartPhase
}
```

CLI 为 `mm daemon restart [--output table|json]`；TUI“服务管理”分别提供“重启 Core”和“重启全部”。不新增 daemon HTTP 路由或协议版本。

### 3. Contracts

- 事务由仍存活的 CLI/TUI 客户端执行：旧 daemon capability 负责 Core stop，新旧 daemon 都只通过现有 `/v1/status` 与 `/v1/core/*` 握手；systemd adapter 只执行 `systemctl --user restart mm.service`，保持 `mm.socket` active。
- 任何副作用前必须确认 systemd user 可用、两个 unit 文件摘要有效且均为受管文件、`mm.service`/`mm.socket` 都 active；不得通过 PID 信号、进程名扫描、sudo、linger 或 system service 接管前台 daemon。
- Core 目标矩阵：running -> running，stopped -> stopped，degraded/failed -> 尝试干净 running，starting/stopping -> 冲突且零副作用。需停止时必须通过 capability stop 并再次读取 `/v1/status` 确认 stopped。
- 新 daemon 只有在协议握手成功，且 PID 与 startedAt 都不同于旧实例后才算更换成功；随后恢复 Core 并再次验证 daemon 身份、Core 目标状态及 systemd service/socket 状态。
- 默认前向事务 40 秒、新 daemon ready 10 秒、轮询 200ms；发生副作用后的失败使用独立 15 秒 context 启动 socket、等待兼容 daemon 并恢复 Core。恢复成功仍返回原失败，partial result 固定放入 `app.Error.Details["restart"]`。
- TUI 确认前 Esc 零副作用；执行开始后 Esc/q/Ctrl+C 不取消事务，页面按 typed phase 展示预检、停止 Core、重启 daemon、等待握手、恢复 Core、验证与必要的自动恢复。

### 4. Validation & Error Matrix

| 条件 | code / 分类 | 必须行为 |
|---|---|---|
| systemd user 不可用、service/socket inactive | `DAEMON_RESTART_PREFLIGHT_FAILED` / daemon_unavailable | 零启停调用，提示前台 daemon 不受支持 |
| unit 缺失、摘要无效或非受管 | `DAEMON_RESTART_PREFLIGHT_FAILED` / conflict | 零副作用，不覆盖或接管 unit |
| Core starting/stopping | `DAEMON_RESTART_CORE_BUSY` / conflict | 零副作用，退出码 4 |
| Core stop 后复核非 stopped 或 daemon 身份并发变化 | conflict | 不执行 systemd restart；有副作用时进入恢复 |
| 新 daemon PID 或 startedAt 未变化、ready 超时 | `DAEMON_RESTART_TIMEOUT` / daemon_unavailable | 不误报成功，进入有界恢复 |
| 协议无交集 | daemon_unavailable，retryable=false | 不通过不兼容客户端控制 Core，报告手工命令 |
| Core 恢复或最终验证失败 | 原 capability 分类 | 保留新 daemon，执行一次有界恢复并报告最终状态 |

### 5. Good / Base / Bad Cases

- Good：旧 daemon/Core running，Core 经 capability 停止；service 重启后 PID/startedAt 均变化，新 daemon 协议兼容，Core 恢复 running，TUI 保持运行。
- Base：旧 Core stopped，事务不调用 Core stop/start，只替换 daemon 并保持 stopped；安装器升级策略不因此自动中断 running Core。
- Bad：把 socket active 当成 service active、只校验 PID、让 daemon IPC handler 调用 `systemctl restart`，或协议不兼容后回退 `pkill`/直连 mihomo；均禁止发布。

### 6. Tests Required

- `internal/platform/systemd`：service/socket 独立状态、受管摘要、Restart 只触及 `mm.service`、unavailable 与 systemctl 失败。
- `internal/app`：六种 Core 状态、stop 二次复核、并发 daemon 变化、PID/startedAt 任一未变化、握手重试/超时/协议不兼容、每个失败阶段及恢复成功/失败；只用 fake client/controller/core/clock。
- `internal/cli`：help、table/json kind、partial error details、退出码和 stdout/stderr 分离。
- `internal/tui`：菜单拆分、确认零副作用、阶段流、Esc/q/Ctrl+C 锁定、完整/恢复/失败结果与窄终端。
- 全量门禁不得访问真实用户 socket、systemd、配置或 Core。

### 7. Wrong vs Correct

错误：让 daemon 重启自己，或同时停止 socket 造成监听端点竞态。

```go
handler := func() { exec.Command("systemctl", "--user", "restart", "mm.service", "mm.socket").Run() }
```

正确：外部客户端先通过 capability 收敛 Core，再只重启 service，并用 typed status 验证新身份。

```go
_ = core.CoreAction(ctx, profileID, "stop")
_, _ = controller.Restart(ctx) // systemctl --user restart mm.service
status, _ := waitForChangedDaemon(ctx, oldPID, oldStartedAt)
```
