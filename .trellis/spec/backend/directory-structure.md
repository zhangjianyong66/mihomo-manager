# 目录结构

## 总体布局

```text
.
├── cmd/mm/                 # Cobra 命令入口，只负责启动应用
├── internal/app/           # 应用 ports、错误/DTO、依赖装配和 TUI 启动
├── internal/cli/           # Cobra 工厂、输出、退出码和脱敏
├── internal/config/        # 路径、端口和环境变量配置
├── internal/domain/        # 稳定 ID、档案/订阅/节点/操作/设置领域模型
├── internal/core/          # 跨 core 类型契约、generation 原子发布和 external 摘要
├── internal/store/         # SQLite 仓储、嵌入式迁移、权限和恢复点
├── internal/daemon/        # 用户级 daemon 生命周期、状态、操作锁和幂等缓存
├── internal/ipc/           # 版本化 HTTP/JSON Unix transport 与 NDJSON 编解码
├── internal/platform/      # 文件锁、Unix peer credential、systemd unit 平台适配
├── internal/mihomo/        # mihomo 进程、API、配置和订阅核心逻辑
├── internal/tui/           # Bubble Tea 状态机、输入处理和界面渲染
├── scripts/                # 安装/卸载脚本及遗留 Shell 实现
│   ├── lib/                # 遗留 Shell/Python 功能库
│   └── tests/              # 遗留 Shell CLI 测试
├── launchd/                # macOS 监控服务模板
├── tests/                  # 依赖本机安装与运行状态的旧集成脚本
├── bin/                    # 构建产物和遗留 Shell 入口
├── Makefile                # install/uninstall 包装命令
└── .trellis/               # Trellis 工作流、任务和项目规范
```

## Go 包职责

- `cmd/mm/main.go`：注入真实 TUI runner 和进程 IO，调用 `cli.Execute()` 并执行唯一的 `os.Exit`。不要在这里定义 Cobra 树、输出或业务逻辑。
- `internal/cli`：定义 Cobra 根命令、已实现子命令、table/json presenter、退出码和显式秘密值。不得直接读配置或调用 mihomo。
- `internal/app`：定义按能力拆分的应用 ports、应用 DTO/错误，并提供 Bubble Tea runner 与客户端本地配置编辑桥接。新业务边界先在这里表达。
- `internal/domain`：定义不依赖框架、SQLite 或 mihomo DTO 的稳定标识符、core、档案、订阅、节点、操作和设置类型。
- `internal/store`：接收显式数据库路径，负责 SQLite 打开参数、权限、迁移/恢复点和领域仓储；不读取 HOME/XDG，不由 CLI/TUI 直接调用。
- `internal/core`：定义不依赖 mihomo DTO 的 adapter/process/runtime 契约，并负责 managed generation 和 external 只读引用；不得导入 SQLite、CLI、TUI 或 `internal/mihomo`。
- `internal/daemon`：拥有 daemon 生命周期、状态快照、store 装配、health/status、operation coordinator、CoreManager 和 request ID 缓存；启动 daemon 不自动启动 mihomo，后续显式操作才托管单实例 core。
- `internal/ipc`：只负责 `/v1/` HTTP/JSON over Unix socket、版本范围协商、请求取消、结构化错误和 NDJSON stream；不导入 store/mihomo/CLI。
- `internal/platform`：隔离 Linux `SO_PEERCRED`、`O_NOFOLLOW` 文件锁、Unix listener、socket activation 和 systemd user unit 控制；协议层不得依赖 Linux syscall。
- `internal/config/config.go`：统一生成 `config.Paths`。新增运行路径或环境变量时，应在这里提供默认值并由调用方注入，避免在业务包重复拼接 `$HOME` 路径。
- `internal/mihomo/adapter.go`、`render.go`、`process.go`、`runtime.go`：2.x adapter 的渲染/验证、精确进程句柄和类型化 runtime API；`client.go` 只保留未接入产品入口的 1.x 兼容业务。
- `internal/tui/model.go`：Bubble Tea `Model`、消息类型、按键处理和视图渲染。它只调用注入的 app/daemon capability，不导入 `internal/mihomo`、不解析订阅或改写 YAML。

## 文件归属规则

- 新增 CLI 命令、参数、输出或错误映射：放在 `internal/cli`；`cmd/mm` 只做进程装配。
- 新增稳定业务标识/状态放在 `internal/domain`；新增用例接口和跨入口 DTO 放在 `internal/app`。
- 新增 SQLite 表只能追加 `internal/store/migrations/NNNN_name.sql`，已提交迁移不得改写；SQL/nullable/time 映射留在 `internal/store`。
- 新增环境变量、默认路径或配置文件位置：放在 `internal/config` 的 `Paths`/`Load`。
- 新增 mihomo API、配置变换、订阅解析或系统进程操作：放在 `internal/mihomo`。
- 新增页面状态、按键、异步消息或渲染：放在 `internal/tui`，通过窄接口注入 `internal/app` capability；所有业务调用封装为 `tea.Cmd`，不得放进 `View`。
- 新增 Go 测试：与被测代码同包放置为 `*_test.go`。现有示例是 `internal/mihomo/client_route_test.go` 和 `client_start_test.go`。
- 远程引导、安装和卸载分别维护 `scripts/bootstrap.sh`、`scripts/install.sh`、`scripts/uninstall.sh`；`launchd/` 仅保留旧 macOS 兼容资产。

## 命名与组织

- Go 包名使用简短小写单词，如 `app`、`config`、`mihomo`、`tui`。
- 导出类型和方法使用 Go 的 PascalCase；包内辅助函数使用 camelCase。可参考 `Client`、`RouteDiagnosisResult`、`parseSubscriptionConfig`。
- 测试名使用 `Test<行为>_<场景>`，例如 `TestUpdateSubscription_PreservesLocalPorts`。
- 配置路径必须来自 `config.Paths`，不要在 `internal/mihomo` 或 `internal/tui` 中新增硬编码的用户目录。

## 遗留代码边界

`bin/mihomo-manager`、`scripts/lib/*.sh`、`scripts/lib/proxy.py` 和未接入入口的 `internal/mihomo/client.go` 是旧兼容实现。当前 Go CLI/TUI 业务入口均经 daemon capability；新功能默认实现于 Go 包和 daemon 边界，只有维护旧兼容或安装流程时才修改这些资产。
