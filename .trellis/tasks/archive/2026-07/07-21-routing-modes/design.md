# 技术设计：三模式路由与实时链路

## 1. 设计目标

在现有 2.x legacy capability 基础上建立一条统一的模式与路由控制链：CLI/TUI 只调用应用 port，应用客户端只经 Unix IPC 访问 daemon，daemon 串行化配置与运行态变更，mihomo adapter/runtime 负责原生验证和运行态核验。安装器只准备受校验的静态资产和用户级 daemon，不启动代理 core 或接管系统代理。

本父设计固定跨 milestone 契约；各 milestone 的文件归属、测试和回滚点见对应子任务设计。

## 2. 总体数据流

```text
scripts/install.sh
  -> CONFIG_DIR/rulesets/{cn-domain,cn-ip}.mrs
  -> install-state 规则集元数据
  -> mm daemon enable/start
  -> 新建配置时 migrate apply

mm CLI / mm TUI
  -> internal/app typed ports + DTO
  -> internal/ipc HTTP/JSON or NDJSON over Unix socket
  -> internal/daemon routing capability + operation lock
  -> internal/legacy compatibility transaction
  -> internal/mihomo config transform / native validation / runtime API
  -> config.yaml + mihomo /configs, /rules, /connections
```

事实源分工：

- legacy `config.yaml`：持久化模式、规则 provider、规则顺序和 DNS 策略。
- mihomo `/configs`、`/rules`、`/connections`：实际运行模式、加载规则和活动连接的唯一运行态事实源。
- `whitelist.yaml`：白名单持久化事实源。
- legacy migration expected SHA-256：daemon 安全写入前的外部修改冲突检测。
- `install-state` 与已安装 `.mrs`：首次安装资产来源、固定引用、摘要和路径的审计来源。
- 不新增连接历史表；活动连接只在内存和 NDJSON 流中存在。

## 3. 稳定领域与应用契约

新增 `domain.RoutingMode` 枚举，仅允许 `global|rule|direct`，与现有 `domain.ProfileMode`（managed/external/legacy）严格区分。

`internal/app` 定义单一模式/路由能力：

```go
type RoutingModeStatus struct {
    ConfiguredMode domain.RoutingMode
    RuntimeMode    domain.RoutingMode
    CoreState      domain.CoreState
    EffectiveGroup domain.GroupID
    SelectedNode   domain.NodeID
    ActiveConnections int
    Rulesets       []RulesetStatus
    Ingress        ProxyIngressStatus
    Warnings       []string
}

type SetRoutingModeRequest struct {
    ProfileID       domain.ProfileID
    Mode            domain.RoutingMode
    CloseConnections bool
}

type Connection struct {
    ID, Host, DestinationIP, Network, Rule, RulePayload string
    Chains []string
    Upload, Download int64
}
```

同一 DTO 贯穿 daemon JSON、app client、CLI presenter 和 TUI model；mihomo 原始 JSON 只在 `internal/mihomo` 解码一次，不允许各入口重复读取 `map[string]any`。

## 4. Rule 配置合成

manager 保留三个展示组：内置 `GLOBAL`、自定义 `🌐 代理`、仅含 `DIRECT` 的 `🎯 直连`。运行模式与组分离：Global 使用 `GLOBAL`，Direct 使用原生 direct，Rule 的兜底使用 `🌐 代理`。

规则顺序固定为：

```yaml
rules:
  # manager 本机/局域网规则
  - DOMAIN,localhost,DIRECT
  - DOMAIN-SUFFIX,local,DIRECT
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR6,::1/128,DIRECT,no-resolve
  - IP-CIDR6,fc00::/7,DIRECT,no-resolve
  - IP-CIDR6,fe80::/10,DIRECT,no-resolve
  # 用户自定义非 manager 规则，保持原顺序
  # 白名单 DOMAIN-SUFFIX,<domain>,DIRECT
  - RULE-SET,mm-cn-domain,DIRECT
  - RULE-SET,mm-cn-ip,DIRECT,no-resolve
  - MATCH,🌐 代理
```

`rule-providers.mm-cn-domain` 使用 `behavior: domain`、`format: mrs`；`mm-cn-ip` 使用 `behavior: ipcidr`、`format: mrs`；`path` 指向 `CONFIG_DIR/rulesets` 已安装缓存，`url` 指向可覆盖的 MetaCubeX `meta` 稳定更新引用，`interval: 86400`。

配置变换以 provider 名称和完整规则签名识别 manager 所有字段，删除旧 `MATCH`、旧 CN Geo 规则、重复 manager 规则和旧 provider 定义，保留其他 custom rule/provider 的相对顺序。YAML 注释和字段顺序仍不承诺保留。

DNS 只接管满足路由正确性所需的 resolver 字段，保留 `enhanced-mode`、fake-IP 范围/过滤、IPv6 和不冲突的 `nameserver-policy`。目标模板使用国内 bootstrap/直连 resolver、境外加密 resolver、`proxy-server-nameserver` 防循环和 `respect-rules`；最终字段必须以 mihomo v1.19.28 原生验证和隔离 DNS 测试为准，不能凭 YAML 可解析即判定有效。

## 5. 规则集安装与更新

默认首次安装固定：

- 仓库：`MetaCubeX/meta-rules-dat`。
- commit：`32ae0e8658ca541374b721efcee84955e8a59755`。
- `geo/geosite/cn.mrs` SHA-256：`52c146262ef51dc23a84533a0d13f8addd031c61708a863d17cdb75cc3089ee4`。
- `geo/geoip/cn.mrs` SHA-256：`206ad4cc22005976e8bfb50a869e5483cb81cc174a56c9a79c8a13e3e64e2eea`。

