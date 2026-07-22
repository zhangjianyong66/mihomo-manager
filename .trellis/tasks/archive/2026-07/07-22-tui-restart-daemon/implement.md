# 实施计划

## 1. systemd 精确重启能力

- [x] 扩展 `internal/platform/systemd` 的 status result，分别报告 `mm.service` 与 `mm.socket` active。
- [x] 增加只执行 `systemctl --user restart mm.service` 的 `Restart`，保留 socket，不修改 unit 文件。
- [x] 更新 adapter 接口与 fake runner 测试，覆盖不可用、inactive、失败和调用范围。

## 2. 应用层组合重启事务

- [x] 在 `internal/app` 定义 phase/progress/result 与窄化 Core/systemd ports。
- [x] 为 `DaemonService` 实现预检、六状态矩阵、Core stop 复核、daemon PID/startedAt 握手、Core 恢复和最终验证。
- [x] 增加集中且可注入的 ready/overall/recovery timeout 与 polling，恢复使用独立有界 context。
- [x] 所有失败附带 failure phase 和可用 partial result；恢复成功仍返回原失败。
- [x] 使用 fake client/controller/clock 覆盖成功、并发变化、PID 未变化、协议错误、超时及每个补偿分支。

## 3. CLI 入口

- [x] 注册 `mm daemon restart [--output table|json]`，复用 `DaemonService.Restart`。
- [x] 增加 `DaemonRestart` table/json presenter，并让错误 details 保留 partial result。
- [x] 覆盖 help、参数、成功输出、冲突/daemon unavailable 退出码和 writer 分离。

## 4. TUI 交互

- [x] 把服务菜单旧 `重启` 改为 `重启 Core`，新增 `重启全部`。
- [x] 注入 app restarter，所有调用留在 `tea.Cmd`；增加确认、started/progress/done 消息和页面状态。
- [x] 确认页 Enter 执行、Esc 零副作用返回；执行阶段屏蔽 Esc/q/Ctrl+C，完成后恢复。
- [x] 展示阶段进度、不可取消状态及旧/新 daemon PID、Core 终态和恢复结果。
- [x] 覆盖菜单、确认、按键、阶段顺序、错误/恢复结果和窄终端渲染。

## 5. 文档与稳定约定

- [x] 更新 `.trellis/spec/backend/{cli-contract,daemon-ipc,deployment}.md`，记录 restart 命令、客户端编排和安装器策略边界。
- [x] 更新 README 的 daemon/TUI 命令说明和升级后手动生效方式。
- [x] 更新根 `AGENTS.md`，记录显式组合重启、systemd-only、状态保持和安装器不自动中断 running Core 的稳定约定。

## 6. 验证与回滚检查

- [x] `gofmt` 与 `git diff --check`。
- [x] `go test ./...`。
- [x] `GOTOOLCHAIN=go1.22.12 go test -race ./...`。
- [x] `go vet ./...`。
- [x] Linux amd64/arm64 `GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0` 构建到临时目录。
- [x] `go run ./cmd/mm daemon restart --help`、根命令和 TUI help 冒烟。
- [x] 确认测试没有访问真实用户 socket、systemd、配置或 Core。
- [x] 回滚审查：无 schema、unit 模板或持久数据变化，删除新增入口后旧 core-only 行为仍可恢复。

## 风险点

- Core stop 成功但 systemd restart 失败是主要中断窗口，必须先完成恢复分支测试再接 TUI。
- 旧 daemon status 与 restart 之间存在并发状态变化，必须二次读取 Core 状态，不能只依赖初始快照。
- socket activation 期间会短暂 dial 失败，轮询只接受协议握手成功且 PID/startedAt 已变化，不能把旧 daemon 误判为新实例。
- JSON 错误只能输出稳定 result/details，禁止把原始 systemctl、socket 路径或底层错误直接暴露。
