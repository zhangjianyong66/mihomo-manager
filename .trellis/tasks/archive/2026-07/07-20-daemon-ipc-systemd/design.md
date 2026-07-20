# 技术设计：daemon、Unix socket 与 systemd user service

## 1. 边界与依赖方向

```text
cmd/mm -> internal/cli -> internal/app ports
                         -> internal/ipc client -> Unix socket
                                                   -> internal/daemon server
                                                        -> internal/store
                                                        -> internal/platform
```

- `internal/ipc` 只拥有传输、协议版本、JSON/NDJSON 编解码、请求取消和 peer credential 接口；不导入 SQLite、mihomo 或 Cobra。
- `internal/daemon` 拥有生命周期、状态快照、operation lock、request ID 缓存、HTTP handler 注册和 graceful shutdown；业务用例通过注入的 app/service ports 扩展。
- `internal/platform` 拥有 Linux `SO_PEERCRED`、文件锁、systemd unit 写入/控制和 socket activation 适配；协议包不使用 Linux 专属 syscall。
- `internal/config` 增加显式的 manager paths 解析，默认路径遵循 XDG；`internal/store.Open` 仍只接收 daemon 传入的数据库路径。
- `internal/cli` 只接线 `daemon` 命令和输出映射；`cmd/mm` 只负责依赖装配与退出码。

A3 只提供 daemon health/status handler 和可复用的请求/流式传输层。业务 endpoint 在 A6/A7 注册，避免在 A3 创建空壳命令或重复业务逻辑。

## 2. 路径与文件权限

`config.ManagerPaths` 由环境快照或显式测试值生成：

| 用途 | 默认路径 |
|---|---|
| manager 数据 | `${XDG_DATA_HOME:-$HOME/.local/share}/mihomo-manager` |
| SQLite | `<data>/state.db` |
| manager 状态/日志 | `${XDG_STATE_HOME:-$HOME/.local/state}/mihomo-manager` |
| 非 systemd 运行目录 | `$XDG_RUNTIME_DIR/mihomo-manager`；缺失时使用 state 下 `run/` |
| socket | `<runtime>/mm.sock` |
| lock | `<runtime>/daemon.lock` |
| user units | `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user` |

创建或打开前：拒绝路径中的符号链接、相对路径和非目录父项；运行/数据目录收紧为 `0700`，数据库、锁和 socket 为 `0600`。socket 目标若是普通文件、目录或符号链接，直接失败，不自动删除。已持有 flock 后，只有 owner 为当前 UID、确为 socket 且连接返回 `ECONNREFUSED` 的陈旧 socket 才可删除。非 systemd fallback 目录必须每次启动校验 owner/mode，并在 daemon 退出时移除自身 socket。

## 3. IPC 协议

### 3.1 HTTP over Unix socket

- 监听地址只接受 Unix stream；使用 Go `net/http`，不创建 TCP listener。
- API 路径为 `/v1/health` 与 `/v1/status`，后续业务路径继续使用 `/v1/`。
- 请求头：`MM-Protocol-Min`、`MM-Protocol-Max` 表示客户端支持的整数版本范围；修改请求可带 `MM-Request-ID`。响应返回 `MM-Protocol-Version` 和回显 `MM-Request-ID`。
- 当前协议版本为 `1`。服务端选择 `min(clientMax, serverMax)`，不在客户端范围内则返回 HTTP 426 和 `PROTOCOL_INCOMPATIBLE`。
- JSON body 上限为 1 MiB；请求头读取、读写和空闲超时由 server/client context 控制。HTTP 错误正文仍使用结构化错误，不写入原始底层错误。

基础状态：

```json
{
  "protocolVersion": 1,
  "state": "running",
  "pid": 1234,
  "startedAt": "2026-07-20T08:00:00Z",
  "schemaVersion": 2
}
```

CLI 将该 DTO 投影为 A1 `mm/v1` envelope；IPC 层不依赖 `internal/cli`。

### 3.2 错误与状态码

| HTTP | `code` | 客户端分类 |
|---:|---|---|
| 400 | `INVALID_REQUEST` | 输入错误 |
| 404 | `NOT_FOUND` | 资源不存在 |
| 409 | `OPERATION_CONFLICT` / `REQUEST_ID_CONFLICT` | 状态冲突 |
| 426 | `PROTOCOL_INCOMPATIBLE` | daemon 不可用/协议不兼容，退出码 5 |
| 500 | `INTERNAL` | 内部错误 |

peer UID 失败在 HTTP 前断开；客户端将 Unix dial 的 `EACCES`/`EPERM` 转换为 permission denied，将连接后 EOF 转换为 daemon unavailable，不根据 EOF 猜测服务端拒绝原因。错误 envelope 的 `details` 只允许稳定 ID、协议范围、重试提示等白名单字段。

### 3.3 NDJSON

```json
{"kind":"event","seq":1,"data":{}}
{"kind":"done","seq":2}
```

