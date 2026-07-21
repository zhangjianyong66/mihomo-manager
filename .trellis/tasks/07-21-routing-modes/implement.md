# 总体实施计划：三模式路由与实时链路

## 执行规则

本任务是规划父任务，不直接作为实现目标。每次只启动用户明确指定的 milestone；当前 milestone 完成测试、提交和归档后，才进入下一个依赖 milestone。不得一次会话自动连续执行全部 milestone。

## Milestone 顺序

1. [M1：安装 CN 规则集与用户 daemon](../07-21-m1-ruleset-install/implement.md)
   - 固定并原子安装两份 CN `.mrs`，扩展 install-state/测试。
   - 安装启用 manager daemon，并区分新建配置自动迁移与已有配置显式迁移。
2. [M2：构建 Rule 分流与订阅保留](../07-21-m2-rule-policy/implement.md)
   - 建立唯一 RoutingPolicy 解析/合成器、DNS 模板和 native validation。
   - 让白名单、CN 预设和订阅更新共用策略并保持状态。
3. [M3：实现模式事务与 daemon API](../07-21-m3-mode-transaction/implement.md)
   - 增加 RoutingMode 领域/app/runtime 契约。
   - 实现 daemon 单写者事务、运行态核验、失败恢复和可选关闭连接。
4. [M4：提供模式 CLI 与 TUI](../07-21-m4-mode-surfaces/implement.md)
   - 增加稳定 CLI 输出与错误。
   - 将 TUI 从 legacy 直连 client 迁移到 daemon app 能力，并增加运行模式页面。
5. [M5：提供实时链路与入口诊断](../07-21-m5-route-observability/implement.md)
   - 增加 connections snapshot/follow typed stream。
   - 增加只读系统代理入口检测、warnings 和 TUI 实时视图。

## 父验收映射

| 父验收 | 主要负责 milestone |
|---|---|
| AC1 三模式 CLI/TUI 与运行态 | M3、M4 |
| AC2 Global/Direct 真实链路 | M3、M5 |
| AC3 Rule 大陆/局域网分流 | M2、M5 |
| AC4 custom/白名单/唯一兜底 | M2 |
| AC5 DNS 分流与事务回滚 | M2、M3 |
| AC6 固定规则集安装与缓存失败策略 | M1 |
| AC7 24 小时更新与安装状态 | M1、M2 |
| AC8 订阅更新保留与节点回退 | M2 |
| AC9 模式事务失败恢复 | M3 |
| AC10 活动连接默认保留/显式关闭 | M3 |
| AC11 实时连接与 NDJSON | M5 |
| AC12 系统代理入口警告 | M5 |
| AC13 daemon 安装启用与降级 | M1 |
| AC14 全量质量门 | M1-M5，父集成验收 |

## 跨 Milestone 质量门

- [ ] 每个 child PRD 的验收标准映射到父 PRD AC1-AC14，且没有未解决开放问题。
- [ ] 新路径、环境变量和安装状态字段同步更新 `internal/config`、安装/卸载脚本、部署 spec 与根 `AGENTS.md`。
- [ ] CLI/TUI/IPC/runtime 使用同一 typed DTO；原始 mihomo JSON 只在 adapter 边界解码。
- [ ] 所有配置/资产变更使用临时文件、校验、原子替换和失败恢复；测试不访问真实 HOME/core/socket/systemd。
- [ ] 最终执行 `go test ./...`、`GOTOOLCHAIN=go1.22.12 go test -race ./...`、`go vet ./...`、Shell 安装测试/语法检查和 Linux amd64/arm64 无 CGO 构建。
- [ ] 完成端到端隔离验收：安装 -> daemon -> 新建配置注册/已有配置提示 -> 三模式 -> 订阅更新 -> 活动连接链路 -> 卸载保留。

## 全局停止条件

- 固定规则资产无法从官方不可变引用取得或无法建立可信 SHA-256：停止 M1。
- v1.19.28 无法验证计划中的 `.mrs` provider/DNS 字段：停止 M2，先调整配置契约，不回退完整 Geo 数据而不告知用户。
- 运行态更新失败后无法恢复文件和原 mode：停止 M3，不发布模式写命令。
- TUI 仍存在绕过 daemon 的写路径：停止 M4，不声称单写者迁移完成。
- `/connections` 无法在 1 MiB/取消契约内稳定流式读取：停止 M5，保留快照查询并重新设计 follow。
