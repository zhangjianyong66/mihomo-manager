# 实施计划：daemon、Unix socket 与 systemd user service

## 1. 前置门与工作方式

- [x] 加载 `trellis-before-dev`，阅读 backend 目录、代码风格、CLI、storage、deployment 和跨层思考规范。
- [x] 记录 `go test ./...`、race、vet、无 CGO amd64/arm64 构建基线；确认 A1/A2 已归档。
- [x] 不修改 `internal/mihomo.Client`、真实 `~/.config/mihomo`、旧 Shell CLI 或安装器默认行为；A3 只增加可复用 daemon/unit 能力。

停止门：如果 Linux peer credential 无法在目标架构可靠校验、socket 权限无法收紧，或 systemd activation 无法区分 daemon 与 core，则停止实现并回到设计，不以放宽权限或直接控制 core 绕过。

## 2. 实施顺序

### 2.1 Manager 路径与平台基础

- [x] 在 `internal/config` 增加 manager data/state/runtime/socket/database/unit 路径模型，支持 XDG 默认值和显式测试注入。
- [x] 在 `internal/platform` 增加安全目录、`O_NOFOLLOW`、文件 mode、flock、Unix listener 和 Linux peer credential 接口；非 Linux 明确返回不支持。
- [x] 为路径符号链接、权限收紧、错误目标和锁竞争建立临时目录测试。

质量门：`go test ./internal/config ./internal/platform`；不得读取真实 HOME。

### 2.2 IPC 协议与 transport

- [x] 新建 `internal/ipc`：协议版本常量、请求/响应 envelope、结构化错误、JSON body 限制、HTTP over Unix client/server transport。
- [x] 实现版本范围协商、`MM-Request-ID` 回显、客户端 context 取消和 daemon unavailable/协议不兼容错误映射。
- [x] 实现 NDJSON event decoder/stream helper，验证单调 `seq`、done/error 终止、最大行和断开取消。
- [x] 使用 `httptest` 风格的自定义 Unix listener/fake handler 编写 round-trip、错误、超时和流式测试。

质量门：`go test ./internal/ipc`；JSON/NDJSON 契约不依赖 CLI presenter 或 mihomo DTO。

### 2.3 Daemon lifecycle 与状态 API

- [x] 新建 `internal/daemon`：Run/Shutdown、状态机、store.Open 装配、健康/status handler 和依赖注入。
- [x] 加入 systemd socket activation fd 3 检测与 Unix socket listener 验证；普通前台模式安全创建/清理 socket。
- [x] 实现 operation coordinator、request ID cache（TTL/容量/body hash）和请求级 graceful cancellation。
- [x] 实现 `mm daemon run`，前台输出仅用于诊断，不能把协议 JSON 混入 stdout；退出时清理资源。
- [x] 用临时 store/fake clock/fake handler 覆盖多 client、并发 mutation、重复请求、启动失败清理和 SIGTERM。

质量门：`go test ./internal/daemon -race`；临时 daemon 可被两个 client 访问且不启动 mihomo。

### 2.4 CLI daemon 命令

- [x] 在 `internal/app` 增加 `DaemonService`、`DaemonRunner` 和 status/control DTO；具体 facade 组合 IPC client 与 systemd controller，CLI 不知道 transport 或 `systemctl` 细节。
- [x] 注册 `mm daemon run|status|start|stop|enable|disable`，每个实际命令定义 table/json 结果、stderr 错误和 A1 退出码；不注册未来业务空壳。
- [x] `status` 优先探测 IPC 并报告协议/schema；systemd 诊断作为 details；`start/stop/enable/disable` 通过 platform/systemctl client 注入测试。
- [x] 覆盖未知 daemon、协议不兼容、systemd 不可用、重复 enable/disable、stdout/stderr 分离和秘密脱敏。

质量门：`go test ./internal/cli ./internal/app`、`mm daemon --help` 和各命令隔离 HOME 冒烟。

