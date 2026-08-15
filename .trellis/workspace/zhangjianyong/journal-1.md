# Journal - zhangjianyong (Part 1)

> AI development session journal
> Started: 2026-07-14

---



## Session 1: 补全项目基础规范

**Date**: 2026-07-14
**Task**: 补全项目基础规范
**Branch**: `master`

### Summary

基于现有 Go、Shell、安装脚本和项目文档补全 backend Trellis 规范，记录目录结构、运行测试命令、代码风格、部署方式与重要约定，并完成质量验证。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `b80f314` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: 完成 Ubuntu Debian 一键安装流程

**Date**: 2026-07-14
**Task**: 完成 Ubuntu Debian 一键安装流程
**Branch**: `master`

### Summary

实现远程一行安装、本地统一安装器、依赖与隔离 Go 准备、mihomo core 校验安装、独立 mm、PATH/配置保护、安全卸载和 13 项隔离测试，并同步 README、AGENTS.md 与 Trellis 安装部署规范。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `a3b1e19` | (see git log) |
| `a04ddd5` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: 修复 mihomo 重复节点配置

**Date**: 2026-07-14
**Task**: 修复 mihomo 重复节点配置
**Branch**: `master`

### Summary

分析并修复 config.yaml 中 10 组不同参数但同名的 VLESS 节点，同步代理组引用，保留 256 个节点，创建时间戳备份，权限收紧为 600，并通过 mihomo 配置校验；同时记录当前 xray 占用 10808、mihomo 未运行的本机状态。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `312c17d` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: 完成 A1 CLI 与应用服务契约

**Date**: 2026-07-20
**Task**: 完成 A1 CLI 与应用服务契约
**Branch**: `master`

### Summary

建立领域 ID 与 core 状态、按能力拆分的应用 ports、mm/v1 table/json 输出、结构化错误与 1-8 退出码、显式秘密脱敏、可注入 Cobra 命令工厂和 mm tui 兼容入口；同步 README、AGENTS、能力矩阵与 backend CLI 契约规范，并通过全量测试、race、vet 和无 CGO 构建。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `148117d` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: 完成 A2 领域模型与 SQLite 存储

**Date**: 2026-07-20
**Task**: 完成 A2 领域模型与 SQLite 存储
**Branch**: `master`

### Summary

完成 internal/domain 聚合校验、无 CGO SQLite 迁移与权限底座、profile/subscription/node/operation/settings 仓储和事务回滚测试；固定 modernc.org/sqlite v1.36.1，完成双架构构建、race、vet、许可证与体积门禁，并更新存储规范、README、AGENTS 与父任务清单。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `9ecb7f9` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 6: 完成 A3 daemon、Unix IPC 与 systemd 用户服务

**Date**: 2026-07-20
**Task**: 完成 A3 daemon、Unix IPC 与 systemd 用户服务
**Branch**: `master`

### Summary

实现 XDG manager 路径、同 UID Unix socket IPC、daemon 生命周期与状态、operation/request 幂等基础、systemd user unit 控制和 daemon CLI；通过 Go 1.22.12 全量测试、race、vet、双架构无 CGO 构建及隔离 XDG 冒烟。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `250ec03` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 7: 完成 A4 mihomo adapter 与 core 生命周期

**Date**: 2026-07-20
**Task**: 完成 A4 mihomo adapter 与 core 生命周期
**Branch**: `master`

### Summary

实现类型化 core 契约、原子 generation、external 只读摘要、mihomo 原生验证与 loopback RuntimeClient、精确进程托管，以及 daemon 档案切换/失败恢复状态机；同步规范并通过 race、vet 和双架构无 CGO 构建。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `9b4ce99` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 8: 完成 A5 legacy 迁移与回滚

**Date**: 2026-07-20
**Task**: 完成 A5 legacy 迁移与回滚
**Branch**: `master`

### Summary

实现 migrate plan/apply/status/rollback、legacy 恢复点与 SQLite v3、daemon IPC、兼容服务和安全回滚；补齐专项测试、文档与规范并通过完整质量门。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `1e40359` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 9: 完成 A6 现有能力 CLI 等价迁移

**Date**: 2026-07-21
**Task**: 完成 A6 现有能力 CLI 等价迁移
**Branch**: `master`

### Summary

