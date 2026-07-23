# 实施计划：GNOME 与 Bash 代理配置

## 阶段 1：路径、领域 DTO 与 platform 适配器

- [x] 在 `internal/config` 增加 `ManagerPaths.ProxyState` 与 `Bashrc`，补充 XDG/临时目录测试和 `AGENTS.md` 项目约定。
- [x] 在 `internal/platform` 抽取/复用代理 endpoint、listener 默认选择和 loopback/输入校验；新增 GNOME GVariant 读取、写入、ignore-hosts 合并、快照恢复和冲突检测。
- [x] 新增 Bash 标记区块解析、hash 冲突检测、NO_PROXY/no_proxy 合并去重、原子文件写入和权限校验。
- [x] 为 platform 适配器增加 fake command runner、临时 HOME/`.bashrc` 测试，覆盖 set/restore/disable、重复设置、外部修改、部分 gsettings 失败、无会话、IPv4/IPv6 和隐私边界。

## 阶段 2：daemon service、状态与 IPC

- [x] 新增 `ProxyConfigStatus`、请求/结果类型和 `CapabilityService` 的 system/env status/set/restore/disable 方法，复用 Coordinator 和 legacy listener ports。
- [x] 新增 4 条 `/v1/proxy/*` 路由，严格 method/body/request ID 校验、typed error code、request cache 和 JSON envelope。
- [x] 将 proxy state 原子文件纳入 daemon paths/装配，确保目录/文件权限与状态损坏/恢复失败语义一致。
- [x] 增加 daemon handler/unit/integration 测试：默认端口、显式 IP/port、all、Core stopped、快照生命周期、冲突、无快照、请求幂等/冲突和错误 details。

## 阶段 3：app capability、CLI 与输出

- [x] 扩展 `internal/app.CapabilityAPI`、DTO 转换、请求 ID 与错误映射；保持 CLI 进程只读取自身环境，不直接写用户文件。
- [x] 新增 `internal/cli/proxy.go`、命令参数校验、table/json presenter、帮助文本、stdout/stderr 和退出码测试。
- [x] 确认 `set` 参数必须成对出现，默认 listener 缺失返回 `INVALID_REQUEST`，不接受 domain/URL/userinfo。

## 阶段 4：TUI 入口

- [x] 在配置管理增加 GNOME/Bash 两个分层入口、状态视图和 target 选择。
- [x] 增加 IP/port 输入、all 默认/显式端点、确认页、应用/恢复/禁用命令和 typed 异步消息。
- [x] 确保 Update/View 不直接执行 gsettings/文件写入；确认取消、busy、冲突、无快照、nextStart 和窄终端布局。
- [x] 增加 TUI reducer/view/command 测试与静态边界检查。

## 阶段 5：质量门与文档

- [x] `gofmt -w` 所有 Go 改动，运行 `go test ./...`。
- [x] 运行 `GOTOOLCHAIN=go1.22.12 go test -race ./...`、`go vet ./...`。
- [x] 运行 Linux amd64/arm64 `CGO_ENABLED=0` 构建、CLI `--help`/命令冒烟和隔离 XDG daemon IPC 集成测试。
- [x] 运行 `bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh`，确认未改动遗留 Shell 入口。
- [x] 更新 `.trellis/spec/backend/route-observability.md` 或新增代理配置规范，补充根 `AGENTS.md` 的新路径/命令/权限约定。
- [x] 执行 PRD 全量验收映射，检查 CLI/TUI 共用 capability、无 Core 生命周期副作用、GNOME/Bash 回滚和隐私输出。

## 风险与回滚点

- gsettings 任一键写入失败：回滚已写键；回滚失败返回 `RESTORE_FAILED`，保留状态文件并要求人工处理。
- GNOME 当前值与 expected 不一致：返回 `CONFLICT`，不写任何键。
- `.bashrc` marker 冲突或原子 rename 失败：保留原文件，返回 `CONFLICT`/`PERMISSION_DENIED`。
- proxy state 持久化失败：回滚 GNOME 或 `.bashrc` 已发布内容，禁止写入半成品 state。
- 现有 mode status 诊断契约不得改变；若新增 DTO 字段，旧 JSON 客户端必须仍可解码。

## 开始实现前检查

- [ ] 用户已审阅 `prd.md`、`design.md`、`implement.md`。
- [ ] `task.py start` 前确认无未解决产品问题，确认本任务不拆子任务。
- [ ] 读取 `trellis-before-dev` 指定的 backend/CLI/daemon/TUI 规范后再修改代码。
