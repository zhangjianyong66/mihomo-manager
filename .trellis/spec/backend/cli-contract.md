# CLI 与应用边界契约

## Scenario：新增或修改 `mm` 命令

### 1. Scope / Trigger

以下变化必须遵守本规范：新增 Cobra 命令、修改应用服务接口、增加 table/json 输出、增加机器错误码或输出可能含秘密的字段。

目标是让 `cmd/mm` 只负责进程装配，让 CLI/TUI 复用 `internal/app` 用例，并保证脚本可依赖 JSON 和退出码。A1 发布 `mm` 与 `mm tui`，A3 增加 daemon 命令，A5 增加 migrate 命令，A6 增加 core/config/group/node 和 legacy subscription/route/log 命令，M4 增加 mode CLI/TUI。

daemon `run` 只向 stderr 写诊断并保持前台运行；其他 daemon 命令通过 `internal/app.DaemonService` 访问 IPC/systemd，不直接读写 SQLite、socket 或 unit 文件。daemon 不可用/协议不兼容映射退出码 5，锁冲突映射 4，路径安全/权限拒绝映射 7。

### 2. Signatures

入口签名：

```go
func NewRoot(deps Dependencies) *cobra.Command
func Execute(
    ctx context.Context,
    deps Dependencies,
    args []string,
    stdin io.Reader,
    stdout, stderr io.Writer,
) int
```

应用服务接口放在 `internal/app`，按 core、profile、node、group、subscription、route、config、log 拆分。所有方法首参为 `context.Context`，参数和返回值只能使用标准库、`internal/domain` 或 `internal/app` 类型。

领域 ID 和枚举放在 `internal/domain`：`ProfileID`、`SubscriptionID`、`NodeID`、`GroupID`、`CoreType`、`CoreState`。

模式能力通过 `internal/app.ModeService` / `CapabilityAPI.ModeStatus|SetMode` 暴露；请求使用 `SetRoutingModeRequest{ProfileID, Mode, CloseConnections, RequestID}`，返回 `RoutingModeStatus`，CLI/TUI 不直接解析 daemon map 或调用 mihomo `/configs`。

模式 CLI 固定为 `mm mode status [--profile ID] [--output table|json]` 与 `mm mode set <global|rule|direct> [--profile ID] [--close-connections] [--output table|json]`；成功 kind 分别为 `RoutingModeStatus`、`RoutingModeChange`。daemon 不可用和未迁移错误必须分别给出 `mm daemon status/start`、`mm migrate plan/apply` 的可执行提示。

监听端口 CLI 固定为 `mm config ports [--profile ID] [--output table|json]` 与 `mm config port set <field> <port> [--profile ID] [--output table|json]`；成功 kind 为 `ListenerPortStatus`。TUI 常驻入口为“配置管理 > 监听端口”，只能通过 `app.CapabilityAPI.ListenerPorts|SetListenerPort` 异步调用 daemon。

### 3. Contracts

命令行为：

- 无参数 `mm` 和 `mm tui` 调用同一个 `TUIRunner`。
- help、补全和参数错误不得启动 TUI。
- 未实现的业务命令不得注册空壳；`--output`、`--show-secrets` 只绑定到实际使用它们的命令。
- stdin/stdout/stderr 从 `Execute` 注入，不允许命令测试读取真实终端或 HOME。

JSON 成功 envelope：

```json
{"apiVersion":"mm/v1","kind":"CoreStatus","data":{},"warnings":[]}
```

JSON 错误 envelope：

```json
{"apiVersion":"mm/v1","kind":"Error","error":{"code":"PROFILE_CONFLICT","message":"档案正在切换","retryable":false,"details":{}}}
```

应用错误的 `Category` 只决定进程退出码，不进入 JSON；`Code` 是稳定且可细化的机器码。底层 `Err` 只保留错误链，不直接输出。

秘密输出：

- DTO/presenter 对秘密字段显式使用 `cli.Secret`，不得靠关键词猜测任意字符串。
- `Secret.String()` 和 JSON marshal 永远脱敏；需要完整值时由输出投影显式调用 `Display(showSecrets)`。
- `NewURLSecret` 默认只保留 scheme/host，隐藏 userinfo、非根路径、query 和 fragment。
- 只有命令行显式 `--show-secrets` 可以使投影显示完整值，环境变量不得隐式开启。

