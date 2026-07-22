# 修复 TUI 批量测速初始进度

## Goal

修复 TUI“节点管理 > 节点切换列表”按 `a` 启动批量测速后，在首个节点返回结果前错误显示“批量测速 0/0”的问题。用户按键后必须立即看到本组实际可测速节点总数，以确认批测已经生效。

## Background

- 2026-07-22 用户实测：按 `a` 后页脚显示“批量测速 0/0”，容易被理解为批测没有启动。
- `internal/tui/model.go` 的 `startNodeTest` 会在启动每条流时把 `switchTestDone`、`switchTestTotal` 都重置为 `0`。
- `internal/mihomo/node_runtime.go` 当前只在首个节点完成后发送带 `Total` 的结果事件；单节点探测最长可等待 3 秒，因此首批结果返回前 TUI 只能显示 `0/0`。
- 进入节点列表时，TUI 已从 `Group.NodeStates[].Testable` 构建 `nodeTestable`，并通过 `testableNodeCount()` 得到当前组可测速节点数；按 `a` 前也已经用该计数阻止空批次。

## Requirements

- R1：在节点列表按 `a` 启动或重启批量测速时，TUI 必须在同一次按键更新中把进度初始化为 `0/N`，其中 `N` 是当前已加载代理组的可测速节点数。
- R2：批测首个结果尚未返回时，页脚必须继续显示“批量测速 0/N”，不得回退为 `0/0`。
- R3：收到 daemon 测速流事件后，进度仍以事件中的 `Done` 和 `Total` 为准，避免运行时节点集合变化造成长期不一致。
- R4：现有单测队列、批测重启、单测/批测互相抢占、generation 丢弃迟到事件和 `Esc` 取消语义保持不变。
- R5：不修改 daemon、IPC、mihomo 测速流、3 秒单节点超时、批量并发数或批量范围。

## Acceptance Criteria

- [x] AC1：代理组包含两个可测速节点和至少一个不可测速节点时，空闲状态按 `a` 后、执行返回的 `tea.Cmd` 前，model 立即处于批测模式且进度为 `0/2`。
- [x] AC2：上述状态的 View 明确包含“批量测速 0/2”，不会显示“批量测速 0/0”。
- [x] AC3：批测进行中再次按 `a`，新一轮在首个结果前仍立即显示 `0/2`，并继续取消旧 context、使用新 generation。
- [x] AC4：收到首个流事件后，model 使用事件携带的进度覆盖初始化值；完成、错误、取消和迟到事件行为继续符合现有契约。
- [x] AC5：`go test ./internal/tui` 与 `go test ./...` 通过，现有节点测速测试无回归。

## Out of Scope

- 不给测速流新增 `progress` 事件，也不修改 CLI `mm node test` 输出。
- 不调整哪些节点属于可测速节点；继续使用当前节点详情中的 `Testable` 判定。
- 不处理真实 daemon/core 的安装、重启或部署验证。
