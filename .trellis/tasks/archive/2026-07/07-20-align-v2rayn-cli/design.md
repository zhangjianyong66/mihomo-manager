# 技术设计：对齐 v2rayN 的终端 CLI

## 1. 设计目标

把当前 mihomo 专用 TUI 演进为终端优先的代理客户端管理器：稳定 CLI 是产品契约，TUI 是交互前端，用户级 daemon 是运行状态和写操作的唯一所有者，core 适配器负责生成与运行不同内核配置。

本设计覆盖 2.x 到 3.0 的目标架构，但实施按独立里程碑推进。2.0 Alpha 只完成架构基线和现有能力等价迁移。

设计原则：

1. mihomo-first：先完成一个内核的纵向闭环，再接入第二个内核。
2. CLI-first：每个核心能力先定义命令、JSON、错误和退出码，再接入 TUI。
3. 单写者：只有 daemon 修改状态、生成配置或控制 core。
4. 不破坏：旧配置不自动改写，迁移前备份，2.x 可回退 1.x。
5. 普通用户运行：daemon/core 不长期 root，高权限能力显式配置并可撤销。
6. 默认保密：权限最小化、输出脱敏、连接明细不落盘。

详细能力差距见 `research/capability-matrix.md`，选型依据见 `research/architecture-options.md`。

## 2. 总体架构

```text
mm CLI ---------+
                |  HTTP/JSON + NDJSON
mm TUI ---------+  over Unix socket
                v
        +-------------------+
        |     mm daemon     |
        | local API/schedule|
        +---------+---------+
                  |
          application services
                  |
   +--------------+---------------+
   |              |               |
 domain/store  config generator  runtime supervisor
   |              |               |
 SQLite       core adapter     core process/API
                  |               |
                  +----- mihomo --+
                  +----- Xray (3.0)
                  +----- sing-box (3.0)
```

边界约束：

- CLI/TUI 不直接读写 SQLite、用户配置或 core API。
- daemon 的 HTTP API 不监听 TCP，不作为远程控制 API。
- 领域层不依赖 mihomo 原始 YAML 或 external-controller DTO。
- 平台操作通过接口隔离；首期 Linux 实现可使用构建标签。

## 3. 目标目录结构

```text
cmd/mm/                    # 单二进制入口
internal/app/              # 依赖装配、应用服务
internal/cli/              # Cobra 命令、输出、退出码
internal/tui/              # Bubble Tea 状态和渲染
internal/daemon/           # 本地 API、任务调度、运行状态机
internal/ipc/              # Unix socket 协议、客户端、peer 校验
internal/domain/           # core/profile/subscription/node/rule 模型
internal/store/            # SQLite 仓储、迁移、事务
internal/core/             # 适配器接口、进程监督
internal/core/mihomo/      # mihomo 生成、验证、API 和进程实现
internal/configgen/        # 快照、覆盖层、diff 和发布管线
internal/platform/         # systemd、TUN setup、系统代理
internal/security/         # 脱敏、权限、摘要、敏感值策略
```

现有 `internal/mihomo/client.go` 和 `internal/tui/model.go` 采用绞杀式迁移。新模块先包装已有已测行为，再按业务边界移动；任何阶段都保持可构建、可测试、可回退，不做一次性替换。

## 4. 配置档案与所有权

### 4.1 档案模式

| 模式 | 事实源 | 可管理范围 | 写回行为 |
|---|---|---|---|
| `managed` | SQLite 规范化模型 | 节点、订阅、路由、DNS、设置、覆盖层 | 只生成独立运行配置 |
| `external` | 用户指定完整 core 配置文件 | 验证、启动、停止、观察 | 永不写回源文件 |
| `legacy` | 现有 1.x `CONFIG_DIR/config.yaml` 及配套文件 | 保留 1.x 行为 | 仅 2.x 兼容层可按原语义备份后写回 |

`legacy` 只由升级迁移创建，不允许新建。它是 2.x 兼容例外，不是目标架构。用户通过 `mm migrate convert` 预览转换为 `managed`；转换成功也不删除旧文件。3.0 是否移除 `legacy` 需另立迁移任务。

### 4.2 单活动实例