### 2.5 systemd user unit 与控制器

- [x] 在 `internal/platform/systemd` 嵌入 `mm.socket`/`mm.service` 模板，定义 owner marker/checksum 和原文件备份策略。
- [x] 实现原子写入、daemon-reload、enable --now socket、disable --now socket/service、start/stop/status 命令包装；禁止隐式 sudo/linger。
- [x] 单元模板测试断言 `%t` socket、`0600/0700`、`Accept=no`、Restart 退避、NoNewPrivileges、无 core ExecStart。
- [x] 隔离 HOME 和 fake systemctl 覆盖成功、非零、恢复原文件、未知 unit 文件不删除和 systemd 不可用前台指引。

质量门：`go test ./internal/platform/systemd`；若宿主有 `systemd-analyze`，执行 user unit verify，否则保留结构断言。

### 2.6 跨层集成与最终验证

- [x] 将 `cmd/mm` 装配为真实 daemon/IPC/systemd 依赖，保持现有无参数 `mm`/`mm tui` TUI 行为不变。
- [x] 运行临时 HOME 端到端：`daemon run`、两个 status client、协议错误、Ctrl-C 清理、unit enable/disable fake 流程。
- [x] 更新 backend directory/development/deployment/CLI 规范、README、根 `AGENTS.md` 和父任务 A3 清单，只记录已实现路径与命令。
- [x] 执行全量质量门并审查 diff 中不存在真实用户路径、订阅 URL、节点 URI、凭据或生成的 socket。

## 3. 验证命令

```bash
test -z "$(gofmt -l cmd internal)"
GOTOOLCHAIN=go1.22.12 go test ./...
GOTOOLCHAIN=go1.22.12 go test -race ./...
GOTOOLCHAIN=go1.22.12 go vet ./...
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -o /tmp/mm-a3-amd64 ./cmd/mm
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -o /tmp/mm-a3-arm64 ./cmd/mm
git diff --check
```

快速迭代：

```bash
go test ./internal/ipc ./internal/daemon ./internal/platform/...
go test ./internal/daemon -run 'Protocol|Peer|Request|Shutdown|Socket'
go test ./internal/platform/systemd -run 'Unit|Backup|Rollback|Unavailable'
```

## 4. 风险、回滚与完成门

- Unix peer credential 是最高风险边界；Linux 实现和错误 UID 测试未通过时不进入 systemd 或 CLI 接线。
- systemd socket activation 与前台 bind 必须共享同一 daemon Run 路径；若 fd 识别失败，拒绝启动，不退回 TCP 或宽权限 socket。
- unit 文件写入失败必须恢复原文件；daemon 启动失败必须按逆序关闭 store/listener/lock，不能留下可疑 socket。
- A3 代码可整体回滚而不改变 A1 CLI 契约和 A2 schema；新 manager 数据目录不被旧 1.x 读取。

完成门：

- [x] `trellis-check` 通过，所有高优先级发现已修复。
- [x] `trellis-update-spec` 记录 daemon/IPC 路径、peer UID、systemd 和测试隔离约定。
- [x] 父任务仅勾选 A3 已交付项，未提前声称 A4/A5/A8 能力完成。
- [x] 使用中文 Conventional Commit，例如 `feat(daemon): 建立 Unix socket daemon 与 systemd 用户服务`。
- [x] 执行 `trellis-finish-work`，归档 A3 并记录 journal。

## 5. 启动前审阅清单

- [x] PRD 已完成收敛，无重复事实或未决范围问题。
- [x] 设计定义了路径、协议、错误、权限、生命周期、幂等和 systemd 回滚。
- [x] 实施顺序先完成平台安全与 transport，再接 daemon、CLI 和 unit 管理。
- [x] 用户审阅并明确批准本任务工件。
- [x] 批准后运行 `task.py start 07-20-daemon-ipc-systemd`，再进入编码。
