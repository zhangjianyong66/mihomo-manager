# 实施计划

1. 在 `internal/mihomo` 实现从 legacy YAML 构建 bootstrap routing 配置的纯函数，覆盖 manager provider/rule/DNS 移除、global 模式和原配置不变。
2. 在 `internal/core` 增加外部配置的私有 bootstrap generation 准备与清理，执行路径、权限、摘要和原生验证测试。
3. 在 `internal/daemon` 将 ruleset install 扩展为受 Coordinator 串行化的 bootstrap 事务，使用 Supervisor/CoreManager 管理临时进程，并在成功后激活正式 Core。
4. 扩展 typed install phases、HTTP 错误映射、CLI/TUI 展示和回归测试。
5. 运行格式、单元、race/vet、跨平台无 CGO 构建与真实 mihomo 原生配置验证；通过后以一次独立提交交付。

## 验证

- `test -z "$(gofmt -l cmd internal)"`
- `go test ./...`
- `GOTOOLCHAIN=go1.22.12 go test -race ./...`
- `go vet ./...`
- Linux/Darwin、amd64/arm64 的 `CGO_ENABLED=0 go build -trimpath ./cmd/mm`
- `MIHOMO_NATIVE_TEST=1 go test ./internal/mihomo`

## 回滚点

- 引导 generation 仅在成功时创建、失败时清理；开发中若发现 external config 只读或 CoreManager 恢复契约无法保持，先停止在 core/daemon 边界，撤销未提交代码改动。
