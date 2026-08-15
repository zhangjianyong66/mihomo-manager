# 域名与 IP 分流规则设计

## 架构边界

分流规则仍属于 legacy profile 的配置派生数据。CLI/TUI 仅通过 `app.CapabilityAPI` 调用 daemon；daemon 通过 `legacy.Compatibility` 创建 `mihomo.Client`，由 client 读取、验证、原子写入并恢复 `config.yaml`。不增加 SQLite schema，也不让 CLI/TUI 直接访问 `whitelist.yaml` 或配置文件。

## 数据模型与兼容

`whitelist.yaml` 演进为两类规则的私有持久化文件：保留 `domains` 作为旧版本直连域名字段，并新增明确的 `direct` 与 `proxy` 条目列表。读取时合并、规范化、去重；写入时使用新字段。旧文件只有 `domains` 时无损映射为直连规则。

内部使用带目标的受管规则值，而不是把规则类型编码在字符串中：

- `direct` 目标渲染为 `DIRECT`。
- `proxy` 目标渲染为 `mihomo.ProxyGroupName`（`🌐 代理`）。
- 域名渲染为 `DOMAIN-SUFFIX`；IPv4 网络渲染为 `IP-CIDR`；IPv6 网络渲染为 `IP-CIDR6`。
- 输入通过 `net/netip` 判定 IP/CIDR；域名延续既有 URL、`*.`、小写和尾点规范化逻辑。解析失败返回输入错误，不静默忽略。

旧 `/v1/routes/whitelist`、`CapabilityAPI.Whitelist/AddWhitelist/RemoveWhitelist/EditWhitelist` 和 `mm route whitelist` 保留为直连规则兼容别名。新的通用 route-rules API 能表达类别、查询和替换值；旧调用不应要求调用方升级。

## 路由合成

`RoutingPolicy` 从单一 `Whitelist []string` 演进为受管直连与代理规则集合。`ParseRoutingPolicy` 识别并剥离两类受管规则，保留用户手写规则；`ApplyRoutingPolicy` 在本机规则和用户自定义规则之后、CN providers 之前渲染受管规则。

跨类别排序使用稳定的“具体性优先”：

1. 域名按标签深度降序（父/子关系中子域名先输出）。
2. IP 网段按前缀长度降序（单 IP 的 `/32` 或 `/128` 最优先）。
3. 无重叠的同级规则按规范化字符串排序，保证配置稳定。

类别相同的重复规则去重；同一规范化域名或同一规范化网络同时存在于 `direct` 与 `proxy` 时拒绝 mutation。范围重叠允许，排序决定命中结果。手写规则保持更高优先级，且本机/局域网、CN providers、`MATCH,🌐 代理` 和 DNS 合成不变。

## IPC、CLI 与 TUI

新增 daemon `/v1/routes/rules`，返回两类受管规则并接受类别化 add/edit/remove 请求。`app.CapabilityAPI`、daemon capability 和 legacy compatibility 增加等价 typed 方法，HTTP 请求继续使用 `MM-Request-ID`、Coordinator 和现有错误映射。

CLI 暴露 `mm route direct|proxy list|add|remove|edit`；写命令支持 `--restart`。默认成功只报告已保存、下次启动生效；`--restart` 在保存成功后调用受管 Core restart，重启失败返回错误但不回滚已验证的规则文件。旧 `route whitelist` 继续映射到 direct。

TUI 主菜单重命名为“域名分流”。进入后先选“直连规则”或“代理规则”，再沿用现有列表/输入/条目操作状态机。写入完成后读取 Core status：

- `running`：进入不可取消的副作用之前的确认页；Enter 调用 `CoreAction("restart")`，Esc 保留规则并返回结果页。
- `stopped`：不显示确认页，明确提示下次启动生效。
- status 获取失败或非稳定状态：不尝试自动重启，显示规则已经保存及状态查询错误。

在非 `rule` 模式中，状态页增加分流规则不生效的 warning；不改变当前 mode。

## 原子性、恢复与风险

规则 mutation 先在内存中计算新规则集合，检查跨类别冲突和原生 config 校验，再原子写配置和规则数据文件。任一步失败时恢复旧配置及旧规则文件，遵循现有 legacy mutation 的 expected SHA-256、备份和权限规则。新增文件仍为 `0600`。

“保存后重启”不是单一事务：保存成功即为持久结果；重启失败时应明确说明配置已保存、Core 未成功重启，由用户重试 Core 操作。这样不因运行态失败丢失合法配置。

## 测试策略

- `internal/mihomo`：输入规范化、IPv4/IPv6/IP/CIDR 渲染、旧文件迁移、冲突拒绝、具体性排序、自定义规则优先、订阅/路由预设保留、失败恢复和可选原生 mihomo 校验。
- `internal/daemon` 与 `internal/app`：typed IPC 请求/响应、旧白名单兼容、Coordinator、重启请求错误映射。
- `internal/cli`：direct/proxy 命令的 table/JSON、`--restart` 有无行为、profile、非法类别/输入和退出码。
- `internal/tui`：双列表导航、输入 mutation 经 tea.Cmd、running 确认/取消/重启、stopped 无确认与窄终端渲染。
