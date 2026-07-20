# 实施计划：对齐 v2rayN 的终端 CLI

## 1. 执行方式

当前任务作为总体规划父任务，不直接承载全部实现。实施前按下述交付物创建 Trellis 子任务；每个子任务独立补齐 PRD、设计差异、验证和回滚点，并明确依赖。只有当前子任务通过质量门后，才开始下一个依赖任务。

不设置日历期限。发布由可执行验收门槛驱动，不通过迁移、回滚、安全或自动化测试时不得压缩质量换取版本号。

通用规则：

- 不复制 v2rayN GPL-3.0 源码，只依据公开用户能力独立实现。
- 每次只跨越一个主要风险边界；TUN、更新、多内核不得混入 2.0 Alpha 架构迁移。
- 新实现优先包装并测试现有行为，再移动代码；禁止一次性删除 `mihomo.Client` 或重写 TUI。
- 每个子任务结束时更新能力矩阵、README、Trellis spec 和根 `AGENTS.md` 中发生变化的事实。

## 2. 总体任务树

```text
align-v2rayn-cli（当前规划父任务）
├── 2.0 Alpha：架构与现有能力等价迁移
│   ├── A1 CLI/应用服务契约
│   ├── A2 领域模型与 SQLite
│   ├── A3 daemon/IPC/systemd
│   ├── A4 mihomo 适配器与配置生成
│   ├── A5 legacy 迁移与回滚
│   ├── A6 现有能力 CLI 等价迁移
│   ├── A7 TUI 复用应用服务
│   └── A8 安装、许可与 Alpha 发布门
├── 2.0 Beta：托管配置闭环
├── 2.1：运行、TUN 与可观测
├── 2.2：更新、定时任务、备份与同步
├── 2.3：桌面辅助
└── 3.0：Xray、sing-box 多内核
```

规划审阅通过后按依赖创建并启动 A1、A2、A3、A4 独立子任务；A1/A2/A3 已归档，A4 已完成 adapter/generation/core 生命周期质量门，后续 legacy 迁移和业务能力仍不得提前声称完成。

## 3. 2.0.0-alpha.1

### A1：CLI 与应用服务契约

依赖：无。

- [x] 定义 `internal/domain` 最小标识符和通用状态类型，不包含 mihomo DTO。
- [x] 定义应用服务 ports：core 生命周期、档案、节点/组、订阅、路由、配置、日志。
- [x] 建立 `internal/cli` 和 Cobra 命令工厂，保持 `cmd/mm` 薄入口。
- [x] 实现 `--output table|json`、JSON envelope、结构化错误和退出码映射。
- [x] 保持无参数 `mm` 打开 TUI，增加显式 `mm tui`。
- [x] 使用 fake application service 为命令成功、输入错误、秘密脱敏、stdout/stderr 和退出码建立契约测试。

验收：不改现有运行逻辑时，CLI 契约测试稳定；`mm` 和 `mm tui` 行为兼容，`mm --help` 展示规划内已实现命令，不出现空壳命令。

回滚：新命令通过独立工厂接入；失败可移除新命令注册而不改现有 `app.RunInteractive()`。

### A2：领域模型、SQLite 与迁移框架

依赖：A1 的领域/应用边界。

- [x] 对无 CGO SQLite 候选做 amd64/arm64、Go 1.22、`CGO_ENABLED=0`、许可证和二进制体积验证。
- [x] 固定驱动版本，建立 `internal/store`、连接参数、权限检查和 schema 迁移器。
- [x] 实现 profile、subscription、node、operation 和 settings 的最小 schema；后续表通过迁移增加。
- [x] 实现事务仓储、稳定 ID、来源外键、唯一约束和删除边界。
- [x] 建立并发、崩溃恢复、损坏检测、备份快照和 schema 前后兼容测试。
- [x] 所有测试使用 `t.TempDir()`，不得读取真实用户数据库。

验收：订阅来源事务替换、失败回滚、权限 `0700/0600`、重复迁移和旧 schema 升级测试通过；安装构建仍不依赖 C 编译器。