完成 legacy core/config/node/group/subscription/route/log CLI、daemon IPC、NDJSON 流式协议、敏感信息脱敏与安全配置编辑；通过全量测试、race、vet、双架构构建并归档 A6 任务。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `9c470e1` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 10: 归档 v2rayN 对齐总体规划

**Date**: 2026-07-21
**Task**: 归档 v2rayN 对齐总体规划
**Branch**: `master`

### Summary

同步父任务规划状态，标记 A6 CLI 迁移完成，明确 A7/A8 及 Beta 至 3.0 为后续独立路线，并归档 07-20-align-v2rayn-cli。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `31a7d96` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 11: 完成 M1 CN 规则集与用户 daemon 安装

**Date**: 2026-07-21
**Task**: 完成 M1 CN 规则集与用户 daemon 安装
**Branch**: `master`

### Summary

实现 CN 规则集事务安装、daemon 安全升级与 fresh migrate，并完成全量质量门和真实安装验收

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `ae2ccff` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 12: 完成 M2 Rule 分流与订阅保留

**Date**: 2026-07-21
**Task**: 完成 M2 Rule 分流与订阅保留
**Branch**: `master`

### Summary

新增统一 RoutingPolicy 与 RoutingMode，生成本机/局域网、白名单、CN MRS providers、唯一兜底和防循环 DNS；订阅更新保留 mode、规则、DNS、providers 与组选择，节点消失返回 warning，失败恢复配置、权限和运行态；完成 v1.19.28 原生验证及全量质量门禁。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `2c3a7e5` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 13: 完成 M3 模式事务与 daemon API

**Date**: 2026-07-21
**Task**: 完成 M3 模式事务与 daemon API
**Branch**: `master`

### Summary

实现 global/rule/direct 模式事务、typed mihomo runtime、失败补偿、共享写协调器和 /v1/mode 幂等 IPC。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `45b4829` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 14: 完成 M4 模式 CLI 与 TUI

**Date**: 2026-07-22
**Task**: 完成 M4 模式 CLI 与 TUI
**Branch**: `master`

### Summary

新增 mode status/set 稳定 table/json 输出与恢复提示；将 TUI 全面迁移到 daemon capability，增加三模式页面、连接关闭选项和可取消流；补充编辑器桥接、回归测试与项目规范。全量 test/race/vet 及 amd64/arm64 无 CGO 构建通过。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `6761798` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 15: 完成 M5 实时链路与入口诊断

**Date**: 2026-07-22
**Task**: 完成 M5 实时链路与入口诊断
**Branch**: `master`

### Summary

实现连接快照/follow、TUI 实时页和 GNOME/env 入口诊断，并通过全量质量门。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `4a5680e` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 16: 完成端口冲突检测与监听端口管理

**Date**: 2026-07-22
**Task**: 完成端口冲突检测与监听端口管理
**Branch**: `master`

### Summary

将默认 mixed 端口改为 7890，新增六类端口启动预检、结构化 daemon/CLI/TUI 管理和安全重配置恢复；补齐跨层测试与规范，并在真实环境验证 PORT_CONFLICT、7890 恢复及 xray 10808 保持不变。另独立提交 legacy 备份候选去重修复。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `8ddb447` | (see git log) |
| `4ef2007` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 17: 修复 legacy 迁移重复备份

**Date**: 2026-07-22
**Task**: 修复 legacy 迁移重复备份
**Branch**: `master`

### Summary

统一去重 legacy 固定与动态备份候选，补充标准备份和时间戳备份回归测试，完成真实迁移验证并归档任务。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `8ddb447` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 18: 完成三模式路由父任务验收

**Date**: 2026-07-22
**Task**: 完成三模式路由父任务验收
**Branch**: `master`

### Summary

完成 M1-M5 到父 AC1-AC14 的证据映射，补跑全量 test/race/vet、安装器、Shell、双架构与 mihomo 原生验证；真实验证 global/direct/rule、CN domain/IP、MATCH 链路和显式关闭连接，恢复 rule 运行态并确认 xray 与 GNOME 代理未变化。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `2b387e2` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 19: 重设计节点测速交互

**Date**: 2026-07-22
**Task**: 重设计节点测速交互
**Branch**: `master`

### Summary

节点列表改为展示 mihomo 历史结果并由 t/a 显式触发单个或批量测速；新增独立单测 IPC 路由、CLI --node、取消与 generation 隔离，并完成全量测试、race、vet 和双架构构建。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `6f4232b` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 20: TUI 组合重启 daemon 与 Core

