# M4 实施计划：模式 CLI 与 TUI

## Task 1：模式 CLI

- [ ] 增加 mode status/set Cobra 工厂、参数枚举、close flag 和 table/json presenter。
- [ ] 穿透 typed warnings 与稳定错误，补 help/IO/退出码/JSON envelope 测试。
- [ ] 更新根命令注册与非交互帮助冒烟。

验证：`go test ./internal/cli -run 'Mode|Root'`。

## Task 2：TUI daemon capability 迁移

- [ ] 重构 app TUI runner 依赖注入，移除内部 `mihomo.New` 和直接路径装配。
- [ ] 按页面迁移 core、group/node、subscription、whitelist/route、config/editor、logs 到 app capability。
- [ ] 保持流取消、配置编辑摘要、错误和现有键盘行为；删除发布路径中的 direct write/control。

验证：`go test ./internal/app ./internal/tui`，搜索 `internal/tui` 对 `internal/mihomo`、`os.WriteFile`、`exec.Command`、原始 HTTP 的依赖。

## Task 3：运行模式 TUI 页面

- [ ] 增加 status 加载消息、三模式选择、close checkbox、busy/result/warnings 状态。
- [ ] 显示配置/运行模式、有效路径、节点、规则集和 stopped/migrate/daemon 提示。
- [ ] 覆盖成功、失败、部分成功、ESC/取消和窄终端布局测试。

验证：`go test ./internal/tui -run Mode`。

## Task 4：兼容与质量门

- [ ] 更新 CLI/TUI 文档、cli-contract、directory-structure、project-conventions 和根 `AGENTS.md`。
- [ ] 执行全量 Go/race/vet/双架构构建及 `mm --help`、`mm mode --help`、`mm tui --help` 冒烟。
- [ ] 人工审查 TUI 无双写路径、输出无凭据、错误提示可执行。

## 回滚点

- CLI 命令和 TUI 页面分别提交；TUI 页面迁移按纵向页面提交但 milestone 只在全部写路径迁完后完成。
- 若某页面无法等价迁移，停止 M4 发布，不允许长期混用 daemon 和 direct client 写操作。