停止条件：候选驱动无法在两种目标架构无 CGO 构建，或许可证与 MIT 分发不兼容时，不继续编写业务 schema，先更换驱动。

### A3：daemon、Unix socket 与 systemd user service

依赖：A1；A2 仅对持久状态部分构成依赖，可先用内存 store 开发协议。

- [x] 实现版本化 HTTP/JSON over Unix socket 和 NDJSON 流。
- [x] 实现 socket 目录/文件权限、peer UID 校验、单实例锁和协议协商。
- [x] 实现 CLI IPC client、daemon unavailable/版本不兼容错误和请求取消。
- [x] 实现 operation lock、request ID 幂等缓存和基础 daemon 状态。
- [x] 增加 systemd user `.socket`/`.service` 模板、安装/禁用命令和前台 `daemon run`。
- [x] socket 激活只启动 manager daemon，不自动启动 proxy core。
- [x] 使用临时 socket、隔离 HOME 和 fake systemctl 验证同 UID、错误 UID、并发启动、断流和 systemd 模板。

验收：多个 CLI 客户端只能连接同一 daemon；daemon 不监听 TCP；无 daemon 时不直接写文件或控制 core；前台模式可用于无 systemd 测试。

回滚：systemd unit/socket 安装前备份同名用户文件；禁用并移除 unit 后，旧版 `mm` 仍能直接运行 TUI。

### A4：mihomo 适配器与配置生成管线

依赖：A1、A2；进程托管集成依赖 A3。

- [x] 定义最小 `core.Adapter`、`RuntimeClient`、`Process` 接口，以 mihomo 当前真实需求驱动。
- [x] 将进程启动/停止、external-controller API、配置验证分离为 mihomo 子组件。
- [x] 保留 `Setsid`、超时、状态码和日志行为的回归测试。
- [x] 实现 managed 配置最小渲染、生成目录、generation 元数据、静态验证和 mihomo 原生验证。
- [x] 实现 external 只读启动和源文件摘要检查。
- [x] 实现档案切换操作记录、就绪探测、失败恢复和明确 failed 状态。
- [x] 使用 golden 配置和受控 mihomo 二进制执行契约测试。

验收：适配器不泄漏 mihomo `map[string]any` 到领域层；配置发布可重复且原子；失败启动恢复旧实例；测试不误杀外部 mihomo/xray 进程。

停止条件：新生成配置无法与现有配置语义等价，或恢复旧实例无法稳定验证时，不迁移用户功能。

### A5：legacy 迁移、兼容层与回滚

依赖：A2、A3、A4。

- [ ] 实现 `migrate plan|apply|status|rollback|convert` 的 Alpha 子集：plan/apply/status/rollback。
- [ ] 发现 `CONFIG_DIR`、旧配置、订阅、白名单、备份和运行状态，不读取秘密到日志。
- [ ] 创建时间戳恢复点和 legacy profile 元数据，不自动转换或重写旧 YAML。
- [ ] 通过兼容 service 包装原 1.x 配置/订阅/白名单行为，所有写入沿用备份和验证规则。
- [ ] 明确 `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT`、`EDITOR` 在 legacy 模式的作用。
- [ ] 建立 Good/Base/Bad 矩阵：空环境、有效旧配置、无效配置、中断迁移、重复迁移、回滚。

验收：迁移失败后旧 mm/core/config 摘要不变；成功后 2.x 可管理 legacy 档案；回滚后 1.x 可继续使用；外部配置不会误分类后改写。

停止条件：无法可靠判断文件归属时必须创建 legacy 或 external 引用，不得猜测转换为 managed。

### A6：现有能力 CLI 等价迁移

依赖：A1-A5。

按纵向能力逐项迁移，每项单独提交、单独测试：

