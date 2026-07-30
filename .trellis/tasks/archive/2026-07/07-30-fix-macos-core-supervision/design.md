# 技术设计

## 边界

- 代码改动只涉及 `internal/daemon` Supervisor 及其测试；进程句柄仍由 `internal/core.Process` 提供，daemon 不引入扫描、按 PID 杀进程或旧 Shell 监控。
- 远程运维只操作当前用户明确归属的旧 LaunchAgent 和新版 manager CLI；不删除配置、core 或日志。

## 状态与并发

`Supervisor.start` 在 runtime readiness 成功、写入 `s.process/s.spec/status=running` 后启动一个后台 watcher，等待同一 `Process.Done()`。watcher 只在该 process 仍是当前受管实例且状态仍为 running 时写入 failed；它清除 `s.process` 与 `s.spec`，保留 profile/generation 以便诊断。显式 `Stop` 先把状态改为 stopping，并在清理后写 stopped；watcher 通过 process identity 和状态检查避免覆盖 stopped 或新实例。

为避免多个 goroutine 竞态，复用 Supervisor 的 mutex；不在锁内执行阻塞的 `Done()` 等待。退出错误不直接暴露给 JSON，使用既有 `PROCESS_EXITED` 错误码。

## 远程恢复与回滚

先 `launchctl bootout gui/<uid>/com.mihomo.monitor` 并将旧 plist 移到用户可恢复位置（不删除），再用新版 `mm core start`/必要时 `mm daemon restart` 恢复。验证失败时不修改配置；若新版启动失败，仅查看日志并保留旧 plist 备份，避免重新启用冲突监控。

## 兼容性

旧测试 fake process 的 `Done()` 已存在；新增测试使用可控 channel，验证正常 stop 和异常 exit 两条路径。不会改变 core 初始 stopped、显式 start/stop 或 reconfigure 的既有事务语义。
