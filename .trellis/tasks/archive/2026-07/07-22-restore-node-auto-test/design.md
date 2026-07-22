# 节点测速交互重设计

## 设计目标

- 节点列表加载只读取 mihomo 现有运行时 history，不调用任何 `/delay` 接口。
- 单节点和整组测速都经 daemon capability，CLI 与 TUI 共用类型化请求和事件。
- 保持现有 `/v1/nodes/test` 批量调用兼容，不让旧 daemon 把新单节点请求误解释为批量请求。
- 测速结果只影响展示，不自动切换代理组选择，也不写入 SQLite。

## 数据流

### 历史结果

```text
mihomo GET /proxies
  -> internal/mihomo 类型化 group/node/history
  -> internal/legacy 只读兼容边界
  -> daemon GroupInfo.nodes + additive nodeStates
  -> app.Group.NodeStates
  -> TUI 节点列表最新结果/相对时间
```

`GroupInfo.nodes` 继续保留字符串数组，避免破坏现有 `mm/v1` 消费方；新增 `nodeStates` 数组，按 `nodes` 顺序返回：

```go
type NodeTestStatus string // success | failed

type NodeTestResult struct {
    NodeID    domain.NodeID
    Status    NodeTestStatus
    Delay     time.Duration
    TestedAt  time.Time
}

type GroupNodeState struct {
    NodeID    domain.NodeID
    Testable  bool
    Latest    *NodeTestResult
}
```

`Latest == nil` 表示未测速。history 最新条目的 `delay > 0` 映射为 `success`，非正值映射为 `failed`。时间使用 RFC3339Nano 经 IPC 传输，在 app 边界解码为 `time.Time`；TUI 不解析原始 JSON。

`internal/mihomo` 新增一次性类型化的 group detail 解析，复用统一的 proxy type 判定来标记 leaf 节点是否可测速。`DIRECT`、`REJECT`、说明项和嵌套代理组均不可被单测或批测；列表仍可展示并选择原本允许选择的项。

### 显式测速

```text
TUI t / CLI --node
  -> app.NodeTestRequest{ProfileID, GroupID, NodeID}
  -> POST /v1/nodes/test-single
  -> daemon/legacy membership + testable validation
  -> mihomo GET /proxies/<node>/delay
  -> NDJSON NodeTestEvent(status, delayMs, testedAt)

TUI a / CLI batch
  -> app.NodeTestRequest{ProfileID, GroupID, Concurrency, Limit}
  -> existing POST /v1/nodes/test
  -> filtered current-group leaf nodes
  -> bounded concurrent /delay calls
  -> NDJSON events + terminal done
```

单节点使用新的 `/v1/nodes/test-single` 路由，body 包含 `profileId`、`groupId` 和必填 `nodeId`。不在旧 `/v1/nodes/test` body 中仅增加可忽略字段：旧 daemon 若忽略 `nodeId`，会把用户的单测错误执行为整组测速；独立路由会让旧 daemon 明确返回不支持。

`app.NodeTestRequest` 增加 `NodeID`，`DaemonCapabilities.TestNodes` 根据其是否为空选择单节点或批量路由。daemon service 和 legacy adapter 仍使用一个类型化请求入口，验证所有权集中在 mihomo adapter：

- `NodeID` 非空时只允许一个确切节点；`GroupID` 非空时先验证成员关系。
- 节点不存在、不属于指定组或不是 leaf 可测速节点时，在发起 `/delay` 前返回结构化输入/不存在错误。
- 单测流的 `Total` 固定为 1；探测失败作为该节点的 `failed` 结果返回，随后发送 done，不把整条流伪装成成功延迟。
- 批量 `Limit == 0` 表示不截断。TUI 使用 concurrency 5、limit 0；CLI 原有默认 limit 120 保持兼容。

NDJSON event 在现有 `done`、`total`、`name`、`delayMs` 基础上追加 `status` 和 `testedAt`。成功事件继续保留正数 `delayMs`；失败事件使用 `status=failed`，CLI text 输出“失败/超时”而不是负毫秒。系统性 controller/IPC 错误仍走 stream error event。

