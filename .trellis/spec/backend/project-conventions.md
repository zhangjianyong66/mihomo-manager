# 重要项目约定

## 产品与兼容边界

- 当前产品是终端优先 Go 应用，无参数 `mm` 与显式 `mm tui` 打开相同界面。A6 已提供 core/config/group/node 和 legacy subscription/route/log 业务 CLI；旧 Shell 子命令仍不是产品契约。
- `bin/mihomo-manager` 和 `scripts/lib` 是遗留实现；除兼容维护外，不作为新功能入口，也不能用其帮助信息判断 Go 版能力。
- 项目运行时依赖独立的 mihomo core 和本机 external-controller；一键安装器会准备默认 core，但 Go 二进制本身不内嵌代理内核。

## 配置文件与环境变量

`internal/config/config.go` 是 Go 版路径的事实来源：

| 项目 | 默认值 | 覆盖方式 |
|------|--------|----------|
| mihomo core | `~/.local/bin/mihomo` | `MIHOMO_BIN` |
| 配置目录 | `~/.config/mihomo` | `CONFIG_DIR` |
| 主配置 | `<CONFIG_DIR>/config.yaml` | 随配置目录变化 |
| 备份 | `<CONFIG_DIR>/config.yaml.bak` | 随配置目录变化 |
| CN 规则集目录 | `<CONFIG_DIR>/rulesets` | 随配置目录变化 |
| CN domain 规则集 | `<CONFIG_DIR>/rulesets/cn-domain.mrs` | 随配置目录变化 |
| CN IP 规则集 | `<CONFIG_DIR>/rulesets/cn-ip.mrs` | 随配置目录变化 |
| 订阅地址 | `<CONFIG_DIR>/subscription.url` | 随配置目录变化 |
| 白名单 | `<CONFIG_DIR>/whitelist.yaml` | 随配置目录变化 |
| mihomo 日志 | `<CONFIG_DIR>/mihomo.log` | 随配置目录变化 |
| external-controller | `http://127.0.0.1:9090` | `MIHOMO_API_PORT` 仅覆盖端口 |

配置和订阅文件可能包含节点地址、密码或订阅凭据，不得写入测试夹具之外的仓库文件、日志或文档示例。

安装器固定 `MetaCubeX/meta-rules-dat` commit `32ae0e8658ca541374b721efcee84955e8a59755`，两份 `.mrs` 经 SHA-256 和 mihomo 原生 provider 配置校验后以 `0700/0600` 权限发布。覆盖 `MM_RULESET_BASE_URL` 或 `MM_RULESET_REF` 时必须同时提供 `MM_RULESET_DOMAIN_SHA256` 与 `MM_RULESET_IP_SHA256`，不得绕过完整性校验。

## 路由规则语义

- `domain.RoutingMode` 仅允许 `global|rule|direct`，不得与 `ProfileMode(managed|external|legacy)` 混用；缺少配置 `mode` 时 policy 按 `rule` 解析。
- `internal/mihomo/routing_policy.go` 是 Rule 规则、CN providers 与 DNS 所有权的唯一来源。顺序固定为本机/局域网、自定义规则、白名单、`mm-cn-domain`、`mm-cn-ip`、唯一 `MATCH,🌐 代理`。
- 白名单域名生成 `DOMAIN-SUFFIX,<domain>,DIRECT`；清理 manager 本机规则、旧 CN Geo 规则、两个 manager provider 规则和所有旧 `MATCH`，其他 custom rules/providers 保持内容及相对顺序。
- CN providers 使用 `type: http`、`format: mrs`、`behavior: domain|ipcidr`、相对 `<CONFIG_DIR>/rulesets` 路径、MetaCubeX `meta` 更新 URL 和 `interval: 86400`。
- Rule DNS 管理 `enable`、`respect-rules`、`default-nameserver`、`nameserver`、`proxy-server-nameserver` 及本机/CN nameserver policy；保留 fake-IP、IPv6 和不冲突的用户 policy，移除会绕开该模型的旧 fallback 字段。
- `ApplyRouteCN()` 是设置 `mode: rule` 并应用同一 policy 的兼容入口，不得恢复独立 `GEOSITE/GEOIP + MATCH,GLOBAL` 算法。所有写入使用 `mihomo -t -d <dir> -f <file>` 校验，失败恢复内容和权限。