错误终止使用 `kind=error`，每条记录一行、最大 1 MiB、`seq` 从 1 单调递增；服务端关闭或 context 取消后不得继续向 channel 写入。`internal/ipc.Stream` 只负责校验 envelope、序号和取消，A6 再把 data 解码成具体事件。

## 4. 访问控制与生命周期

### 4.1 peer UID

Linux listener 使用 `Accept` 包装器，在 `http.Server` 处理请求前通过 `unix.GetsockoptUcred(SO_PEERCRED)` 读取 peer UID，并与 `os.Geteuid()` 比较。非 Linux 构建通过平台接口返回“不支持 peer 校验”，在默认安全策略下拒绝启动而不是放宽 socket 权限。测试使用真实 Unix socket、同 UID client 和可控的错误凭据适配器。

### 4.2 单实例与 graceful shutdown

- 以 `O_NOFOLLOW|O_CREAT` 打开 lock，`flock(LOCK_EX|LOCK_NB)` 获得唯一 daemon 所有权；锁冲突返回 `DAEMON_ALREADY_RUNNING`。
- 非 socket activation 模式先安全清理确认属于本次运行的陈旧 socket，再 bind；不删除未知文件。
- systemd activation 从 fd 3 读取 listener，验证其为 Unix socket 后跳过 bind，但仍获取 daemon lock。
- `Run(ctx)` 启动顺序为：路径/权限 -> lock -> store.Open -> listener -> server。任一步失败按逆序清理。
- SIGINT/SIGTERM 触发 `Server.Shutdown`，等待活动请求至多 5 秒，然后关闭 store 和 lock；不会停止 mihomo（A4 才拥有 core）。

daemon 状态包含 `starting`、`running`、`stopping`、`failed`，状态快照只保存在内存；`/v1/status` 只能在 `running` 返回。store schema 版本从 A2 的 `SchemaInfo` 读取。

### 4.3 operation lock 与幂等

operation coordinator 使用容量为 1 的 channel 或等价可取消锁，持有记录包含 `operationID`、`kind`、`startedAt`。等待请求使用 context；无法立即获取时返回 `OPERATION_CONFLICT`。request cache 以 method/path/requestID/body SHA-256 为键，TTL 5 分钟、最多 1024 个完成 JSON 响应；同 ID 不同 body 立即冲突，流式响应和 5xx 不缓存。缓存只在单 daemon 进程内有效，重启后客户端必须安全重试。

## 5. systemd user units 与管理命令

嵌入模板的逻辑内容：

```ini
# mm.socket
[Socket]
ListenStream=%t/mihomo-manager/mm.sock
SocketMode=0600
DirectoryMode=0700
Accept=no
RemoveOnStop=yes

[Install]
WantedBy=sockets.target
```

```ini
# mm.service
[Unit]
StartLimitIntervalSec=60s
StartLimitBurst=5

[Service]
ExecStart=%h/.local/bin/mm daemon run
Restart=on-failure
RestartSec=2s
NoNewPrivileges=yes
UMask=0077
```

unit 带稳定 owner marker、模板版本和内容 checksum；本项目旧版本可升级，内容被本地修改或来源未知时先备份为带时间戳的 `.mihomo-manager.bak`。文件以 `0600` 临时写入、fsync 后 rename。systemd 会话可用时再执行 `daemon-reload` 与 `enable --now mm.socket`，失败则恢复原文件并再次 reload；systemctl 缺失或用户会话不可用时保留已验证 unit，返回 `installed=true, enabled=false` 和前台指引。`disable` 执行 `disable --now mm.socket mm.service`，不删除未知用户 unit。`start/stop` 只作用于 manager units；`status` 先访问 IPC，失败时附带 systemd 可用性诊断。

systemd 不可用、用户无会话或命令返回非零时，命令返回可诊断错误并提示 `mm daemon run`；不启用 linger，不调用 sudo，不启动 proxy core。A8 再把这些操作接入安装器的默认安装和卸载回滚。

## 6. 测试与回滚

- IPC：临时 socket、协议范围、JSON 错误、NDJSON 序号/完成/取消、请求体上限和 context 断开。
- 安全：目录/文件 mode、非 socket 目标、符号链接、lock 竞争、peer UID 平台适配器和 TCP 未监听。
- daemon：多 client、状态快照、graceful shutdown、store 打开失败清理、幂等缓存 TTL/容量。
- systemd：隔离 HOME 的模板解析、原文件备份/恢复、命令幂等、systemd 不可用降级；若 `systemd-analyze` 可用则执行 `verify`，否则运行结构断言。
- 架构：Linux amd64/arm64 `CGO_ENABLED=0` 构建；所有测试使用 `t.TempDir()`，不接触真实 `$HOME`、mihomo 配置或 core。

失败回滚顺序为：停止 socket/service -> 恢复 unit 备份 -> 删除本次创建的 socket/lock/runtime 目录 -> 保留数据库恢复点。A3 不提供 schema downgrade 或 core 回滚。