实现前 M1 再验证固定引用仍可下载，随后把引用和摘要作为同一组常量提交。`MM_RULESET_BASE_URL` 与 `MM_RULESET_REF` 只能显式覆盖来源；覆盖固定引用时必须同时提供可信摘要或通过受控 manifest 取得摘要，不能因自定义源而跳过校验。

安装写入同目录临时文件，摘要和 mihomo provider 配置验证通过后原子替换。首次无缓存失败终止安装；升级失败保留旧缓存。运行时 provider 更新失败由 mihomo 继续使用旧缓存。

## 6. 模式切换事务

模式切换只支持已成功注册的活动 legacy profile；全新安装器创建的配置自动注册，已有配置要求显式 migrate。

事务步骤：

1. 获取 daemon 配置/core 操作锁，读取 profile、migration expected 摘要、原配置字节和当前 runtime mode。
2. 检查源文件未被外部修改，按目标模式构建候选；Rule 模式确保 provider/rules/DNS 契约，其他模式保留这些配置以便后续无损切回。
3. 在同目录私有临时文件写候选，执行静态校验与 `mihomo -t -d <dir> -f <candidate>`。
4. 原子替换 `config.yaml` 并刷新 migration expected 摘要。
5. core 停止时返回“已保存，下次启动生效”。core 运行时优先用类型化 runtime API 更新 mode；首次引入规则/DNS 等需重载完整配置时使用受控 reload。
6. 查询 `/configs` 核验 runtime mode；Rule 模式同时核验 provider/rules 已加载。根据请求可关闭 `/connections`。
7. 任一步失败恢复原文件、expected 摘要和原 runtime mode；恢复失败返回明确 `RESTORE_FAILED` 并将 operation/core 状态标成 failed，不伪报成功。

默认不关闭既有连接。状态返回切换后仍存在的活动连接数；显式 close 只调用 mihomo `/connections`，不触碰系统网络连接。

## 7. 订阅更新兼容

订阅更新先从旧配置提取 routing overlay：mode、manager providers/rules、custom rules/providers、DNS、白名单和两个组的选择。更新仅替换订阅提供的 proxies 与组候选列表，再重新应用 routing overlay。节点选择仍存在时保留；消失时确定性回退首个可用节点并返回 warning。

该变换与模式切换必须复用同一个 `RoutingPolicy` 解析/合成模块，禁止分别维护规则顺序。

## 8. CLI、TUI 与 IPC

新增 IPC：

- `GET /v1/mode`：配置/运行模式、组节点、规则集健康、连接数和 warnings。
- `PUT /v1/mode`：`profileId`、`mode`、`closeConnections`，带 request ID。
- `GET /v1/connections`：当前快照。
- `GET /v1/connections/follow`：daemon 轮询 mihomo 并输出单调 seq 的 NDJSON event/done/error。

CLI 使用 `mm mode status`、`mm mode set <mode> [--close-connections]` 与 `mm route connections [--follow]`。查询遵循 table/json；follow 遵循 text/NDJSON。

TUI 从 `*mihomo.Client` 迁移到窄化的 app capability interface；页面只消费 typed DTO/event，不读配置、不调用原始 HTTP。迁移必须覆盖现有页面，发布时不得保留两套写路径。

## 9. 入口诊断

daemon 提供 mihomo 监听端口和运行状态；只读 platform inspector 检测当前 GNOME system proxy。CLI 进程本地规范化 HTTP(S)/ALL_PROXY 环境端点时只保留 scheme/host/port，不传递或显示 userinfo/path/query。状态将这些入口与 mihomo listener 比对并生成 warning。

检测失败只降低诊断置信度，不改变模式切换结果。任何入口诊断都不得写 gsettings、停止进程或占用端口。

## 10. 安装、daemon 与迁移

`scripts/install.sh` 在 mm 原子安装后调用正式 CLI 安装/启用 user units；systemd 用户会话可用时启动 manager daemon。新建配置才自动调用 migrate apply；已有配置只打印 plan/apply 提示。无 systemd 会话时只保留 unit 与前台提示。

升级发现 daemon/core 正在运行时不得为替换 manager 进程中断 core：core running 时保留旧 daemon 进程并提示稍后显式重启；core stopped 时可启动/重启 manager daemon。安装器始终不启动 core、不启用 linger、不修改系统代理。

## 11. 兼容与回滚

- 只实现当前 active legacy profile 的写能力；managed profile 后续复用领域/app 契约，但不在本任务扩展完整 managed 路由模型。
- external profile 保持只读并返回 unsupported。
- 原 `route preset cn` 迁移为 Rule 模式的兼容别名或清晰弃用提示，不能继续生成 `MATCH,GLOBAL` 的分叉逻辑。
- 安装资产、配置候选、IPC DTO、NDJSON 事件都必须有大小、类型、摘要或 schema 校验。
- 每个 milestone 独立提交；任一 milestone 失败时按子计划回滚，不跨越未通过的依赖门。

## 12. Milestone 设计

1. [M1 安装 CN 规则集与用户 daemon](../archive/2026-07/07-21-m1-ruleset-install/design.md)
2. [M2 构建 Rule 分流与订阅保留](../archive/2026-07/07-21-m2-rule-policy/design.md)
3. [M3 实现模式事务与 daemon API](../archive/2026-07/07-21-m3-mode-transaction/design.md)
4. [M4 提供模式 CLI 与 TUI](../archive/2026-07/07-21-m4-mode-surfaces/design.md)
5. [M5 提供实时链路与入口诊断](../archive/2026-07/07-21-m5-route-observability/design.md)
