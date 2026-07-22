# 实施计划

## 1. 补齐 TUI 测速状态

- [x] 在 `internal/tui/model.go` 增加明确的 idle/single/batch 模式、当前单测节点和有序去重队列。
- [x] 集中实现节点是否正在执行/已排队、队列清理和页内状态重置，避免多个按键分支各自维护字段。
- [x] 保持 capability 调用全部封装为 `tea.Cmd`，不在 `Update` 或 `View` 内直接执行网络副作用。

## 2. 实现按键与流转

- [x] `t` 在 idle 启动单测，在 single 入队，在 batch 取消并切换单测；重复项和不可测速项给出可见提示。
- [x] `a` 在任意模式清空队列、取消当前流并立即启动 `concurrency=5`、`limit=0` 的批量测速。
- [x] 单测正常完成或异常 close 后按顺序启动队首；失败结果继续，系统性错误停止队列。
- [x] `Esc` 清空队列并取消当前流；旧 generation 的所有消息继续被严格忽略。

## 3. 改进状态反馈

- [x] 页脚区分单测、批量和 idle，单测显示当前节点及待测数量，active 状态也显示入队/重复提示。
- [x] 复用现有宽度约束和字符串截断，验证窄终端不溢出或覆盖列表。

## 4. 增加回归测试

- [x] 扩展 `fakeTUIService` 支持每次调用独立、可长期保持未完成的 channel，并记录各请求 context。
- [x] 覆盖单测中连续选择并按 `t` 的队列顺序、无取消、去重和即时页脚反馈。
- [x] 覆盖单测完成自动启动下一项、失败结果继续、系统错误停止，以及异常 channel close。
- [x] 覆盖 single→batch、batch→single、batch→batch 的取消、请求参数和 generation 迟到消息隔离。
- [x] 覆盖 Esc 清队列两阶段行为、结果保留和窄终端页脚。

## 5. 同步稳定契约

- [x] 更新 `.trellis/spec/backend/cli-contract.md` 的节点历史与显式测速场景。
- [x] 更新根 `AGENTS.md`；必要时同步 README 的用户操作说明。

## 6. 验证

- [x] `gofmt -w internal/tui/model.go internal/tui/model_test.go`
- [x] `go test ./internal/tui`
- [x] `go test ./...`
- [x] `GOTOOLCHAIN=go1.22.12 go test -race ./...`
- [x] `go vet ./...`
- [x] `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/mm`
- [x] `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/mm`
- [x] `git diff --check`

## Risk and Rollback Points

- Bubble Tea 一条完成消息返回下一条 command 的状态丢失风险：测试必须先断言新 model 字段，再执行 command，并覆盖迟到旧消息。
- 取消竞态风险：所有 started/event/close/error 都按 generation 过滤，旧消息不得清理新 cancel 或新队列。
- 测试泄漏风险：阻塞 fake channel 必须由 context 取消或测试清理关闭，race 测试不得遗留 goroutine。
- 若实现扩大到 `internal/app`、daemon 或 IPC，先返回规划阶段补充跨层设计；当前计划不授权该扩展。
