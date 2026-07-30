# 执行计划

1. 激活任务并读取 `trellis-before-dev` 相关步骤。
2. 在 `internal/daemon/core_manager.go` 增加 ready 后 process watcher，使用 process identity 与状态保护 Stop/Start 竞态。
3. 在 `internal/daemon/core_manager_test.go` 增加 ready 后退出和显式 Stop 竞态回归测试。
4. `gofmt`、运行 daemon 相关测试和全量 `go test ./...`；必要时运行 `go vet ./...`。
5. 远程停用并隔离旧 `com.mihomo.monitor` LaunchAgent，重启/恢复新版 core，验证 daemon、端口和基础只读命令；记录 PID 与时间线。
6. 做最终 diff、状态和测试检查；按项目要求更新长期有效的 backend spec（仅在确有新契约时），提交中文 Git commit，并归档任务。

## 风险与回滚点

- watcher 若误处理 Stop 退出，会把 stopped 覆盖成 failed；测试必须覆盖 identity/state guard。
- 远程仅隔离旧 plist，不删除；如新版恢复失败，可恢复旧 plist 文件但不应重新启用其与新端口冲突的行为。

## 验证命令

```bash
gofmt -l internal/daemon/core_manager.go internal/daemon/core_manager_test.go
go test ./internal/daemon ./...
go vet ./...
```
