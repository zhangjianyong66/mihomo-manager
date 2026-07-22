# 端口冲突检测与修改实施计划

## Implementation

- [x] 统一安装器、旧 Shell 公共层、旧订阅补全和旧环境测试的 mixed 默认值为 `7890`，为 fresh config 增加安装回归断言。
- [x] 在 `internal/core` 增加结构化端口冲突错误，在 `internal/mihomo` 增加六类端口解析、单字段替换与 TCP/UDP 临时 bind 探测。
- [x] 在 `Adapter.Start` 接入 preflight，Supervisor 保存最近冲突；补充无冲突、单/多冲突、禁用端口、不同网络和“不创建进程”测试。
- [x] 扩展 CoreManager 为共享 Coordinator 内的配置重启事务，覆盖 stopped、running 成功、候选失败、端口冲突恢复和 RESTORE_FAILED。
- [x] 在 legacy/daemon 增加端口读取与 expected-digest 替换编排，新增 `/v1/config/ports` GET/PUT、请求 ID、错误 details 和集成测试。
- [x] 在 app capability 增加 typed DTO/客户端映射，确保 `PORT_CONFLICT` 分类为 conflict 且 details 不经字符串解析。
- [x] 实现 `mm config ports` 与 `mm config port set <field> <port>`，覆盖 table/json、参数校验、退出码和帮助冒烟。
- [x] 在 TUI 配置管理中增加监听端口页、异步加载、逐项输入、冲突标记、成功刷新与生效提示测试。
- [x] 更新 README、根 `AGENTS.md` 和相关 `.trellis/spec/backend` 契约，保留工作区中 legacy 迁移任务的既有改动。

## Validation

```bash
test -z "$(gofmt -l cmd internal)"
go test ./...
GOTOOLCHAIN=go1.22.12 go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/mm-port-amd64 ./cmd/mm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/mm-port-arm64 ./cmd/mm
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
git diff --check
```

## Live validation

仓库质量门通过后才执行：

1. 重新安装 `~/.local/bin/mm` 并重启 manager daemon，不停止或修改 xray。
2. 通过受管 port API 把真实 legacy `mixed-port` 从 `10808` 改为 `7890`。
3. 确认配置权限 `0600`、mihomo 原生校验通过、core running 时完成受控重启。
4. 确认 mihomo 监听 `7890/7891/9090`，xray 继续监听 `10808`。
5. 通过临时占用一个测试端口或隔离配置验证 `PORT_CONFLICT`，不得破坏真实代理入口。

## Risk and rollback points

- CoreManager 与 legacy 配置事务是最高风险边界；先完成其单元测试，再接 IPC/UI。
- 任一运行中失败路径必须断言配置内容、migration expected 摘要、core 状态和旧进程恢复结果，不能只断言 error 非空。
- 不运行依赖本机状态的 `tests/test.sh` 作为质量门；只做 Shell 语法检查。
- 真实切换前记录配置摘要和 core 状态；受管事务失败时停止后续 live 操作并保留日志/operation 证据。
