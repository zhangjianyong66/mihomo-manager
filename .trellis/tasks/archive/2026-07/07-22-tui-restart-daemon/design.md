# TUI 与 CLI 组合重启设计

## 设计目标

- 用一个应用层事务同时服务 TUI `重启全部` 和 `mm daemon restart`。
- 由 daemon 外部的客户端进程执行 systemd 操作，避免 daemon 通过 IPC 重启自己。
- 在重启 manager daemon 前先把受管 Core 收敛到 `stopped`，新 daemon 就绪后再按状态矩阵恢复。
- 对每个失败阶段保留部分结果并执行有界恢复，避免 TUI 只显示笼统的“daemon 不可用”。

## 边界与所有权

```text
TUI 确认页 / CLI daemon restart
  -> app.DaemonService.Restart(progress callback)
     -> app DaemonStatusClient              GET /v1/status
     -> app DaemonCoreController            /v1/core/status|stop|start
     -> platform/systemd.Controller          status/restart/start
  -> typed DaemonRestartResult / app.Error
```

- `internal/app` 拥有状态矩阵、步骤顺序、超时、补偿和结果 DTO。
- `internal/platform/systemd` 只负责精确的 `mm.service`/`mm.socket` systemctl 调用和可控性探测，不读取 Core 状态。
- `internal/tui` 只把确认、progress callback 和最终结果转换为 Bubble Tea 消息；`Update`/`View` 不直接执行 IPC 或 systemctl。
- `internal/cli` 只做命令、table/json 投影和错误呈现。
- `internal/daemon`、`internal/mihomo`、SQLite 和 IPC 路由不增加新能力。

## 应用契约

新增稳定的应用类型：

```go
type DaemonRestartPhase string

type DaemonRestartProgress struct {
    Phase   DaemonRestartPhase
    Step    int
    Total   int
    Message string
}

type DaemonRestartResult struct {
    PreviousDaemonPID int
    DaemonPID         int
    PreviousStartedAt time.Time
    StartedAt         time.Time
    PreviousCoreState domain.CoreState
    CoreState         domain.CoreState
    DaemonRestarted   bool
    CoreRestored      bool
    RecoveryAttempted bool
    RecoverySucceeded bool
    FailurePhase      DaemonRestartPhase
}
```

阶段枚举集中定义为 `preflight`、`stop_core`、`restart_daemon`、`wait_daemon`、`restore_core`、`verify`、`recover`。显示层只映射枚举，不根据错误文本推断阶段。

`DaemonService` 增加窄化的 `DaemonCoreController` 依赖以及同步 `Restart(ctx, progress)` 用例。TUI 在 `tea.Cmd` 内运行该用例并把 callback 写入有界 channel；CLI 直接同步调用。真实装配让 `DaemonService` 与业务 capability 各自使用同一路径的 IPC client，不共享长连接；现有 IPC client 已禁用 keep-alive，daemon 切换后会重新 dial Unix socket。

## systemd 控制

`systemd.Result` 增加 additive 的 `ServiceActive` 与 `SocketActive`，`Status` 分别探测 `mm.service` 和 `mm.socket`。`DaemonController`/`systemd.Controller` 增加 `Restart`：

```text
systemctl --user restart mm.service
```

重启时不停止 `mm.socket`。socket 继续持有监听端点，可在 service 退出期间排队连接并激活新 daemon，也避免删除/重建 socket 路径造成额外竞态。恢复路径可以幂等执行现有 `Start`，确保 `mm.socket` 已启动。所有 systemctl 调用继续只作用于两个固定 manager unit，不调用 sudo、linger、PID 信号或进程扫描。

## 状态机与顺序

预检阶段在任何副作用前完成：

1. 读取旧 daemon status，保存 PID、startedAt、Core state/profile。
2. 读取 systemd status，要求 user session 可用、`mm.service` active、`mm.socket` active。
3. `starting`/`stopping` 返回 `DAEMON_RESTART_CORE_BUSY` 冲突。
4. 计算目标 Core 状态：running/degraded/failed -> running；stopped -> stopped。

执行阶段：