A6 业务命令默认使用活动 legacy profile，可选 `--profile` 只用于显式消歧；无活动档案返回 `NOT_FOUND`。普通查询使用 table/json，测速与日志 follow 使用 text/NDJSON。NDJSON 每行必须是完整 JSON，最后包含 terminal 事件；取消必须传到 daemon 并关闭 HTTP body/channel，不得泄漏 producer goroutine。

`config edit` 不得让 systemd daemon 启动交互编辑器。CLI 通过 GET 取得配置内容与 SHA-256，在本地私有临时文件中调用 `EDITOR`，再通过 PUT 回传内容和 expected SHA-256；daemon 负责摘要冲突、原子写入、原生校验和失败恢复。

端口表格必须展示 field/port 或禁用/host/networks/conflict；修改成功明确区分“已重启并生效”和“已保存，下次启动生效”。`PORT_CONFLICT` 固定分类为 `conflict`、退出码 4，TUI core action 提示进入监听端口页，不得从错误文本反向解析 conflicts。

### 4. Validation & Error Matrix

| 条件 | `ErrorCategory` | 退出码 |
|---|---|---:|
| 未分类错误、缺少内部依赖 | `internal` | 1 |
| 未知命令、额外参数、非法 flag/输出格式 | `invalid_argument` | 2 |
| 资源不存在 | `not_found` | 3 |
| 状态或并发冲突 | `conflict` | 4 |
| daemon 不可用或协议不兼容 | `daemon_unavailable` | 5 |
| 配置、迁移或校验失败 | `validation_failed` | 6 |
| 权限不足或安全策略拒绝 | `permission_denied` | 7 |
| core、订阅、更新源等上游失败 | `upstream_failure` | 8 |

模式细化码分类固定为：`INVALID_ROUTING_MODE`/`REQUEST_ID_REQUIRED` -> 输入错误，`CONFIG_CHANGED`/`PROFILE_MODE_UNSUPPORTED` -> 冲突，`MODE_RUNTIME_MISMATCH`/`CONNECTION_CLOSE_FAILED` -> 上游失败，`RESTORE_FAILED` -> 内部错误。连接关闭 partial failure 必须同时保留返回的 `RoutingModeStatus`。

`errors.As` 必须能穿透包装后的 `*app.Error`。未知底层错误只输出“内部错误”，不得直接把 `err.Error()` 写到 stdout/stderr 或 JSON。

### 5. Good / Base / Bad Cases

- Good：`mm future-command --output json` 的 stdout 只有一份有效 JSON，stderr 为空，`warnings` 即使为空也是 `[]`。
- Base：`mm` 和 `mm tui` 在同一 fake runner 上各调用一次；`mm --help` 不调用 runner。
- Bad：未知命令返回 2，stdout 为空，stderr 不附带 usage，也不启动 TUI。
- Bad：秘密出现在 URL path/query、错误详情或表格字段时，默认输出不得包含原值。

### 6. Tests Required

- 根命令：无参数、`tui`、两级 help、未知命令、额外参数、未知 flag、IO 注入和 runner 错误映射。
- 输出：table/json writer 分离、稳定 envelope、空 warnings、非法格式。
- 错误：0-8 全部映射、wrapped `app.Error`、细化机器码与分类解耦、未知错误隐藏。
- 秘密：table、JSON、错误详情、URL 默认脱敏及显式展示。
- 领域/应用：空 ID、非法枚举以及每个 port 的 fake 编译期断言。
- 端口管理：六字段 table/json、禁用/冲突、未知字段/范围、请求 field/port、running/stopped 提示、`PORT_CONFLICT` 退出码 4，以及 TUI capability 延迟到 `tea.Cmd` 执行。
- 提交前执行：`go test ./...`、`go vet ./...`、`CGO_ENABLED=0 go build -trimpath -o /tmp/mm ./cmd/mm`、两个 help 冒烟。

### 7. Wrong vs Correct

#### Wrong

```go
cmd := &cobra.Command{RunE: func(*cobra.Command, []string) error {
    fmt.Println(subscription.URL) // 绕过 writer 注入并泄漏秘密
    return nil
}}
```

#### Correct

```go
secret := cli.NewURLSecret(subscription.URL)
result := cli.Result{
    Kind: "Subscription",
    Data: func(show bool) any {
        return map[string]any{"url": secret.Display(show)}
    },
    Table: func(w io.Writer, show bool) error {
        _, err := fmt.Fprintln(w, secret.Display(show))
        return err
    },
}
```

命令只做输入和展示适配；业务调用通过 `internal/app` port 完成，不能直接访问 `internal/mihomo`、配置文件或未来 SQLite。

## Scenario：模式 CLI 与 TUI daemon 表层

