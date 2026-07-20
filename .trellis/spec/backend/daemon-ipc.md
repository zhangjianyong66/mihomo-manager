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
func (*systemd.Controller) Enable|Disable|Start|Stop|Status(context.Context) (systemd.Result, error)
```

CLI 签名：`mm daemon run|status|start|stop|enable|disable`；除 `run` 外均支持 `--output table|json`。

## 3. Contracts

- XDG：data 为 `${XDG_DATA_HOME:-$HOME/.local/share}/mihomo-manager`，state 为 `${XDG_STATE_HOME:-$HOME/.local/state}/mihomo-manager`，runtime 优先 `$XDG_RUNTIME_DIR/mihomo-manager`、否则 `<state>/run`，unit 为 `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user`。所有注入路径必须是绝对路径。
- 权限：runtime/data/state 相关创建目录收紧为 `0700`；database/lock/socket/unit 为 `0600`；拒绝已有符号链接、非预期文件类型和非当前用户目录。lock 使用 `O_NOFOLLOW` 和非阻塞 `flock`。
- IPC：只监听 Unix stream，路径以 `/v1/` 开头；请求头 `MM-Protocol-Min`、`MM-Protocol-Max` 是严格正整数范围，响应返回 `MM-Protocol-Version`，修改请求可带 `MM-Request-ID`。当前协议版本为 `1`，JSON/NDJSON 单体上限 1 MiB。
- 状态 DTO：`protocolVersion`、`state`、`pid`、`startedAt`、`schemaVersion`、类型化 `core` 状态；daemon 初始 core 为真实 `stopped`，不得伪造 running。
- NDJSON：`seq` 从 1 单调递增，`kind` 只能是 `event|done|error`，且必须以 `done` 或 `error` 终止；终止后禁止继续写。
- 安全：Linux listener 在 HTTP 前以 `SO_PEERCRED` 校验 peer UID；root daemon 拒绝启动；daemon 不监听 TCP、不调用 sudo、不启用 linger。启动 daemon 不自动启动 mihomo core，显式 CoreManager 操作才可托管。
- 幂等：完成且非 5xx/非流式的 JSON 响应按 request ID 缓存 5 分钟、最多 1024 条；同 ID 不同 method/path/body 返回 `REQUEST_ID_CONFLICT`。
- systemd：`mm.socket` 使用 `%t/mihomo-manager/mm.sock`、`0600/0700`、`Accept=no`、`RemoveOnStop=yes`；service 只执行 `%h/.local/bin/mm daemon run`，设置 `Restart=on-failure`、退避、`NoNewPrivileges=yes`、`UMask=0077`。unit 带 owner/version/content checksum marker，以临时文件 fsync+rename 安装。

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

服务端 peer UID 拒绝发生在 HTTP 前，客户端只能分类为 daemon unavailable，不能根据 EOF 猜测权限原因。

## 5. Good / Base / Bad Cases

- Good：隔离 XDG 下前台 daemon 打开 schema v2 database，同 UID 两个 client 获得相同 PID/status；SIGTERM 后 socket 和 lock 释放，未启动 mihomo。
- Base：无 systemd user 会话执行 `daemon enable`，两个 unit 经校验后保留，结果明确为“已安装未启用”并提示 `mm daemon run`。
- Bad：socket 目标是普通文件/符号链接、peer UID 不同、协议范围无交集、request ID 对应不同 body 或 unit enable 失败时，拒绝继续且不覆盖未知资源；unit 失败恢复原内容。

## 6. Tests Required

- `internal/config`：XDG 与 fallback、所有相对路径拒绝。
- `internal/platform`：目录/lock/socket mode、符号链接、普通文件目标、flock 竞争、同 UID 成功和错误 UID 断连。
- `internal/ipc`：协议交集/无交集、request ID、超大 body/line、JSON envelope、NDJSON 序号/终止/取消。
- `internal/daemon`：真实临时 Unix socket、两个 client、schema status、operation conflict、幂等重放/冲突/过期、graceful cleanup；不得启动真实 core。
- `internal/platform/systemd`：模板结构/checksum、无 systemd 降级、fake systemctl 调用范围、失败恢复和未知 unit 备份。
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
