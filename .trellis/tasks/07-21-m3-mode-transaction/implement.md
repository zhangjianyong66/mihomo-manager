# M3 实施计划：模式事务与 daemon API

## Task 1：领域、应用与 runtime 契约

- [ ] 新增 `domain.RoutingMode`，补 Validate/JSON/非法值测试。
- [ ] 新增 app status/request DTO 与 Mode capability interface。
- [ ] 扩展 mihomo runtime 的 mode/rules/count/close 类型化方法及 HTTP 错误测试。

验证：`go test ./internal/domain ./internal/app ./internal/mihomo`。

## Task 2：legacy 模式事务

- [ ] 在共享 profile 操作锁下实现候选捕获、M2 policy 应用、原生验证、原子发布和 expected 刷新。
- [ ] 实现 stopped 保存与 running set/reload/verify 分支。
- [ ] 实现文件/runtime 双向补偿、RESTORE_FAILED 和 close partial-success。
- [ ] 记录 operation phases，覆盖所有注入失败点和并发冲突。

验证：`go test ./internal/legacy ./internal/daemon -run 'Mode|Routing|Restore|Conflict'`。

## Task 3：daemon HTTP 与 app IPC client

- [ ] 注册 GET/PUT `/v1/mode`，严格校验方法、body、profile、mode 和 request ID。
- [ ] 在 `DaemonCapabilities` 映射 typed DTO、warnings 和稳定错误。
- [ ] 添加真实临时 Unix socket 集成、幂等重放和协议错误测试。

验证：`go test ./internal/ipc ./internal/daemon ./internal/app`。

## Task 4：全量质量与契约文档

- [ ] 更新 cli/app、daemon-ipc、project-conventions、core-adapter spec 和根 `AGENTS.md`。
- [ ] 搜索并消除 daemon mode 写路径对 legacy `pkill`/原始 HTTP/map 的依赖。
- [ ] 执行全量、race、vet 和双架构无 CGO 构建。

## 回滚点

- M3 独立提交。HTTP route 注册可整体移除，M2 的静态 policy 仍可保留。
- 任何 restore 测试不稳定时停止发布 mode mutation，只保留只读 status 直到事务可证明恢复。