### 1. Scope / Trigger

新增或修改 `mm mode`、TUI 运行模式页面、TUI capability 注入或客户端本地编辑器时适用。目标是让 CLI/TUI 共用 daemon 单写者，且 Bubble Tea 状态机不执行网络、文件或进程副作用。

### 2. Signatures

```text
mm mode status [--profile ID] [--output table|json]
mm mode set <global|rule|direct> [--profile ID] [--close-connections] [--output table|json]
```

```go
func app.RunInteractiveContext(context.Context, io.Reader, io.Writer, tea.Model) error
func app.NewInteractiveCapabilities(app.CapabilityAPI, string) *app.InteractiveCapabilities
func tui.NewWithContext(context.Context, tui.Capabilities) tui.Model
```

`tui.Capabilities` 只组合页面实际使用的 mode/core/group/node/subscription/route/config/log 方法和客户端本地 `EditConfig`，不接受 `*mihomo.Client`。

### 3. Contracts

- status/set 成功 kind 固定为 `RoutingModeStatus` / `RoutingModeChange`；JSON 使用 `mm/v1` envelope，warnings 始终为数组。
- 模式命令没有秘密字段，不注册 `--show-secrets`；table 在状态字段之后输出 `警告:` 行。
- `cmd/mm` 只构造一个 `DaemonCapabilities`，CLI 直接使用，TUI 通过 `InteractiveCapabilities` 复用同一实例。
- TUI capability 调用必须封装为 `tea.Cmd`；`Update` 只转换消息状态，`View` 只渲染。测速和日志 follow 使用派生 context，离开页面时 cancel。
- TUI 配置编辑通过 daemon 读取 content/SHA，在客户端 `0700` 临时目录内创建 `0600` 文件，运行 `EDITOR` 后携带 expected SHA 回传；内容未改变不提交。
- mode partial failure 保留成功后的 typed status：CLI 放在 error `details.status`，TUI 更新已生效模式并单独显示关闭连接失败。

### 4. Validation & Error Matrix

| 条件 | 表层行为 |
|---|---|
| mode 非 `global|rule|direct` | `INVALID_ROUTING_MODE`，退出码 2，不调用 daemon |
| daemon unavailable | 退出码 5；提示 `mm daemon status` / `mm daemon start`，不 fallback |
| 无活动 legacy migration | `NOT_FOUND`，退出码 3；提示 `mm migrate plan` / `mm migrate apply` |
| unsupported profile | `PROFILE_MODE_UNSUPPORTED`，退出码 4；TUI 保留原选择 |
| runtime mismatch | `MODE_RUNTIME_MISMATCH`，退出码 8；不得显示切换成功 |
| restore failed | `RESTORE_FAILED`，退出码 1；提示状态可能不确定及 daemon/core status |
| connection close failed | `CONNECTION_CLOSE_FAILED`，退出码 8；保留已成功模式状态 |

### 5. Good / Base / Bad Cases

- Good：running core 切换后 CLI/TUI 同时显示 config/runtime mode、有效组/节点、连接数、规则集和 warnings。
- Base：stopped core 只显示配置已保存、下次启动生效，规则集标记为待启动后核验。
- Bad：daemon 不可用时 TUI 构造 `mihomo.Client`、读取 HOME 或直接调用 controller；任何一种都属于双写回退，禁止发布。

### 6. Tests Required

- CLI：help 不出现 `--show-secrets`；三枚举、非法输入、table/json kind、warnings、退出码和 partial `details.status`。
- TUI：mode load/set 在执行返回的 `tea.Cmd` 前不得调用 fake；覆盖 stopped、三项选择、close checkbox、partial/restore/daemon/migration 提示、Esc cancel 和窄终端控制行。
- 迁移回归：core/group/node/subscription/whitelist/route/config/log 使用 fake capability；配置编辑断言 `0600` 和 expected SHA；日志/测速离页触发 context cancel。
- 静态检查：`internal/tui` 不得导入 `internal/mihomo`，不得出现 `os.WriteFile`、`exec.Command` 或原始 HTTP。

### 7. Wrong vs Correct

错误：在 `Update` 中直接调用业务客户端，daemon 失败后回退到 legacy client。

```go
err := m.client.Start()
legacy := mihomo.New(config.Load())
```

正确：`Update` 返回命令，命令只调用注入的 daemon capability，结果再通过消息回到状态机。

```go
return m, func() tea.Msg {
    err := capabilities.CoreAction(ctx, "", "start")
    return actionDoneMsg{err: err}
}
```

