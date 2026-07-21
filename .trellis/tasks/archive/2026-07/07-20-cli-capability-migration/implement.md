# A6 实施计划：现有能力 CLI 等价迁移

## 1. 前置与进入门

- [x] 确认 A1-A5 已归档且工作树干净；读取 `trellis-before-dev`、backend 目录结构/代码风格/CLI/daemon/core 规范。
- [x] 为每个新增 DTO、IPC 路径和退出码建立唯一命名，搜索现有实现，避免重复 helper。
- [x] 所有测试使用 `t.TempDir()`、隔离 XDG 和 fake mihomo；禁止触碰真实 `~/.config/mihomo`。

## 2. 纵向执行顺序

### A6.1 共享应用与 IPC 基础

- [x] 扩展 `internal/app`：profile 解析、core/config/group/node/subscription/route/log service 实现和 daemon client adapter。
- [x] 将 daemon `CoreManager`、legacy Compatibility、mihomo runtime 包装为 app service；统一映射 `app.Error`。
- [x] 在 `internal/daemon/server.go` 注册非流式路由、请求 DTO 和 envelope；先完成 status/validate/list/select。
- [x] 目标验证：app fake 编译期断言、daemon 路由表和协议错误测试。

### A6.2 core/config 命令

- [x] 注册 `core status|start|stop|restart|reload|logs`、`config validate`，实现活动 legacy profile 默认解析和可选 `--profile`。
- [x] 接入 core supervisor 的状态、ready、失败恢复和日志 tail；保证无 daemon 不直连。
- [x] 目标验证：fake core、真实 daemon/socket 和 `--output table|json` CLI 契约。

### A6.3 组、节点和测速

- [x] 注册 `group list|show|select`、`node list|test|select`；实现当前组/节点展示、选择和并发/限制参数。
- [x] 将 `TestNodesStreamWithStop`/`TestGroupNodesStreamWithStop` 转为 context 事件；定义 text/NDJSON 单行协议和取消清理。
- [x] 目标验证：空组、未知节点、部分失败、超时、SIGINT、重复选择和 NDJSON 解析。

### A6.4 legacy 订阅

- [x] 注册 `subscription show|set|update`，URL 使用 `Secret` 投影，帮助文本说明单来源 legacy 语义。
- [x] 复用 A5 Compatibility 的 digest/backup/validate/restore；成功更新后执行明确 reload 策略并报告结果。
- [x] 目标验证：完整 YAML、URI/base64、网络失败、无效配置、秘密脱敏和旧文件不变。

### A6.5 legacy 路由与白名单

- [x] 注册 whitelist list/add/edit/remove、CN preset 和 route diagnose；补齐 edit 的输入/替换语义。
- [x] 复用 mihomo 白名单清理、去重、排序、唯一 MATCH 和配置回滚逻辑。
- [x] 目标验证：域名规范化、重复/空输入、mihomo 校验失败、诊断未知目标和摘要恢复。

### A6.6 legacy 配置与日志

- [x] 注册 config backup/restore/edit；编辑使用本地临时文件 + expected digest，daemon 负责验证、恢复和权限拒绝。
- [x] 完成 logs tail/follow 的过滤、取消、EOF/断流和敏感信息脱敏。
- [x] 目标验证：编辑器退出码、SIGINT、文件权限、日志 text/NDJSON 和 stderr 分流。

### A6.7 文档、矩阵与回归

- [x] 更新 `README.md`、`.trellis/spec/backend/cli-contract.md`、能力矩阵、`AGENTS.md` 和任务 journal。
- [x] 为已迁移能力标记 Alpha 已有；明确 legacy 单 profile 限制和 Beta/TUN/更新等未实现范围。
- [x] 运行完整质量门并记录输出。

## 3. 质量门

```bash
go test ./...
GOTOOLCHAIN=go1.22.12 go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/mm-a6-amd64 ./cmd/mm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/mm-a6-arm64 ./cmd/mm
test -z "$(gofmt -l cmd internal)"
git diff --check
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
```

另需执行隔离 HOME/XDG 的真实 daemon/Unix IPC 冒烟：`core status`、`config validate`、订阅 show、whitelist list、logs follow、node test（含取消）和 daemon 不可用错误。

## 4. 风险与回滚点

- **IPC DTO 漂移**：先添加协议测试和稳定 envelope，再接 CLI；路由注册按能力逐项提交。
- **旧 mihomo 行为回归**：保留原方法和测试，adapter 只包装不复制 YAML/HTTP 逻辑；失败时回退到未注册的新命令。
- **流式泄漏**：所有 producer 必须监听 context 并在 defer 中关闭 channel；race 测试失败即停止该纵向项。
- **敏感信息泄露**：新增字段默认使用 `Secret`，审查 fixtures、错误 details 和日志；发现泄露时不得进入下一项。
- **外部配置误写**：profile mode 检查和 digest 校验失败即拒绝；不通过时不扩大命令支持范围。

## 5. 完成门

- PRD 的 AC1-AC7 全部有测试证据；A6 命令不绕过 daemon，TUI 尚未迁移但可继续运行。
- 能力矩阵只把本任务覆盖的 legacy 能力标记为 Alpha 已有，其余维持部分/缺失。
- 质量门全部通过，变更已提交中文 Conventional Commit；随后归档 A6，A7 才可开始。
