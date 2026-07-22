# M5 实施计划：实时链路与入口诊断

## Task 1：连接 typed runtime 与快照 API

- [x] 定义 app/runtime connection DTO，集中解码 `/connections` 和 null/缺失字段。
- [x] 增加 daemon GET `/v1/connections`、app client 和响应大小/错误映射测试。
- [x] 实现 CLI 快照 table/json presenter。

验证：`go test ./internal/mihomo ./internal/daemon ./internal/app ./internal/cli -run Connection`。

## Task 2：NDJSON follow

- [x] 实现有界轮询 diff、open/update/closed 事件和取消/terminal 协议。
- [x] 增加 daemon stream、app decoder、CLI text/NDJSON 与 goroutine 泄漏测试。
- [x] 明确轮询间隔、上限和上游错误，不持久化连接。

验证：`go test -race ./internal/ipc ./internal/daemon ./internal/app ./internal/cli`。

## Task 3：入口检测与 warnings

- [x] 在 `internal/platform` 实现可注入的 GNOME read-only inspector。
- [x] 实现 proxy env 安全规范化、mihomo listener 协议匹配和 typed warning。
- [x] 接入 mode status，覆盖 disabled/unknown/matched/mismatched 与凭据脱敏。

验证：`go test ./internal/platform ./internal/app ./internal/daemon ./internal/cli`。

## Task 4：TUI 页面与最终质量门

- [x] 增加实时连接 TUI model、滚动、稳定列、ESC 取消和错误状态。
- [x] 在首页/模式页展示入口匹配结果，不添加任何系统代理写操作。
- [x] 更新 specs、README、根 `AGENTS.md`，执行全量/race/vet/双架构构建和隔离端到端验收。

## 回滚点

- 快照、follow、入口检测分层提交；follow 不稳定时可回滚流 route，保留已测试的快照能力但 milestone 不标完成。
- 任何隐私或取消泄漏问题未解决时不得发布实时连接页面。
