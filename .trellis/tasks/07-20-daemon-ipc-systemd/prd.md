# daemon、Unix socket 与 systemd user service

## Goal

为 2.0 Alpha 建立用户级 daemon、版本化本机 IPC 和 systemd user socket 激活基础，使 CLI/TUI 后续可以通过单一 daemon 管理状态、文件和 proxy core。A3 只交付运行边界与协议骨架，不迁移 1.x 配置、不托管 mihomo core、不实现业务命令。

## Background

- 父任务已确定：daemon 是运行状态和写操作的唯一所有者；CLI/TUI 不得在 daemon 不可用时直接写文件、SQLite 或控制 core。
- A1 已定义 `mm/v1` 的 CLI JSON envelope、错误分类和退出码；A2 已提供显式路径的 SQLite store，daemon 装配负责默认用户路径。
- IPC 使用 HTTP/JSON over Unix socket；普通响应使用 JSON，日志/测速/连接等后续流式能力使用 NDJSON。
- 首期只支持 Ubuntu/Debian amd64、arm64 和 Linux 用户会话；不监听 TCP，不安装 setuid，不隐式请求 sudo，不静默启用 linger。

## Requirements

### 运行与所有权

- R1：实现前台 `mm daemon run`，启动时打开注入的 SQLite 路径、建立 daemon 状态并监听 Unix socket；收到 SIGINT/SIGTERM 后有序关闭 HTTP server、store、socket 和单实例锁。
- R2：daemon 是唯一状态写者。A3 提供的 CLI client 在连接失败、协议不兼容或权限拒绝时返回可识别的 daemon 错误，绝不回退为进程内执行或直接访问文件/store。
- R3：daemon 使用单一 operation lock 串行化未来的启动、停止、档案切换、配置发布等写操作；锁冲突返回稳定冲突错误并包含可审阅的操作标识，不阻塞到不可控超时。
- R4：实现短期 request ID 幂等缓存。相同方法、路径、请求 ID 和请求体重试返回同一完成响应；相同 ID 对不同请求体返回冲突；流式请求不进入缓存。
- R5：daemon 暴露基础状态查询，至少包含运行状态、PID、协议版本、启动时间和 schema 版本；core 状态不在 A3 伪造或自动启动。

### IPC 协议与安全

- R6：Unix socket API 路径以 `/v1/` 开头；客户端发送支持的协议版本范围，daemon 返回选定版本。无交集时返回结构化 `PROTOCOL_INCOMPATIBLE`，CLI 映射为退出码 5。
- R7：普通请求使用 JSON envelope；NDJSON 流每行包含事件类型和单调序号，并以明确的完成或错误事件结束。请求接受 `context.Context` 取消，断开客户端不会遗留服务端 goroutine 或锁。
- R8：socket 父目录权限为 `0700`，socket 权限为 `0600`；Linux 使用 `SO_PEERCRED` 校验连接 UID 必须等于 daemon 有效 UID。非本用户连接在 HTTP 处理前关闭，不返回敏感信息。
- R9：拒绝 TCP 监听、相对路径、socket/锁文件符号链接和不安全的父目录；已有非 socket 目标不得被删除覆盖。请求体、NDJSON 行和 header 受大小/超时限制。
- R10：客户端区分 Unix socket 拨号权限错误、daemon 不可用、协议不兼容、取消和服务端结构化错误；服务端在 HTTP 前拒绝 peer 时按不可用处理，不猜测权限原因。错误消息不包含订阅 URL、节点 URI、UUID、密码或完整请求体。

### systemd user service

- R11：提供 `mm.socket` 与 `mm.service` 的嵌入式模板。socket 使用 `%t/mihomo-manager/mm.sock`、`0600` socket/`0700` 目录和 `Accept=no`；service 只启动 manager daemon，不启动 proxy core。
- R12：提供 `mm daemon enable|disable|start|stop|status`：enable 安装/校验 unit、执行 user daemon-reload 并启用 socket；start/stop 只控制 manager socket/service；status 可探测 IPC，也能诊断 systemd 不可用。`disable` 不删除用户未明确授权的文件。
- R13：安装或替换同名 unit 前保存原文件并校验归属；写入失败，或在可用 systemd 会话中 daemon-reload/enable 失败时恢复原文件。systemd 命令缺失或用户会话不可用时保留已验证 unit，返回“已安装但未启用”并明确提示 `mm daemon run`，不得假报已启用。
- R14：unit 设置普通用户运行、`NoNewPrivileges`、`UMask=0077`、失败重启退避和启动速率限制；不启用 linger、不写 sudoers、不扩大 TUN 权限。

### 平台与兼容

- R15：Linux peer credential、systemd 和 socket 激活实现隔离在平台包；其他平台构建可明确报告不支持，不污染 IPC 协议层。
- R16：默认运行目录、状态目录和数据库路径遵守 XDG，保留 `~/.local/share/mihomo-manager`、`~/.local/state/mihomo-manager` 的默认约定；测试可注入临时路径，不能触碰真实用户状态。

## Acceptance Criteria

- AC1（R1-R5）：前台 daemon 在临时目录启动后，多个 client 连接到同一实例；状态查询稳定返回 PID/协议/启动时间/schema，关闭后移除 socket、释放文件锁并关闭 store，daemon 不启动 mihomo。
- AC2（R6-R7）：协议版本有交集时完成 JSON 往返；无交集得到 `PROTOCOL_INCOMPATIBLE`；NDJSON 流具备序号、完成/错误终止、取消和断流清理测试。
- AC3（R8-R10）：目录/socket 权限和 `SO_PEERCRED` 测试通过；错误 UID、socket 路径冲突、符号链接、超大请求、超时和 TCP 探测均被拒绝；敏感夹具不出现在错误/日志。
- AC4（R3-R4）：并发 mutation 只有一个持有 operation lock；相同 request ID 重试得到相同响应，不同 body 冲突，缓存过期/容量淘汰有测试。
- AC5（R11-R14）：unit 模板结构检查通过，socket 激活只拉起 daemon；enable/disable/start/stop 在隔离 HOME 下可重复执行，失败恢复原文件，systemd 不可用时给出前台指引。
- AC6（R15-R16）：Linux amd64/arm64、`CGO_ENABLED=0` 构建通过；路径、peer credential 和 systemd 平台实现有清晰编译边界，所有测试使用临时 HOME/socket/database。
- AC7（全部）：`gofmt`、`go test ./...`、`go test -race ./...`、`go vet ./...`、无 CGO 构建和 `git diff --check` 通过；不修改真实 mihomo 配置或启动真实 core。

## Out Of Scope

- mihomo/Xray/sing-box 进程托管、配置生成、core 崩溃恢复和 TUN；属于 A4、2.1 或 3.0。
- 真实 `~/.config/mihomo` 发现、legacy/external/managed 迁移；属于 A5。
- profile、subscription、node 等业务 API 和 CLI 等价迁移；属于 A6。
- 安装器默认安装 unit、卸载清理、发布许可证和 Alpha 全流程；属于 A8。A3 只提供可调用的 unit 管理实现和模板。
- TCP/局域网远程协议、root daemon、linger、宽泛 sudoers、GUI/托盘能力。

## Dependencies

- 依赖已归档的 A1「CLI 与应用服务契约」和 A2「领域模型、SQLite 与迁移框架」。
- A4 依赖 A3 的 daemon 生命周期和 IPC client；A5 依赖 A3 的单写者和默认路径装配；A6 依赖 A3 的 CLI client。