## Scenario：节点历史与显式测速

### 1. Scope / Trigger

修改节点组 DTO、`mm node test`、`/v1/nodes/test*` 或 TUI 节点列表时适用。目标是让节点列表只读取 mihomo 运行时 history，测速必须由用户显式触发，并保证单测不会退化成整组测速。

### 2. Signatures

```text
mm node test [--group <group>] [--concurrency 5] [--limit 120]
mm node test --node <node> [--group <group>]
POST /v1/nodes/test
POST /v1/nodes/test-single
```

`app.NodeTestRequest` 以 `NodeID` 区分单测与批测；`Group.NodeStates` 按节点 ID 提供 `testable` 和可选的最新 `status/delay/testedAt`。旧 `nodes: []string` 字段保持不变。

### 3. Contracts

- TUI 进入节点列表只调用 group 查询并展示最新 history，不产生 `/delay` 请求；无 history 显示“未测速”。
- TUI 使用 `t` 测试光标节点、`a` 测试当前组全部可测速 leaf；批测固定低并发且 `limit=0` 表示不截断。
- 测速只更新结果，不自动切换节点；Enter 选择成功后不得清空 history 或启动测速。
- 测速中 Esc 取消并留在列表，保留部分结果；空闲 Esc 才返回。每轮异步消息携带 generation，取消或替换后丢弃旧流事件。
- 单测固定走 `/v1/nodes/test-single`，不得把 `nodeId` 仅加到旧批量 body；指定 group 时在请求 `/delay` 前验证成员关系和 leaf 可测速性。
- NDJSON event 保留 `done/total/name/delayMs`，追加 `status=success|failed` 与 RFC3339Nano `testedAt`；旧响应缺字段时按正 delay 推导 success，不伪造测试时间。
- history 归 mihomo 运行时所有，不写 SQLite，也不承诺跨 core 重启保留。

### 4. Validation & Error Matrix

| 条件 | daemon/stream code | CLI 行为 |
|---|---|---|
| `nodeId` 为空 | `INVALID_REQUEST` | 退出码 2，不发起 `/delay` |
| 节点或 group 不存在 | `NOT_FOUND` | 退出码 3 |
| 节点不属于显式 group，或不是可测速 leaf | `INVALID_REQUEST` | 退出码 2，不发起 `/delay` |
| 单测显式混用 `--limit` 或 `--concurrency` | 客户端拒绝，不请求 daemon | 退出码 2 |
| 单个探测失败或超时 | `status=failed` 节点 event，随后 `done` | 正常输出失败结果，不伪造正延迟 |
| controller/IPC 系统错误 | terminal `error` | 映射对应 app 分类与退出码 |
| context 取消 | 请求与 HTTP body/channel 关闭 | CLI 正常取消；TUI 留在节点列表 |

### 5. Good / Base / Bad Cases

- Good：进入列表立即看到最新 history；按 `t` 只产生一个节点请求，按 `a` 完整测试超过 120 个可测速 leaf，结果逐项刷新且当前选择不变。
- Base：core history 为空或旧 daemon 未返回 additive 字段时显示“未测速”；旧批量 event 缺 `status/testedAt` 时只按正 delay 推导 success。
- Bad：进入列表或 Enter 选择后隐式调用 `/delay`；把 `nodeId` 放进旧 `/v1/nodes/test` body 让旧 daemon 忽略后执行整组测速；两种行为都禁止发布。

### 6. Tests Required

- `internal/mihomo`：history 的 null/空/成功/失败、嵌套组过滤、单测恰好一次、成员拒绝、`limit=0` 超过 120 和取消。
- `internal/daemon` / `internal/app`：additive group 字段、新旧路由、status/time 解码、旧 event 兼容和取消。
- `internal/cli`：`--node`、可选 group、与显式批量参数冲突、text/NDJSON 成功失败及脱敏。
- `internal/tui`：进入列表零测速、相对时间、`t/a`、两阶段 Esc、generation 丢弃迟到事件、Enter 不测速和窄终端稳定行宽。

### 7. Wrong vs Correct

错误：单测复用旧批量路由，或进入节点页立即启动测速。

```go
client.OpenStream(ctx, http.MethodPost, "/v1/nodes/test", requestWithNodeID)
return m, startNodeTestCmd(ctx, service, groupID)
```

正确：`NodeID` 选择独立路由；group load 只把 history 投影到页面状态，返回空 command。

```go
if request.NodeID != "" {
    path = "/v1/nodes/test-single"
}
return m, nil
```