## 订阅更新

- `UpdateSubscription()` 支持完整 YAML，以及纯文本或 base64 编码的 URI 列表；Go 版当前解析 `vless://`、`vmess://`、`trojan://`、`ss://`。
- 更新前读取旧配置和白名单并备份主配置；下载默认绕过系统代理，最多重试 3 次。
- 订阅内容不能覆盖本地 `mixed-port`、`socks-port` 和 `external-controller`；缺失时分别使用 `7890`、`7891`、`127.0.0.1:9090`。
- 更新仅替换 proxies/proxy-providers 和 `🌐 代理`/`🎯 直连`候选，随后重放旧 mode、custom rules/providers、白名单、CN providers 和 DNS policy。
- core API 可用时分别保存/恢复 `GLOBAL` 与 `🌐 代理`选择；节点消失按节点名排序选择首个候选并返回 typed warning，reload/selection 失败恢复旧配置和旧选择。
- YAML 重写可能改变字段顺序并丢失注释，这是当前实现的已知行为。

## 模式事务

- `internal/legacy/mode.go` 是活动 legacy profile 的模式事务入口；候选始终重放 M2 policy，先在同目录 `0600` 临时文件执行原生校验，再备份、原子发布并重新核对内容和 mode。
- core stopped 时不得访问 controller，只返回下次启动生效；running 时经 `internal/mihomo/runtime.go` typed client reload/set/verify，Rule 额外核验两个 CN RULE-SET 和唯一代理兜底。
- 发布后的任一配置/runtime/expected 摘要失败都恢复旧文件、权限、expected 摘要和旧 runtime mode。恢复动作使用独立的有界 context；恢复失败标记 core `failed` 并返回 `RESTORE_FAILED`。
- mode、core、subscription、config 与 route 写操作共享 daemon `Coordinator`；HTTP PUT `/v1/mode` 必须带 request ID。默认不关闭连接，显式关闭失败是 partial-success，不回滚模式。

## 白名单

- `whitelist.yaml` 是白名单的持久化来源，结构为 `domains: []`。
- 如果白名单文件不存在，会从旧 `config.yaml` 中的直连域名规则迁移并写入新文件。
- 域名写入前需要去协议、路径、通配前缀，转为小写、去重并排序；`*.example.com` 最终按 `example.com` 存储。

## 系统与运行约定

- manager daemon 的默认 data/state/runtime/socket/lock/systemd user unit，以及 `generations`、core log/runtime metadata 路径均由 `config.ResolveManagerPaths` 集中计算，优先使用 XDG 环境变量；测试通过 `ManagerEnvironment` 注入临时绝对路径。
- daemon 是 manager 状态写入和 2.x core 进程的唯一所有者，IPC 仅限同 UID Unix socket；无 daemon 时 CLI/TUI 不得回退直接写 SQLite、配置或控制 core。daemon 启动只装配 CoreManager，core 初始状态为 `stopped`。

- 1.x TUI 兼容层仍通过进程模式 `mihomo.*-f.*config.yaml` 查找 core；2.x daemon 严禁复用该逻辑，只停止 adapter 返回并由 supervisor 持有的 `Process`。
- managed 配置写到 XDG generation 目录；external/legacy 配置只读并在 validate/start 间核对 SHA-256，不得覆盖 `~/.config/mihomo/config.yaml`。
- external-controller 默认只监听 `127.0.0.1`；不要在没有明确需求和安全评估时扩大到公网地址。
- 运行日志写入 `<CONFIG_DIR>/mihomo.log`；TUI 日志页只保留最近 500 行内存缓冲，并支持正则过滤。
- 配置管理、订阅更新和路由功能涉及用户真实网络环境；自动化测试必须隔离到临时目录，不能改写用户的 `~/.config/mihomo`。