- 存储可包含多个档案，但同一用户只有一个活动档案和一个主 core 实例。
- 切换档案先生成并验证目标配置，再停止旧实例。
- 新实例必须通过进程存活、管理 API 和代理端口就绪检查后才提交活动状态。
- 启动失败先尝试恢复旧实例；恢复失败则写入明确 `failed` 状态，不伪报运行中。

### 4.3 生成管线

```text
规范化档案快照
  -> core 基础配置生成
  -> 路由/DNS 预设和用户规则
  -> core 专属专家覆盖层
  -> 管理字段所有权/冲突检查
  -> schema/静态验证
  -> core 原生验证
  -> 同文件系统原子发布
```

提供 `mm config render|diff|validate`。覆盖层按 core 类型保存，不跨内核转换；冲突必须显式报错。

## 5. 领域模型与存储

### 5.1 SQLite

使用 `database/sql` 和无 CGO SQLite 驱动，默认数据库：

```text
~/.local/share/mihomo-manager/state.db
```

尊重 XDG 覆盖，但默认路径保持上述约定。数据库目录 `0700`，文件 `0600`。daemon 是唯一写者；只读诊断工具也优先经 daemon 访问。

首批表/聚合根：

- `schema_migrations`：schema 版本和迁移校验。
- `profiles`：档案、模式、core 类型、活动状态和版本。
- `subscriptions`：来源 ID、URL、策略、ETag/时间和上一成功状态。
- `nodes`：稳定 ID、来源 ID、协议、规范化字段和显示名。
- `routes`、`dns_policies`：有序规则和策略引用。
- `core_overrides`：按档案/core 保存专家覆盖层元数据和文件引用。
- `settings`：非敏感全局/档案设置。
- `operations`：跨数据库/文件/进程操作的阶段和恢复信息。
- `traffic_daily`：有限期聚合流量。

订阅更新在单事务中替换自身来源节点。显示名称不是身份；生成配置时根据来源和稳定 ID 确定性生成唯一名称。

### 5.2 文件布局

```text
~/.config/mihomo-manager/              # 用户可编辑的 manager 设置
~/.local/share/mihomo-manager/
  state.db
  external/                            # 外部配置引用元数据，不复制时仅记录路径
  overrides/<profile-id>/              # core 专属覆盖层
  runtime/<profile-id>/<generation>/   # 生成配置和运行元数据
  backups/                             # 迁移/升级恢复点
~/.local/state/mihomo-manager/
  logs/                                # manager/core 脱敏滚动日志
$XDG_RUNTIME_DIR/mihomo-manager/
  mm.sock
  daemon.lock
```

`CONFIG_DIR` 在 2.x 继续指向 legacy mihomo 配置目录，不重定义为 manager 配置目录。

## 6. CLI 产品契约

### 6.1 命令树

```text
mm                              # 保持打开 TUI
mm tui
mm daemon run|status|start|stop|enable|disable
mm core status|start|stop|restart|reload|logs
mm profile list|show|create|use|delete|import|export
mm subscription list|add|set|show|update|remove
mm node list|show|add|edit|remove|test|select
mm group list|show|select
mm config render|diff|validate|edit
mm route list|add|edit|remove|diagnose|preset
mm route whitelist list|add|edit|remove
mm dns show|set|preset
mm connections list|close
mm stats show|reset
mm tun status|enable|disable
mm setup tun|remove
mm update check|app|core|rules|rollback
mm backup export|import
mm sync push|pull|status
mm proxy env|system
mm privacy purge
mm migrate plan|apply|status|rollback|convert
```

未到对应里程碑的命令不提前放置空壳。

### 6.2 输出

- 人类默认输出使用稳定列含义，但不承诺终端宽度相关排版。
- 查询命令支持 `--output table|json`，流式命令支持 `--output text|ndjson`。
- JSON 使用稳定 envelope：

```json
{
  "apiVersion": "mm/v1",
  "kind": "CoreStatus",
  "data": {},
  "warnings": []
}
```

- 错误写 stderr；JSON 模式输出结构化错误，不混入进度文本。
- 秘密默认脱敏；`--show-secrets` 是显式能力，不由环境变量隐式开启。