1. 目标为 running 且旧 Core 非 stopped 时，经旧 daemon 执行 Core stop。
2. 再读一次 Core status，必须为 stopped；并发变化或停止失败时不重启 service。
3. 执行 `systemctl --user restart mm.service`，保持 socket active。
4. 轮询 `/v1/status`，直到协议握手成功，且 PID 或 startedAt 相比旧 daemon 已变化；同 PID/时间不得误判成功。
5. 目标为 running 时通过新 daemon 启动旧 profile，再读取 status 验证 running。
6. 读取最终 daemon/Core status，形成 typed result。

默认 daemon ready 窗口 10 秒、轮询间隔 200ms、整个前向事务 40 秒；恢复使用独立 15 秒 context。时长和 clock/sleep 通过内部 options 注入测试，生产只使用集中定义的默认值。

## 失败与恢复

| 失败位置 | 副作用 | 处理 |
|---|---|---|
| systemd/Core 状态预检 | 无 | 直接返回，零恢复调用 |
| Core stop 或复核 | Core 可能仍为旧状态 | 不重启 daemon；报告 stop_core |
| systemd restart | Core 已 stopped | 启动 socket、轮询可用 daemon、按目标恢复 Core |
| 新 daemon 握手/PID 验证 | Core 已 stopped | 同上；协议不兼容时停止 IPC 恢复并给出手工命令 |
| Core start/最终验证 | 新 daemon 已运行 | 保留 daemon，报告 Core 终态和手工 `mm core start` |

恢复使用 `context.WithoutCancel` 派生的独立有界 context，保证调用方取消不会跳过补偿。若原失败后恢复成功，方法仍返回携带 `FailurePhase` 和 partial result 的错误；TUI/CLI 明确报告“组合重启异常，服务已恢复”。不能确认 daemon 协议时不通过旧客户端强行启动 Core。

## TUI 交互

服务管理菜单顺序为：`状态`、`启动`、`停止`、`重启 Core`、`重启全部`、`热重载`、`配置测试`、`查看实时日志`、`返回`。

- `重启 Core` 复用当前 `CoreAction(restart)`，成功文案改为“Core 已重启”。
- `重启全部` 进入确认页；Enter 发起事务，Esc 返回服务管理。
- progress 使用 started/event/done 三类 Bubble Tea 消息，页面显示固定阶段、当前进度与“操作进行中，不可取消”。
- 事务 active 时 Esc、q、Ctrl+C 都被状态机消费；完成后进入结果页并恢复正常按键。
- 确认页和进度页使用稳定响应式宽度，窄终端中长文案换行，不改变既有主菜单布局。

## CLI 与错误契约

`mm daemon restart [--output table|json]` 成功 kind 为 `DaemonRestart`。table 展示旧/新 daemon PID、原/最终 Core 状态和恢复结论；JSON 使用同一个 `DaemonRestartResult`。

失败使用现有类别：过渡态为 conflict/退出码 4；daemon/systemd 不可用或握手超时为 daemon_unavailable/退出码 5；Core 启停按原 capability 错误分类。只要已有 partial result，就放入 `app.Error.Details["restart"]`，保证 JSON 错误可机器判断；底层 systemctl 或 IPC 原始错误不直接泄漏到输出。

## 兼容与回滚

- 不改变 protocol version、daemon HTTP 路由、SQLite schema、unit 模板或安装器升级决策。
- 新客户端连接缺少单节点接口但仍兼容 protocol v1 的旧 daemon，可以完成 stop；service 重启后由当前安装路径加载新二进制。
- 当前 TUI 若与新 daemon 协议无交集，握手失败并显示退出 TUI 后执行的手工恢复命令，不回退直连 mihomo。
- 代码回滚只需移除 app 编排器、systemd Restart、CLI 命令和 TUI 页面；没有数据迁移或持久状态需要清理。

## 测试策略

- `internal/platform/systemd`：service/socket 独立状态、Restart 只触及 `mm.service`、systemd unavailable、调用失败。
- `internal/app`：六状态矩阵、二次 Core 复核、PID/startedAt 变化、握手重试/超时、每个失败阶段与恢复成功/失败；使用 fake controller/client/clock，不访问真实 socket/systemd/core。
- `internal/cli`：help、table/json、退出码、partial error details、stdout/stderr 分离。
- `internal/tui`：菜单拆分、确认零副作用、阶段消息、按键锁定、结果投影、窄终端。
- 全量门禁按 daemon/systemd 变更执行 race、vet 和 Linux amd64/arm64 无 CGO 构建；任何集成冒烟使用临时 XDG 与 fake core。
