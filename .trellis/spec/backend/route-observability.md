# 实时链路与代理入口诊断契约

## Scenario：活动连接快照、follow 与入口匹配

### 1. Scope / Trigger

修改 mihomo `/connections` 解码、`/v1/connections*`、`mm route connections`、TUI 实时连接页、mode listener/proxy 状态或 GNOME/env 代理检测时必须遵守本规范。目标是只展示 mihomo 真实运行态，保持流式内存有界、可取消，并确保代理 URL 凭据不会跨越采集边界。

### 2. Signatures

```text
mm route connections [--profile ID] [--output table|json]
mm route connections --follow [--profile ID] [--output text|ndjson]
GET /v1/connections?profileId=...
GET /v1/connections/follow?profileId=...
```

```go
type RoutingRuntime interface {
    Connections(context.Context) ([]mihomo.RuntimeConnection, error)
}

type ConnectionService interface {
    Connections(context.Context, app.ConnectionRequest) ([]app.Connection, error)
    FollowConnections(context.Context, app.ConnectionRequest) <-chan app.ConnectionEvent
}

type platform.ProxyInspector interface {
    Inspect(context.Context) []platform.ProxySource
}
```

### 3. Contracts

- `/connections` 原始 JSON 只在 `internal/mihomo` 解码一次；`connections:null` 和缺失字段按空列表/零值处理，string/number destination port 统一为 `int`。
- 每条 typed connection 包含 ID、host、destination IP/port、network、rule/payload、完整 chains、upload/download、start 和 warnings。最终节点只取 mihomo chains 首项，禁止根据静态组配置猜测。
- runtime 单响应仍限 1 MiB，活动连接最多 `mihomo.MaxRuntimeConnections == 4096`。负计数归零并产生 warning；非法端口/时间不得附带原始值。
- follow 每秒轮询，以 ID 比较有界上一快照：新增为 `open`，可见字段改变为 `update`，消失为 `closed` 并携带最后快照。事件按 ID 排序；IPC writer 唯一分配从 1 递增的 seq，并以 `done|error` 终止。
- TUI 只保留当前活动 map 和最多 20 条关闭提示；进入页面创建派生 context，Esc 必须 cancel。`Update` 通过单一 reducer 处理三种 action，`View` 只排序和渲染。
- mihomo listener 从活动 legacy 配置读取 mixed/http/socks port；GNOME 由 daemon 受控执行 `gsettings get`，CLI env 由客户端进程读取 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY` 及小写形式。
- endpoint 在 `internal/platform` 投影为 scheme/host/port 后才进入 DTO；userinfo、path、query、fragment 永不保留。HTTP/HTTPS source 只匹配 http/mixed，SOCKS/ALL_PROXY 只匹配 socks/mixed，host 必须为等价 loopback 且端口相同。
- 状态固定为 `matched|mismatched|disabled|unknown`。mismatch 产生“普通应用流量不会进入 mihomo”warning；unknown 不使 mode 查询/切换失败。诊断不得调用 `gsettings set`、探测 PID、停止 xray/其他进程或占用端口。
- 连接快照、事件、目标地址和关闭提示不写 SQLite、operation recovery 或持久日志。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|---|---|
| core stopped / 无 connection runtime | 409 `CORE_NOT_RUNNING`，app conflict，提示先启动 core |
| `/connections` 非 2xx、非法/多 JSON、超过 1 MiB | 502 `UPSTREAM_FAILURE`，不回显 response body |
| 连接数超过 4096 | 502 `UPSTREAM_FAILURE`，follow 以 error terminal 结束 |
| follow 客户端取消 | 关闭 HTTP body/channel，producer 退出；CLI 取消按成功结束 |
| stream action 非 `open|update|closed` 或 seq 不单调 | app/IPC 拒绝，终止为 stream error |
| gsettings 缺失、无 DBus、未知 GNOME mode | 三个 GNOME source 为 `unknown`，mode 本身仍成功 |
| env URL 无 host/port 或不可解析 | 对应 source 为 `unknown`，错误不包含原 URL |
| source 协议、loopback 或端口不匹配 | `mismatched` 和 typed warning，不修改系统状态 |

### 5. Good / Base / Bad Cases

- Good：运行中的 mihomo 返回真实 RuleSet/chains/counters；CLI snapshot、NDJSON follow 和 TUI 显示同一 typed 值，seq 单调且离页后 producer 退出。
- Base：`connections:null` 显示空列表；GNOME mode=none 和未设置 env 显示 disabled；无 chains 时 final node 留空而不是伪造组选择。
- Bad：CLI/TUI 直连 controller、从配置推断最终节点、把 userinfo/path/query 放入 DTO、为判断 xray 查询并控制端口进程，或把连接历史写入 SQLite，均禁止发布。

### 6. Tests Required

- `internal/mihomo`：null/空/多连接、缺 host、string/number port、IPv4/IPv6、未知 chains、负计数、非法时间、4096 上限和 1 MiB 上限。
- `internal/daemon`：纯 diff 覆盖 open/update/closed 排序、上游 error、context cancel 和 final-node 投影；HTTP 流覆盖 terminal event。
- `internal/app`：snapshot/stream typed 解码、seq 保留、非法 action、body close/cancel，以及 env 投影不含凭据/path/query。
- `internal/platform`：GNOME manual/none/unknown、HTTP/SOCKS/mixed 协议矩阵、IPv4/IPv6 loopback 等价、其他端口 mismatch。
- `internal/cli`：snapshot table/json kind 与字段、follow text/NDJSON、单调 seq、唯一 done/error terminal、错误退出码。
- `internal/tui`：调用 deferred、单一 reducer、宽/窄布局、滚动、最多 20 条关闭提示、Esc cancel 和 mode 入口摘要。
- 完成门：`go test ./...`、`go test -race ./...`、`go vet ./...`、Linux amd64/arm64 `CGO_ENABLED=0` build、help 冒烟和 TUI 静态边界扫描。

### 7. Wrong vs Correct

#### Wrong

```go
// CLI 自己解析 raw map，并根据配置组猜最终节点。
node := config.Groups[raw["chains"].([]string)[1]].Selected
_ = os.WriteFile("connections.json", body, 0o644)
```

#### Correct

```go
connections, err := runtime.Connections(ctx) // internal/mihomo owns decoding
events := capabilities.FollowConnections(ctx, request)
defer cancel() // TUI leaving the page stops the stream producer
```