### 6.3 退出码

| 退出码 | 含义 |
|---:|---|
| 0 | 成功 |
| 2 | 命令参数/输入错误 |
| 3 | 资源不存在 |
| 4 | 状态冲突或并发操作冲突 |
| 5 | daemon 不可用或协议不兼容 |
| 6 | 配置/迁移/校验失败 |
| 7 | 权限不足或安全策略拒绝 |
| 8 | core、订阅、更新源等上游失败 |
| 1 | 未分类内部错误 |

退出码和 JSON 字段从 2.0 起遵循兼容策略；新增字段允许，删除/改义必须经过主版本升级。

## 7. 本地 IPC

### 7.1 协议

- HTTP/JSON over Unix socket。
- API 路径以 `/v1/` 版本化；客户端发送版本范围，daemon 返回实际协议版本。
- 普通请求使用 JSON；日志、测速、连接事件使用 NDJSON。
- 修改请求携带 request ID，daemon 对短期重复请求提供幂等保护。
- daemon 不可用时 CLI 明确提示 `mm daemon status/start`，不退回直接写文件或直接控制 core。

错误对象：

```json
{
  "code": "PROFILE_CONFLICT",
  "message": "目标档案正在切换",
  "retryable": false,
  "details": {}
}
```

`message` 可本地化，`code` 和 `details` 字段才是机器契约。

### 7.2 访问控制

- socket 位于当前用户运行目录，目录 `0700`、socket `0600`。
- Linux 使用 peer credentials 校验 UID；校验失败立即断开。
- 不监听 TCP，不支持局域网共享，不在 IPC 层实现远程认证。
- 远程管理通过 SSH 执行相同 CLI。

## 8. Daemon 与进程监督

daemon 状态机：

```text
stopped -> starting -> running -> stopping -> stopped
              |          |
              v          v
            failed <--- degraded
```

- 一个操作锁保护启动、停止、档案切换、配置发布和 core 更新。
- core 异常退出使用指数退避，并设置时间窗内最大重启次数；超过阈值进入 `failed`。
- daemon 重启时读取 `operations` 和 PID/runtime 元数据，辨认正在运行的受管 core，不能仅按进程名误杀。
- core 进程使用独立 session；daemon 正常停止时按策略关闭，daemon 崩溃时由 systemd 和恢复逻辑接管。
- systemd user `.socket` 可按需激活 `.service`，但激活 daemon 不等于启动代理 core。
- Ubuntu/Debian 正常用户 systemd 会话中，安装器默认安装并 `enable --now mm.socket`，使首次 `mm` 可按需激活 daemon；提供显式 `--no-daemon` 跳过。systemd user 不可用时保留 mm/unit 文件并明确提示使用 `mm daemon run`，不得假报成功启用。
- 开机常驻/linger 是显式设置，不在安装器中静默开启。

## 9. Core 适配器

核心接口负责发现、生成、验证、进程和运行 API，不承担订阅解析或 CLI 输出。

```go
type Adapter interface {
    Type() CoreType
    Discover(context.Context) (Installation, error)
    Render(context.Context, ProfileSnapshot) (RenderedConfig, error)
    Validate(context.Context, RenderedConfig) error
    Start(context.Context, RuntimeSpec) (Process, error)
    Runtime(context.Context, RuntimeEndpoint) RuntimeClient
}
```

2.x 只实现 mihomo。3.0 先接 Xray，再接 sing-box。接口按实际第二个适配器的差异演进，禁止为未知 core 预先堆叠空抽象。

mihomo 适配器复用当前已验证能力：进程新 session、external-controller、代理组切换、延迟测试、配置测试、日志和路由诊断。原 `Client` 逐步拆成进程、API、订阅/解析、配置和兼容服务。

## 10. 迁移与兼容

### 10.1 首次升级

```text
discover -> plan -> backup -> create legacy profile metadata
         -> validate existing config -> install/activate daemon socket
         -> leave core stopped/running state unchanged where safely detectable
```

