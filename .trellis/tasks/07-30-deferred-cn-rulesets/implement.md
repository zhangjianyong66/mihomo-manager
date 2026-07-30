# 支持延迟安装 CN 规则集：实施计划

## 1. 共享清单与规则集内核

- [x] 新增可嵌入且可被 Shell 读取的 catalog，集中默认 source/ref/path/behavior/SHA-256。
- [x] 新增 `internal/ruleset` 的目标解析、metadata、四态 status、路径/权限/摘要/格式校验和旧 installer state 兼容读取。
- [x] 实现 context-aware HTTP/HTTPS/SOCKS/file 下载、响应大小限制、有限重试和临时文件清理。
- [x] 实现 domain/IP/metadata 三文件发布、权限、锁内二次幂等检查和完整恢复注入点。
- [x] 为 catalog、状态矩阵、代理脱敏、下载限制、pair 校验、发布失败和恢复失败编写临时目录/httptest 单元测试。

## 2. 路由与依赖门禁

- [x] 将 manager provider 改为本地 `type:file`，并只在 Rule policy 注入 CN providers/rules/DNS。
- [x] 提供唯一的配置引用检测与 ruleset readiness 入口，接入 Core validate/start/restart/reload、mode Rule、CN preset，以及 Rule 下会重写 policy 的订阅/白名单/config 操作。
- [x] 增加 `RULESET_NOT_READY` typed error 和 details status；验证 global/direct、最小 Rule、自定义非 manager provider 不受影响。
- [x] 扩展 routing policy、legacy mode/config/subscription、adapter/CoreManager 回归测试，断言缺失时零隐式网络和零配置副作用。

## 3. daemon 与 IPC 事务

- [x] 在 daemon capability 装配 ruleset service，共享 Coordinator、CoreManager runtime 和 config/manager paths。
- [x] 实现 status GET、install POST NDJSON、request ID、progress phase、terminal event 和结构化错误映射。
- [x] 实现下载期可取消、发布后独立有界 context、running Rule reload/verify、失败恢复与 core failed。
- [x] 以真实临时 Unix/HTTP 测试覆盖请求体限制、方法、缺 request ID、流式 flush、不缓存、并发幂等、取消边界及恢复状态。

## 4. app、CLI 与 TUI

- [x] 在 app 定义共享 source/proxy/status/event DTO；客户端解析 `MM_RULESET_*` 和代理优先级，拒绝认证代理并解码 install stream。
- [x] 新增 `mm ruleset status|install`，实现 table/json envelope、最终 warnings、帮助、退出码与可执行错误提示。
- [x] 在“配置管理 > CN 规则集”实现状态、确认、进度、取消锁定、终态刷新和窄终端视图，所有副作用延迟到 `tea.Cmd`。
- [x] 扩展 fake capability、CLI/TUI 测试，覆盖四态、复用、每个阶段、确认零副作用、发布前后取消和秘密不泄露。

## 5. 安装器与兼容

- [x] 增加 `--with-rulesets`/`MM_INSTALL_RULESETS` 参数解析；默认路径完全跳过网络，显式路径保持失败非零。
- [x] 让 Shell 从共享 catalog 读取默认值，离线识别/迁移旧 cache，维护 metadata 和兼容 install-state。
- [x] 更新安装摘要、usage、Makefile/README；默认缺失时给出 `mm ruleset install`，有效 cache 时准确报告复用。
- [x] 扩展 `scripts/tests/test_install.sh` 覆盖默认离线成功、零下载、显式完整安装失败/成功、旧 cache、自定义来源、metadata 和摘要输出。

## 6. 规范与验证

- [x] 更新 deployment、project conventions、CLI/daemon IPC、routing/core 相关 Trellis spec 和根 `AGENTS.md` 的长期契约，删除旧“默认强制下载/HTTP provider 自动更新”描述。
- [x] 运行 `gofmt -w`（仅改动 Go 文件）并确认 `gofmt -l` 零输出。
- [x] 运行 `go test ./...`、`GOTOOLCHAIN=go1.22.12 go test -race ./...`、`go vet ./...`。
- [x] 运行 Linux/Darwin amd64/arm64 的 `CGO_ENABLED=0 go build` 与 `mm ruleset --help`/子命令冒烟。
- [x] 运行 `bash scripts/tests/test_install.sh` 和 `bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh`。
- [x] 最终复核无真实 HOME/config/socket/Core 副作用、无秘密值输出，并检查 git diff 只包含本任务范围。

## 回滚点

- catalog/metadata schema 在发布前可整体撤回，不修改 SQLite。
- provider 改为 `type:file` 必须与 readiness gate 和新安装入口同批发布，不能单独提交中间状态。
- daemon 事务失败必须通过注入测试证明旧 pair/runtime 可恢复；无法证明时停止发布，不以警告替代。