## Scenario：legacy 迁移与恢复点

### 1. Scope / Trigger

- 触发：增加 `migrate plan|apply|status|rollback`、SQLite legacy schema 和跨层 Unix IPC。
- 目标：识别并保留 1.x `CONFIG_DIR`，让 2.x 管理 legacy metadata，同时可安全回退。

### 2. Signatures

- CLI：`mm migrate plan|apply|status|rollback --output table|json`；rollback 必须有 `--restore-point <id>`。
- IPC：`GET /v1/migrations/plan`、`POST /v1/migrations/apply`、`GET /v1/migrations/status`、`POST /v1/migrations/rollback`。
- 数据库：`legacy_migrations(id, profile_id, source_dir, state, error_code, timestamps)` 与 `legacy_files(migration_id, relative_path, before_*, expected_*, snapshot_path)`，通过追加 `0003_legacy_migrations.sql` 创建。

### 3. Contracts

- apply 前创建 `${XDG_DATA_HOME:-~/.local/share}/mihomo-manager/backups/<id>/files`，目录/文件权限 `0700/0600`；manifest 保存 before 内容和 SHA-256，expected 摘要随 daemon 兼容写入更新。
- `CONFIG_DIR` 只选择旧 mihomo 源目录；`MIHOMO_BIN` 只用于受控验证；`MIHOMO_API_PORT` 只用于 loopback 探测；`EDITOR` 不会被迁移自动调用。
- CLI 不可用 daemon 时不得直接读取 SQLite、配置、订阅或 core；响应使用 `mm/v1` envelope，URL/token/完整 YAML 不进入日志或展示。

### 4. Validation & Error Matrix

- 空或缺失 `config.yaml` -> 创建可查询恢复点和非活动 legacy profile，operation `failed`，返回 `VALIDATION_FAILED`/退出码 6。
- mihomo 原生验证失败 -> 同上，旧文件摘要不变。
- 重复 source 或恢复点 ID -> `CONFLICT`/退出码 4，不覆盖已有快照。
- rollback 当前摘要不等于 expected、符号链接、路径越界或不安全权限 -> `CONFLICT`/`PERMISSION_DENIED`，不做部分恢复。
- daemon 不可用/协议不兼容 -> 退出码 5。

### 5. Good/Base/Bad Cases

- Good：有效旧配置、订阅 URL、白名单和历史备份被发现；apply 不改写源文件，status 返回 succeeded，且无活动档案时激活 legacy。
- Base：空环境、无效配置、重复 apply、daemon 重启和取消请求均可查询，不产生伪造 managed 配置。
- Bad：恢复点缺失、外部修改、路径逃逸、world-writable 文件或无法判断归属均拒绝写入。

### 6. Tests Required

- `internal/legacy`：发现脱敏、快照权限、无效/空配置、重复、摘要冲突、显式 rollback；断言源文件内容/权限和不启动真实 core。
- `internal/store`：v3 schema、事务回滚、唯一 source、profile activation 和 manifest round-trip。
- `internal/daemon`：HTTP 方法、IPC 路由、错误状态/代码和显式 restore point 校验。
- `internal/cli`：table/json、stdout/stderr、退出码和 `--restore-point` 必填。

### 7. Wrong vs Correct

#### Wrong

CLI 直接打开 `state.db` 或 `CONFIG_DIR`，重复 apply 先覆盖同名恢复点，rollback 默认猜最近快照。

#### Correct

CLI 只调用 daemon IPC；恢复点目录使用临时目录加原子发布且已存在即冲突；rollback 必须显式指定 ID 并核对 expected 摘要。
