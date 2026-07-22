# 调整 TUI 重启以更新 daemon

## Goal

让用户在安装新 `mm` 二进制后，通过 TUI 显式、安全地重启 mihomo-manager daemon 与 mihomo core，使新 daemon 能力立即生效，同时保留只重启 Core 的日常操作。

## Background

- 当前 TUI“服务管理 > 重启”只调用 `CapabilityAPI.CoreAction(..., "restart")`，不会重启 mihomo-manager daemon，菜单和反馈容易让用户误判。
- 2026-07-22 的实际故障中，新 TUI 已包含 `/v1/nodes/test-single`，但 daemon 仍运行被替换的旧二进制，导致单节点测速显示“daemon 不可用或协议不兼容”。
- `mm.service` 同时持有 manager daemon 与其启动的 mihomo 子进程；daemon 重启后 CoreManager 初始状态为 `stopped`，不能假设 core 会自动恢复。
- TUI/CLI 客户端独立于 `mm.service`，可以在进程外编排 Core 停止、service 重启、socket 握手和 Core 恢复；旧 daemon 不应增加自重启 IPC。
- 安装器在无人值守升级时继续遵守“Core running 则不自动重启 daemon”；本任务只增加用户明确确认后的手动组合重启。

## Requirements

- R1：TUI“服务管理”必须分别提供 `重启 Core` 和 `重启全部`；前者保留 core-only 行为，后者组合重启 daemon 与 Core，不再使用含义不明的 `重启` 标签。
- R2：组合重启逻辑必须位于 `internal/app`，由仍存活的 TUI/CLI 客户端调用本地 systemd controller 和 daemon capability；不得要求 daemon 重启自己。
- R3：任何状态变更前必须确认 systemd user 可用、受管 `mm.service` 正在运行且 `mm.socket` 可控。前台 `mm daemon run`、systemd 不可用或非受管状态必须安全拒绝，不通过 PID 扫描、信号或进程名接管。
- R4：TUI 必须在执行前显示二次确认，说明代理会短暂中断，测速 history、实时连接等 Core 内存态会丢失；Enter 确认，Esc 取消且零副作用。
- R5：Core 状态矩阵固定为：`running` 重启后恢复 running；`stopped` 保持 stopped；`starting`/`stopping` 以冲突拒绝且零副作用；`degraded`/`failed` 允许重启，并在新 daemon 就绪后尝试一次干净启动。
- R6：需要停止 Core 时必须通过现有 daemon capability 停止并复核 `stopped`，随后只重启 `mm.service`、保持 `mm.socket` 在线；新 daemon PID/启动时间必须变化且协议握手成功后，才可恢复 Core 或报告成功。
- R7：组合重启中途失败时必须执行有界的尽力恢复：确保 `mm.socket` 可启动、等待可兼容 daemon，并按 R5 的目标状态恢复 Core。不得恢复已被替换的旧 daemon 二进制。
- R8：恢复成功仍必须报告原失败阶段和“服务已恢复”，不得伪装为完整成功；恢复超时、协议不兼容或 Core 恢复失败时显示当前终态和可执行的手动恢复命令。
- R9：TUI 执行阶段使用固定进度页，至少区分预检、停止 Core、重启 daemon、等待握手、恢复 Core、最终验证；操作开始后 Esc、q、Ctrl+C 不得中途取消，整体与恢复流程都必须有界。外部强制终止进程不在可保证范围内。
- R10：新增 `mm daemon restart [--output table|json]`，与 TUI 共用同一编排器和结果模型；CLI 命令本身已是显式操作，不要求交互确认。
- R11：不得绕过 daemon capability 直接控制 mihomo，不得停止或修改其他代理进程，不新增 SQLite schema、运行路径、daemon TCP listener 或 root/system service。

## Acceptance Criteria

- [x] AC1：TUI 菜单和结果文案明确区分 `重启 Core` 与 `重启全部`；执行前者不会改变 daemon PID。
- [x] AC2：未确认或 systemd 预检失败时，不执行任何 Core/daemon 启停调用，TUI 返回服务管理页并显示原因。
- [x] AC3：组合重启成功后 daemon PID 与启动时间发生变化，协议握手成功，TUI 保持运行并连接新 daemon。
- [x] AC4：Core 原本 running/stopped 时，成功后的状态分别为 running/stopped；degraded/failed 会尝试干净启动；starting/stopping 零副作用返回冲突。
- [x] AC5：TUI 按序展示适用的阶段进度，不适用的停止/恢复阶段可跳过；运行期间 Esc、q、Ctrl+C 不退出，超时后不会永久 busy。
- [x] AC6：daemon 重启、握手或 Core 恢复失败均返回具体失败阶段；自动恢复成功与失败分别显示准确终态，不误报完整成功。
- [x] AC7：完成页展示旧/新 daemon PID、原/最终 Core 状态、daemon 是否更换以及是否执行/完成恢复。
- [x] AC8：`mm daemon restart` 的 table/json 成功输出包含相同结果模型；失败遵守既有错误 envelope、退出码，并在 details 中保留可用的部分结果。
- [x] AC9：六种 Core 状态、并发状态复核、PID 未变化、协议不兼容、systemd 各阶段失败和恢复分支都有确定性测试。
- [x] AC10：自动化测试只使用 fake controller/client、临时 Unix socket 或 fake Core adapter，不连接或修改真实用户配置、真实 systemd、真实 daemon 或真实 mihomo core。
- [x] AC11：README、TUI help、CLI 契约、daemon/systemd 规范和根 `AGENTS.md` 与最终行为一致；安装器的无人值守升级策略保持不变。

## Out Of Scope

- 不自动更新或下载 `mm`、mihomo core、订阅或 systemd unit。
- 不自动检测旧 daemon 后弹窗或隐式重启；组合重启只能由用户明确触发。
- 不承诺保留 mihomo runtime history、实时连接等仅驻留 Core 内存的状态。
- 不支持从 TUI 接管前台 daemon、systemd system service、root daemon 或其他用户进程。