- [ ] `core status|start|stop|restart|reload|logs` 与 `config validate`。
- [ ] `group list|show|select` 和 `node list|test|select`。
- [ ] legacy `subscription show|set|update`，并在命令帮助中标明单来源兼容语义。
- [ ] legacy `route whitelist list|add|edit|remove` 和 CN 路由预设/诊断。
- [ ] legacy 配置 backup/restore/edit。
- [ ] 日志跟随和测速 NDJSON 事件、取消和超时。
- [ ] 为每个查询增加 table/json，为每个修改定义结构化结果和退出码。
- [ ] 对 secret、URI、URL、日志和错误执行统一脱敏测试。

验收：`research/capability-matrix.md` 中标记为当前已有/部分的现有能力，在 CLI 上有等价结果；原回归测试迁移后仍保留失败恢复断言。

### A7：TUI 复用应用服务

依赖：A6。

- [ ] 把 TUI 对 `*mihomo.Client` 的依赖替换为应用服务/IPC client 接口。
- [ ] 按服务、节点/组、订阅、白名单、路由、配置、日志页面逐项切换。
- [ ] 保留测速/日志流取消、分页、过滤和错误展示。
- [ ] 删除 TUI 中残留的业务拼装，不在 View/Update 中读写文件、执行进程或调用原始 HTTP。
- [ ] 拆分 1100 行单模型时保持状态可测试，避免同时重做视觉设计。

验收：无参数 `mm` 的现有主要操作结果等价；TUI fake service 测试不依赖真实 core；CLI 和 TUI 针对同一操作调用相同应用服务。

回滚：各页面逐项切换；未迁移页面在开发分支可通过临时兼容 adapter 工作，发布前不得同时存在两套写逻辑。

### A8：安装、许可、文档与 Alpha 发布门

依赖：A1-A7。

- [ ] 加入 MIT `LICENSE` 和第三方许可证清单。
- [ ] 更新安装状态 schema、systemd user unit/socket 安装、独立 mm 构建和安全卸载。
- [ ] 正常 systemd user 会话中默认安装并 `enable --now mm.socket`，提供 `--no-daemon`；失败时明确降级为手工 `mm daemon run`，不得启动 proxy core、修改系统代理或启用 linger。
- [ ] 实现 1.x mm 备份、2.x 安装失败恢复和显式回退。
- [ ] 更新 README、CHANGELOG、命令参考、迁移/回滚文档、spec 和 AGENTS。
- [ ] 在隔离 HOME 执行安装、首次迁移、CLI/TUI 操作、卸载和回退全流程。
- [ ] 发布 `2.0.0-alpha.1` 前完成安全/许可证/敏感信息 diff 审查。

Alpha 发布门：

```bash
test -z "$(gofmt -l cmd internal)"
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o /tmp/mm ./cmd/mm
/tmp/mm --help
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
```

此外必须在 amd64 和 arm64 CI 产物上验证 `mm --help`、SQLite 打开/迁移和 daemon 前台冒烟。

## 4. 2.0 Beta：托管配置闭环

进入前提：Alpha 已验证迁移和回滚，所有写操作经 daemon。

建议拆分：

- [ ] B1 档案/订阅/手工节点 CRUD、解析器注册表和来源隔离。
- [ ] B2 mihomo managed 配置完整生成、稳定重命名和代理组策略。
- [ ] B3 路由规则 CRUD、预设、优先级、命中诊断和规则资源引用。
- [ ] B4 DNS 策略、预设、Fake-IP/普通 DNS 模式和冲突验证。
- [ ] B5 core 专属覆盖层、`render|diff|validate` 和管理字段保护。
- [ ] B6 legacy/external 到 managed 的预览转换、导入导出和差异报告。

Beta 完成门：用户可以从空状态创建 managed 档案，添加多个订阅/手工节点，配置路由/DNS，生成并运行 mihomo；任一失败不损坏上一活动档案。

## 5. 2.1：运行、TUN 与可观测

进入前提：managed 配置生成稳定。

建议拆分：

- [ ] C1 连接列表/关闭、实时流量和 NDJSON 事件。
- [ ] C2 30 天聚合流量、7 天/50 MiB 日志轮转和 `privacy purge`。
- [ ] C3 延迟策略、真实吞吐测试、健康检查和诊断报告。
- [ ] C4 `setup tun` 变更预览、最小 capability、撤销和 core 更新失效检测。
- [ ] C5 TUN 配置生成、启停、路由恢复和网络中断回滚。