**Date**: 2026-07-22
**Task**: TUI 组合重启 daemon 与 Core
**Branch**: `master`

### Summary

新增客户端侧 daemon 组合重启事务、systemd 受管 unit 与身份校验、CLI restart 命令和 TUI 确认/进度/恢复结果；同步规范文档并通过全量 race、vet、双架构构建及 help 冒烟。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `8818142` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 21: 修复 TUI 连续与批量测速按键响应

**Date**: 2026-07-22
**Task**: 修复 TUI 连续与批量测速按键响应
**Branch**: `master`

### Summary

为节点测速增加 idle/single/batch 状态机和有序去重单测队列；支持 t 入队、a 批测抢占、批测中 t 单测抢占、Esc 清队列取消及 generation 迟到消息隔离；补齐长期未完成 stream、异常关闭、失败继续、系统错误停止和窄终端回归测试，并同步 README、AGENTS 与 CLI/TUI 稳定契约。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `8b5f5b5` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 22: 修复 TUI 批量测速初始进度

**Date**: 2026-07-23
**Task**: 修复 TUI 批量测速初始进度
**Branch**: `master`

### Summary

修复节点列表按 a 后首个测速结果返回前显示 0/0 的问题：批测启动时按已加载 Testable 状态立即初始化为 0/N，流事件到达后继续以 daemon 进度为准；新增批测重启、context 取消和进度覆盖回归测试，并同步测速契约。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `ef69f33` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 23: 修复 daemon 测速流缓冲

**Date**: 2026-07-23
**Task**: 修复 daemon 测速流缓冲
**Branch**: `master`

### Summary

让 RequestCache 在首次 Flush 时下沉已缓冲响应并切换直通，避免 POST NDJSON 被整流缓冲；新增真实 HTTP 时序与不缓存回归测试，并同步 daemon IPC 契约。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `1207761` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 24: 实现 GNOME 与 Bash 代理配置

**Date**: 2026-07-23
**Task**: 实现 GNOME 与 Bash 代理配置
**Branch**: `master`

### Summary

新增 GNOME 系统代理与 Bash 环境代理的 daemon capability、CLI/TUI 入口、快照恢复、原子标记区块、NO_PROXY 合并及跨层测试。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `0305d39` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 25: 完成 macOS 一键安装支持

**Date**: 2026-07-29
**Task**: 完成 macOS 一键安装支持
**Branch**: `master`

### Summary

支持 macOS 12+ amd64/arm64 安装、launchd daemon、Darwin IPC/进程控制、安全卸载与跨平台验证。

### Git Commits

| Hash | Message |
|------|---------|
| `63d220b` | (see git log) |

### Status

[OK] **Completed**


## Session 26: 支持 CN 规则集延迟安装

**Date**: 2026-07-30
**Task**: 支持 CN 规则集延迟安装
**Branch**: `master`

### Summary

默认安装跳过 CN 规则集网络下载，新增显式安装开关、ruleset status/install CLI、TUI 配置管理入口、daemon NDJSON 事务、代理优先级与 readiness 门禁；规则 provider 改为本地 file，补齐缓存复用、原子发布/回滚、跨平台安装测试和项目规范。

### Git Commits

| Hash | Message |
|------|---------|
| `9a422a7` | (see git log) |

### Status

[OK] **Completed**


## Session 27: 修复 macOS core 监控冲突与状态同步

**Date**: 2026-07-30
**Task**: 修复 macOS core 监控冲突与状态同步
**Branch**: `master`

### Summary

停用并隔离目标 Mac 的旧 com.mihomo.monitor，恢复并部署新版 mm；Supervisor 增加 running 后进程退出监听与实例隔离，补充并发回归测试和 core 托管规范。

### Git Commits

| Hash | Message |
|------|---------|
| `b33e64c` | (see git log) |

### Status

[OK] **Completed**


## Session 28: 高亮节点列表当前光标行

**Date**: 2026-08-15
**Task**: 高亮节点列表当前光标行
**Branch**: `master`

### Summary

为节点切换列表的当前光标行增加深青蓝整行背景和白色前景，保留已启用节点的 ✅ 标记；补充宽度和光标移动回归测试，并通过全量 Go 测试。

### Git Commits

| Hash | Message |
|------|---------|
| `9df9382` | (see git log) |

### Status

[OK] **Completed**
