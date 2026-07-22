# 修复 daemon 测速流缓冲

## Goal

修复 daemon 请求幂等缓存对 POST NDJSON 响应的整流缓冲，使 TUI 批量测速结果在每个节点完成时立即到达客户端并刷新进度，而不是全部测速结束后一次性出现。

## Background

- 用户实测批量测速长期停在 `0/162`，完成后进度消失且所有节点延迟一次性出现。
- `internal/mihomo/node_runtime.go` 已在每个节点完成时产生一条 `NodeTestEvent`；`internal/daemon/capability_http.go` 也在每条 NDJSON event 后调用 flush。
- `internal/daemon/cache.go` 会用只写入内存的 `responseRecorder` 包装所有带 `MM-Request-ID` 的非 GET/HEAD 请求。节点测速是带 request ID 的 POST，因此 recorder 不实现 `http.Flusher`，handler 的 flush 无效。
- middleware 只在 handler 完成后才通过响应 `Content-Type` 判断“不缓存流”，此时整个响应已经被内存 recorder 缓冲，来不及恢复流式时序。

## Requirements

- R1：带 `MM-Request-ID` 的 POST 流式响应在 handler 首次 flush 时必须把已写 header/status/body 立即发送到底层 `http.ResponseWriter`，不得等待 handler 返回。
- R2：进入直通模式后，后续 event 和 terminal event 必须按写入顺序继续发送并可逐次 flush；不得重复写 header 或已发送 body。
- R3：任何已经 flush/直通的响应不得写入 RequestCache；相同 request ID 再次发起流请求时必须重新执行 handler。
- R4：普通非流式 JSON 写请求继续由 RequestCache 缓冲后原子返回，并保持五分钟重放、同 ID 不同签名冲突、非 5xx 才缓存的现有语义。
- R5：修复必须基于响应 writer/flush 行为通用生效，不硬编码节点测速、日志或连接路由。
- R6：保持 NDJSON `seq`、`event|done|error`、终止事件、取消、单事件 1 MiB 上限和协议版本不变。

## Acceptance Criteria

- [x] AC1：受 RequestCache 包装的 POST handler 写出首条 NDJSON event 并 flush、随后阻塞时，真实 HTTP client 能在 handler 结束前取得 response 并读到首条 event。
- [x] AC2：解除阻塞后客户端继续读到 terminal event，header/status/body 均只发送一次且顺序正确。
- [x] AC3：使用相同 request ID 再次请求该流时 handler 调用次数增加，证明流响应未进入缓存。
- [x] AC4：现有 RequestCache 重放、签名冲突和过期测试继续通过，普通 JSON 幂等行为无回归。
- [x] AC5：节点测速现有 daemon/app/TUI 测试继续通过；`go test ./...`、`go test -race ./...`、`go vet ./...` 和 Linux amd64/arm64 无 CGO 构建通过。

## Out of Scope

- 不改变批量并发数、节点探测超时、节点排序或 TUI 文案。
- 不为流响应实现幂等重放或断点续传。
- 不安装新 `mm`、不重启真实 daemon/Core、不触碰用户配置；部署验证需另行明确授权。
