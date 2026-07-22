# 实施计划

## 1. 类型化 mihomo 节点状态

- [x] 在 `internal/mihomo` 增加 group node/history 类型，类型化解析 `/proxies`，统一 leaf/testable 判定，保留现有 group 顺序和选择值。
- [x] 增加精确单节点测速流：可选 group 成员校验、不可测速拒绝、成功/失败状态与测试时间；复用既有 `/delay` URL、超时和 stop channel。
- [x] 修正 group 批量过滤以只包含全部可测速 leaf 节点；`limit=0` 不截断，保持 concurrency 有界。
- [x] 添加 `httptest.Server` 单元测试，覆盖 null/空/多条 history、失败延迟、嵌套组、超过 120 节点、取消和无请求副作用。

## 2. 扩展 app、legacy 与 daemon 契约

- [x] 在 app DTO 中增加 NodeID 请求、节点 testable/latest 状态和 typed success/failed 结果时间。
- [x] 在 legacy compatibility 中增加 group detail 和单节点测速透传，不新增 controller 直连路径。
- [x] 为 `GroupInfo` 添加 additive `nodeStates`，保持 `nodes: []string` 不变。
- [x] 新增 `POST /v1/nodes/test-single`，严格校验 nodeId；保留 `/v1/nodes/test` 批量契约，事件追加 `status`/`testedAt`。
- [x] 更新 daemon/app 集成测试，覆盖新旧路由、成员错误、不可测速、取消、additive 字段与旧响应缺字段。

## 3. 补齐 CLI 单节点测速

- [x] 为 `mm node test` 增加 `--node`，支持可选 `--group`；拒绝单测与显式批量参数混用。
- [x] 调整 text/NDJSON formatter，成功保留毫秒，失败显示 typed 状态并输出测试时间；保持脱敏、终止事件和退出码契约。
- [x] 添加 CLI 工厂测试和 `--help` 冒烟断言。

## 4. 重构 TUI 节点测速状态机

- [x] 用 typed result map、active/progress/cancel/generation/message 替换 `switchNodeDelay` 的页面临时整数状态。
- [x] group load 只装载 history；删除进入列表和节点选择成功后的隐式测速 command。
- [x] 实现 `t` 单测、`a` 全组批测、运行中 Esc 只取消、空闲 Esc 返回；新测试替换旧测试并忽略迟到 generation。
- [x] 渲染最新延迟/失败/未测速与相对时间，动态页脚展示快捷键、进度和取消提示；保证窄终端下行宽稳定。
- [x] 添加 Bubble Tea 状态机测试，覆盖无自动探测、按键、取消、迟到事件、部分结果保留、Enter 选择和不可测速项。

## 5. 文档与项目契约

- [x] 更新 README 的节点管理、CLI 示例和快捷键，删除“自动切换最快节点”表述。
- [x] 更新 `.trellis/spec/backend/cli-contract.md`，记录单节点路由、additive event/history 字段和 TUI 显式触发/取消契约。
- [x] 将稳定的节点测速运行约定同步到根 `AGENTS.md`，不记录临时调试数据或真实节点名。

## 6. 验证门禁

- [x] `gofmt -w` 仅格式化本次修改的 Go 文件，并确认 `test -z "$(gofmt -l cmd internal)"`。
- [x] 运行相关包测试：`go test ./internal/mihomo ./internal/legacy ./internal/daemon ./internal/app ./internal/cli ./internal/tui`。
- [x] 运行全量测试：`go test ./...`。
- [x] 运行 `go vet ./...` 和 `GOTOOLCHAIN=go1.22.12 go test -race ./...`。
- [x] 执行 Linux amd64/arm64 无 CGO 构建，输出到 `/tmp`，不覆盖仓库或已安装的 `mm`。
- [x] 执行 `go run ./cmd/mm node test --help`、`go run ./cmd/mm --help` 和 `go run ./cmd/mm tui --help` 冒烟。
- [x] 使用隔离的 httptest/Unix IPC 验证“进入列表无 `/delay` 请求、单测恰好一个、批量超过 120 不截断”；不连接或改写真实用户配置/core。

## 风险与回滚点

- IPC 兼容风险：单节点必须走新路由，禁止把 `nodeId` 仅作为旧批量 body 的可忽略字段。
- 异步竞态风险：每个 started/event/done 消息必须核对 generation；取消后先失效 generation 再调用 cancel。
- 数据一致性风险：`nodes` 与 `nodeStates` 必须按 ID 对齐；缺失状态按未测速处理，不能按数组下标盲配。
- 过滤风险：single 与 batch 必须复用同一 testable 判定，避免嵌套组在一条路径可测、另一条路径不可测。
- 回滚点：每完成 mihomo、daemon/app、CLI、TUI 一层先运行对应测试；任一层失败时恢复该层 additive 变更，不修改 SQLite 或用户文件。
