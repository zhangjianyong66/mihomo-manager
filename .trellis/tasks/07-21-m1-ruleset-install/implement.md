# M1 实施计划：规则资产与 daemon 安装

## Task 1：路径、常量与安装状态

- [ ] 在 `internal/config` 和安装器中定义 `CONFIG_DIR/rulesets` 统一路径及覆盖变量校验。
- [ ] 增加固定 commit、两份 SHA-256 和 install-state 字段；同步卸载读取兼容。
- [ ] 增加路径、权限、符号链接和状态 round-trip 测试。

验证：`go test ./internal/config/...`，安装脚本状态单测。

## Task 2：规则集事务下载

- [ ] 实现两份资产的重试下载、摘要校验、旧缓存复验和同版本原子发布/恢复。
- [ ] 区分首次无缓存硬失败与升级有缓存告警继续。
- [ ] 使用本地 fixture 覆盖下载/摘要/发布中断，不访问真实网络。

验证：`bash scripts/tests/test_install.sh` 中规则资产场景。

## Task 3：daemon 与 fresh migrate 编排

- [ ] 安装 binary 后调用正式 daemon CLI enable/start/status，处理无 systemd 降级。
- [ ] 记录配置是否由本次安装创建；仅 fresh config 自动 migrate apply。
- [ ] core running 升级禁止 daemon restart，core stopped 才允许加载新 binary。
- [ ] 覆盖 daemon/migrate 替身调用顺序、退出码和幂等测试。

验证：隔离 HOME/systemd fake 的安装端到端。

## Task 4：文档与质量门

- [ ] 更新安装帮助、README、deployment spec 和根 `AGENTS.md`。
- [ ] 检查安装输出不宣称启动 core 或修改系统代理。
- [ ] 执行 Shell 与 Go 全量门禁并审查敏感信息。

验证：

```bash
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
go test ./...
go vet ./...
```

## 回滚点

- M1 独立提交。失败时恢复 installer/state schema 变更；旧 ruleset 只在完整新版本通过后替换。
- systemd/migrate 调用失败不得删除已存在 unit、数据库或用户配置；保留明确部分完成提示。