- 自动步骤只读取旧配置并创建时间戳备份，不转换或重写 YAML。
- `subscription.url`、`whitelist.yaml` 等仍由 legacy 兼容服务使用。
- 若旧配置无效，迁移记录错误但不删除旧 mm/core/config。
- `mm migrate plan --output json` 展示将创建、读取、备份和保持不变的路径。
- `mm migrate rollback` 移除新状态/服务注册并恢复升级前 mm，保留用户旧配置。

### 10.2 转换 managed

- `mm migrate convert` 生成候选规范化档案和覆盖层。
- 展示无法识别字段、节点、规则和语义差异。
- 候选必须渲染后通过 mihomo 原生验证。
- 用户明确确认后才切换活动档案；原配置继续保留。

### 10.3 环境变量

2.x 继续支持 `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT`、`EDITOR`。使用旧变量时可输出弃用或作用域提示，但行为不得在 2.x 删除。新路径遵循 XDG 变量，并由 `internal/config` 集中解析。

## 11. 安全设计

- daemon/core 普通用户运行；不安装 setuid，不写宽泛 sudoers。
- TUN 使用显式 `sudo mm setup tun`，先显示计划，再进行最小权限设置，并提供 `--remove`。
- core 更新后重新验证权限；不得自动继承 capability。
- 状态目录 `0700`，敏感文件 `0600`，启动和迁移均审计权限。
- 订阅 URL、节点 URI、UUID、密码和同步凭据默认脱敏；日志和错误使用结构化字段后再统一脱敏。
- 更新下载到同文件系统临时路径，校验摘要/签名、执行版本和配置冒烟后原子切换。
- 不自动切换第三方镜像；覆盖下载源必须显式配置并在状态输出中可见。
- 项目采用 MIT；v2rayN 仅作为能力参考，不复制 GPL 源码。发布前生成第三方许可证清单。

## 12. 可观测、备份与桌面边界

- 实时连接只在内存，daemon 重启即清除。
- 流量仅保存档案/节点/日期聚合，默认 30 天。
- 日志脱敏，默认 7 天、总量 50 MiB；`mm privacy purge` 清理。
- 备份采用版本化逻辑导出并整体加密，不上传活动 SQLite 文件。
- WebDAV 是首个传输适配器；冲突默认停止。
- `mm proxy env` 输出 shell 指令，不修改 shell 配置。
- GNOME 适配器保存和恢复完整原设置；SSH/无 DISPLAY 不自动修改。
- 托盘、主题、GUI 热键、剪贴板监听永久排除。

## 13. 发布与回滚

每个里程碑都必须：

1. 使用预发布版本和变更说明。
2. 数据 schema 迁移前创建恢复点。
3. 新旧 mm/core/config 的替换均在验证后原子进行。
4. 保留上一可用 mm、core 和数据库备份。
5. 提供自动化回滚命令和人工恢复文档。
6. 不跨两个高风险边界合并发布，例如 Alpha 不同时加入 TUN。

2.x 的回退目标是恢复 1.x 二进制和原 `~/.config/mihomo` 行为；由 2.x 创建的新数据可保留，但 1.x 不读取。

## 14. 测试策略

- 领域/存储：临时 SQLite、迁移前后 schema、事务回滚、来源隔离和权限。
- IPC：临时 Unix socket、peer UID、协议协商、结构化错误、NDJSON 取消。
- Core：假 core 进程、`httptest.Server`、就绪/崩溃/退避/恢复。
- 配置：golden snapshots、覆盖层冲突、确定性渲染、真实 mihomo `-t` 契约测试。
- CLI：命令树、stdout/stderr、JSON schema、退出码和秘密脱敏。
- TUI：应用服务 fake，验证页面状态，不触碰文件/进程/网络。
- 迁移：临时 HOME 覆盖 Good/Base/Bad 场景，旧文件摘要在失败后不变。
- 安装：保留现有 shell 测试并增加 user unit/socket、回滚和独立二进制验证。
- 架构：amd64/arm64、`CGO_ENABLED=0` 构建门禁。

全量基础命令：

```bash
test -z "$(gofmt -l cmd internal)"
go test ./...
go vet ./...
go build -o /tmp/mm ./cmd/mm
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
```

涉及生成配置时额外执行受控 mihomo 配置验证；测试不得读取真实订阅或修改真实用户配置。
