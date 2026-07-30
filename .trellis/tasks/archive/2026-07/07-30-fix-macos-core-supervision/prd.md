# 修复 macOS core 监控冲突与异常状态同步

## Goal

让目标 Mac 上的 mihomo 只由新版 `mm daemon` 托管，避免旧监控任务反复杀掉 core；同时让 daemon 能准确反映 core 在就绪后的异常退出，避免 CLI/TUI 继续使用失效 runtime。

## Background

- 目标机 `192.168.1.2` 已安装 `~/.local/bin/mm`、mihomo `v1.19.28` 和新版 LaunchAgent。
- 旧 `com.mihomo.monitor` 每 300 秒检查固定端口 `7890`，异常时执行 `pkill -f "mihomo"`，而新版配置使用 `10808`，因此会杀掉新版受管 core。
- daemon Supervisor 在 core ready 后没有继续消费 `Process.Done()`；core 退出后仍返回 `running` 和旧 PID。
- 配置通过 mihomo 原生 `-t -d -f` 校验，问题不是 YAML 配置语法错误。

## Requirements

1. 远程目标机停用旧 `com.mihomo.monitor` LaunchAgent，保留配置、core、日志和新版 manager LaunchAgent。
2. 远程恢复新版 manager 管理的 core，并确认 daemon、core、controller `9090`、mixed `10808` 和 socks `7891` 可用；确认 `mm mode/group/node` 基础查询不再因失效 core 失败。
3. `internal/daemon` 在 core 进入 running 后异步监听其 `Process.Done()`；异常退出时将状态更新为 `failed`、错误码 `PROCESS_EXITED`，清除受管 process/spec，且不影响显式 Stop 的正常状态转换。
4. 增加回归测试覆盖 ready 后异常退出，以及 Stop 与退出监听并发时不误报/覆盖 stopped 状态。
5. 不恢复旧 `pgrep/pkill` 机制，不修改真实用户配置内容，不自动停止无关 xray 或其他 mihomo 进程。

## Acceptance Criteria

- [x] 目标机 `launchctl print gui/<uid>/com.mihomo.monitor` 不再显示运行态，旧监控不再在后续 5 分钟周期启动或杀 core。
- [x] 目标机 `~/.local/bin/mm daemon status` 显示 daemon running，core 为 running 且 PID 存活；9090、10808、7891 按配置监听。
- [x] 目标机 `mm mode status`、`mm group list`、`mm node list` 至少返回成功或明确业务结果，不再返回“mihomo 或 legacy 操作失败”。
- [x] Supervisor 测试证明 ready 后进程退出最终得到 `CoreStateFailed`/`PROCESS_EXITED`，并且 `Current()` 不再报告受管实例可用。
- [x] `gofmt -l` 无输出，`go test ./...` 通过；相关 daemon 测试不触碰真实用户配置。