## TUI 状态机

节点页使用独立测速状态，不复用全页加载的 `busy`：

- `nodeResults map[nodeID]NodeTestResult`：进入页面时由 group history 初始化，随后被当前会话事件覆盖。
- `testActive`、`testDone`、`testTotal`、`testCancel`：控制进度和 Esc 行为。
- `testGeneration`：每次开始、替换或取消测速时递增；异步 started/event 消息携带 generation，旧流迟到消息直接丢弃，防止污染新页面。
- `testMessage`：在页脚附近显示不可测速、取消或系统性失败等短状态，不跳转到结果页。

交互规则：

- 进入 group node list：填充 history，返回 `nil` command，不调用 `TestNodes`。
- `t`：若光标是可测速节点，取消旧流并启动该节点单测；“返回”或不可测速项不发请求并显示原因。
- `a`：取消旧流并启动当前组全部可测速 leaf 节点批测，concurrency 5、limit 0。
- 测速中 `Esc`：取消、递增 generation、保留部分结果并停留；空闲时 `Esc` 返回组列表。
- 测速中再次按 `t`/`a`：取消旧流并以新 generation 替换，避免并行 producer 竞争同一展示状态。
- `Enter`：只执行节点选择。选择成功后更新当前标记，不清空 history、不自动重测。

列表每行只显示最新状态：`238ms · 9分钟前`、`失败 · 刚刚` 或 `未测速`。相对时间由可注入 clock 计算，未来时间按“刚刚”处理，不设置任意过期阈值。节点名称过长时为状态区域保留稳定宽度并按 rune 截断，避免终端自动换行破坏分页。

页脚空闲时显示 `t 单测 | a 批量`；运行时追加 `进度 done/total | Esc 取消`。完成或取消后保留结果和进度摘要。

## CLI 契约

- 新增 `--node <id>`；可与 `--group <group>` 一起使用。
- `--node` 模式固定测试一个节点。显式同时传入 `--limit` 或 `--concurrency` 时返回输入错误，避免静默忽略批量参数；未显式传入的默认值不影响单测。
- text 成功/失败都输出一行节点结果和完成摘要；NDJSON 保持 `NodeTestEvent`、`NodeTestComplete` kind，并追加稳定的 `status`、`testedAt` 字段。
- 节点名继续走既有脱敏投影，只有 `--show-secrets` 才显示完整值。

## 兼容与错误处理

- `/v1/nodes/test` 的批量 body 和行为保持兼容；`nodeStates`、event 新字段均为 additive。
- 新客户端连接旧 daemon 时，历史字段缺失会显示“未测速”；单节点新路由返回明确错误，不会回退到批量或直连 mihomo。
- 新 daemon 继续接受旧客户端的批量请求。
- 取消是正常终止：TUI 留在列表，CLI 保持既有取消成功语义；取消后的旧 generation 消息不得改变状态。
- 不新增路径、环境变量、SQLite schema、迁移或长期缓存。

## 测试策略

- `internal/mihomo`：类型化解析 success/failed/null history、leaf/group 判定、成员校验、单测恰好一次 `/delay`、批量 limit 0 超过 120、取消和失败事件。
- `internal/legacy`：group details 与单/批测请求透传，不接触真实 controller。
- `internal/daemon`：group additive `nodeStates`、新单测路由方法/body/错误、NDJSON status/time、旧批量路由兼容及请求取消。
- `internal/app`：两类路由选择、时间/status 解码、缺失 additive 字段兼容、取消。
- `internal/cli`：`--node`、`--group` 成员失败、参数冲突、text/NDJSON success/failed、脱敏和退出码。
- `internal/tui`：进入列表零测速调用、history/相对时间、`t`/`a`、不可测速项、全组不截断、Esc 两阶段行为、generation 丢弃迟到事件、Enter 不触发测速和窄终端行宽。

## 回滚

本次无数据迁移。回滚只需恢复新增 DTO/路由/状态机和文档；因为未持久化 history，也不会留下需要清理的用户状态。若新单测路由存在问题，可保持旧批量路由工作，不启用任何直连 fallback。