2.1 完成门：无长期 root；TUN 启停不会遗留路由；连接明细不落盘；core/daemon 崩溃后的状态可诊断。

## 6. 2.2：更新、任务、备份与同步

建议拆分：

- [ ] D1 应用/core/规则更新检查、可信 manifest、校验、原子切换和回滚。
- [ ] D2 daemon 调度器、订阅/更新任务、退避和逐项 opt-in 自动安装。
- [ ] D3 版本化逻辑备份、加密、恢复预览、恢复点和冲突停止。
- [ ] D4 传输接口与 WebDAV push/pull/status，凭据隔离和日志脱敏。

2.2 完成门：默认不自动安装；断网、摘要错误、远端冲突或恢复失败都保留本地上一可用状态。

## 7. 2.3：桌面辅助

- [ ] E1 `proxy env` 的 bash/zsh 输出与取消，不修改 shell 配置。
- [ ] E2 GNOME 系统代理读取、备份、启用、关闭和精确恢复。
- [ ] E3 PAC 在 GNOME 适配器稳定后独立实现。
- [ ] E4 终端二维码显示和图片文件解析，秘密展示需显式确认。

2.3 完成门：SSH/无 DISPLAY 不自动修改桌面；GNOME 原设置在异常/卸载时可恢复；不声称支持 KDE。

## 8. 3.0：多内核

### F1：Xray

- [ ] 用 Xray 需求审查并最小扩展 `core.Adapter`，不破坏 mihomo。
- [ ] 实现 Xray 配置生成、验证、进程、日志、延迟和 TUN 能力矩阵。
- [ ] 明确不能跨 core 转换的覆盖层和协议字段。
- [ ] 使用同一 CLI/daemon/domain 接口完成端到端测试。

### F2：sing-box

依赖：F1 证明适配器可支持第二个 core，并完成接口回顾。

- [ ] 实现 sing-box 适配器和能力矩阵。
- [ ] 删除仅为 Xray 临时加入的错误抽象，保持三种 core 的共性接口最小。
- [ ] 验证档案切换、更新、TUN、日志和迁移在三种 core 下的差异报告。

3.0 完成门：mihomo、Xray、sing-box 均能通过同一 CLI 完成安装发现、档案生成、验证、启动、状态、停止和日志；不支持能力明确报告，不伪装成功。

## 9. 跨里程碑质量门

每个子任务必须检查：

- [ ] 需求和设计边界没有被实现便利性悄悄改变。
- [ ] 新命令有 table/json、结构化错误、退出码和脱敏测试。
- [ ] 新 schema 有迁移、回滚/恢复和旧版本兼容说明。
- [ ] 新文件/路径/环境变量由 `internal/config` 管理，并更新部署文档。
- [ ] 配置/下载/更新先临时写、校验，再原子替换。
- [ ] 测试使用临时 HOME/socket/database/core，不触碰真实用户状态。
- [ ] 日志、fixtures、diff 不包含真实订阅 URL、节点或凭据。
- [ ] `go test ./...`、`go vet ./...`、无 CGO 构建和相关 shell 测试通过。

## 10. 全局停止与回滚条件

- SQLite 无法满足目标架构无 CGO 构建：停止 A2，先解决驱动，不回退到临时 YAML 事实源。
- IPC 无法可靠校验同用户：停止 A3，不通过放宽 socket 权限推进。
- 迁移不能证明旧文件不变：停止 Alpha 发布，只允许新安装实验，不升级现有用户。
- 配置生成不能通过真实 core 验证：不切换活动 generation。
- core 恢复/回滚测试失败：不发布影响运行态的版本。
- TUN 撤销不能恢复路由：不发布 TUN，不要求用户长期 root 绕过。
- 更新源缺少可信摘要/签名：拒绝更新，不静默降级校验。
- 许可证来源不清晰：相关代码/资产不进入发布包。
