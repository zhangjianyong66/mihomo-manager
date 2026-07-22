# daemon POST 流式响应直通设计

## Problem Boundary

问题位于 `internal/daemon.RequestCache.Middleware`，不是 mihomo 探测并发、NDJSON 编解码或 TUI 状态机。当前 middleware 必须在 handler 完成前拦截响应以决定是否缓存，但纯内存 `responseRecorder` 同时破坏了 `http.Flusher` 的时间语义。

## Design

扩展请求缓存内部的响应 recorder，使其同时持有最终目标 `http.ResponseWriter`，并实现 `http.Flusher`：

1. 初始为 buffered 模式，header/status/body 留在 recorder，普通 JSON 行为不变。
2. handler 调用 `Flush()` 时，recorder 将当前 header、status 和缓冲 body 提交给目标 writer，清空本地 body，并标记为 passthrough。
3. passthrough 模式下后续 `Write` 直接写目标 writer，后续 `Flush` 直接调用底层 flusher。
4. handler 返回后，middleware 若发现 recorder 已 passthrough，立即返回，不再复制 body或调用 `store`；否则沿用现有普通响应提交和缓存逻辑。

是否流式不再依赖路由白名单。对当前代码而言只有 NDJSON handler 主动 flush；更一般地，任何 handler 主动 flush 都表示选择实时传输，因此应退出幂等响应缓存。

## State And Invariants

- `WriteHeader` 只接受第一次有效调用；默认 status 为 200。
- 首次 `Write` 仍遵循 net/http 语义，未显式写 header 时使用当前默认 status。
- buffered -> passthrough 只允许单向转换。
- 转换时先复制 header，再写 status，再写已有 body，最后调用底层 flush。
- passthrough 后 middleware 不得再次提交或缓存响应。
- 底层不支持 `http.Flusher` 时仍提交已有内容并进入 passthrough；真实 Go HTTP server 支持 flusher，测试应显式断言 wrapper 暴露该能力。

## Compatibility

- 不修改公开 Go 接口、IPC 路由、payload、protocol version 或 NDJSON event 格式。
- 普通 JSON 幂等缓存条目结构和 lookup/store 签名保持不变。
- 流请求此前按契约本就不应缓存，本次只修复其传输时序，属于兼容性 bug fix。

## Test Strategy

在 `internal/daemon/server_test.go` 使用真实 `httptest.Server` 和 POST request ID：handler 写首条行并 flush 后阻塞。测试必须在解除阻塞前完成 `client.Do` 并读到首条行，随后读 terminal 行；再次使用相同 ID 请求并断言 handler 再次执行。现有 RequestCache 单元测试负责守住普通 JSON 重放/冲突/过期行为。

## Rollback

改动只涉及 `internal/daemon/cache.go` 的内部 writer。若验证失败，可整体回退 writer 的 passthrough 状态与对应回归测试，不涉及数据迁移、协议升级或用户状态恢复。
