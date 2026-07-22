# 实施计划

1. 在 `internal/daemon/server_test.go` 增加真实 HTTP 时序回归测试：POST + request ID 的首条流事件必须在 handler terminal 前可读，并验证同 ID 流不缓存。
2. 修改 `internal/daemon/cache.go`：让内部 recorder 持有目标 writer、实现 `http.Flusher`、在首次 flush 时切换直通，并让 middleware 跳过二次提交与缓存。
3. 运行 `gofmt` 和 `go test ./internal/daemon`，修复状态/header/body/caching 回归。
4. 按 Trellis 质量门执行 `go test ./...`、`go test -race ./...`、`go vet ./...`、Linux amd64/arm64 `CGO_ENABLED=0` 构建及 `git diff --check`。
5. 更新 `.trellis/spec/backend/daemon-ipc.md` 与根 `AGENTS.md`，记录带 request ID 的 POST 流必须在 flush 时直通且不缓存。
6. 提交前复核只包含本任务文件，不安装二进制、不操作真实 daemon/Core；经用户确认后提交、归档并记录日志。
