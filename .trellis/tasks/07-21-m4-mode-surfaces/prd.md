# M4 提供模式 CLI 与 TUI

## Goal

通过统一 daemon capability 为脚本和终端用户提供可理解、可核验的模式状态与切换入口，并完成 TUI 写操作的单写者迁移。

## Dependencies

- 依赖 M3 的 mode app/IPC 契约和事务实现。

## Requirements

- CLI 提供 `mm mode status` 与 `mm mode set global|rule|direct [--close-connections]`。
- status/set 支持 table/json，JSON 使用 `mm/v1` envelope、稳定 kind、warnings 和既有退出码。
- table 输出区分配置模式、实际运行模式、core 状态、有效组/节点、活动连接和规则集健康。
- TUI 增加运行模式页面，提供三项单选、行为说明、当前实际路径、切换结果和 warnings。
- core stopped、daemon unavailable、未 migrate、unsupported profile、runtime mismatch 和 restore failed 均有明确中文状态，不伪报成功。
- TUI 所有现有写操作迁移到 app/daemon capability；发布时不得继续通过 `*mihomo.Client` 直接写配置、控制 core 或调用 controller。
- 保留测速、日志流、编辑器、取消和错误展示能力；业务副作用不进入 Bubble Tea View/Update。

## Acceptance Criteria

- [ ] M4-AC1：CLI 三模式参数、help、table/json、非法输入、warnings 和退出码契约测试通过。
- [ ] M4-AC2：core running/stopped 的 status 文案准确；set 成功、部分成功和恢复失败输出不混淆。
- [ ] M4-AC3：TUI 能查看和切换三模式，选中状态、有效组/节点及“下次启动生效”展示正确。
- [ ] M4-AC4：`mm` 与 `mm tui` 使用同一注入的 daemon capability，fake 测试不读取真实 HOME/socket/core。
- [ ] M4-AC5：TUI 的 core/group/node/subscription/route/config/log 写路径均不再依赖 `mihomo.Client` 或 legacy pgrep/pkill。
- [ ] M4-AC6：daemon 不可用或未迁移时提供 `mm daemon start`、`mm migrate plan/apply` 的可执行提示。
- [ ] M4-AC7：现有 TUI 测速/日志取消、白名单编辑和配置编辑回归测试通过。

## Out of Scope

- TUI 视觉系统重做或鼠标支持。
- 实时连接页面；由 M5 完成。
- 自动启动 core、自动迁移已有配置或接管系统代理。
