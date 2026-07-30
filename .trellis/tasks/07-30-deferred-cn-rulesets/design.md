# 支持延迟安装 CN 规则集：技术设计

## 1. 边界与单一事实来源

新增 `internal/ruleset` 包，拥有规则集目录安全、目标解析、状态检查、下载、SHA-256、mihomo 原生 pair 校验、metadata 和成对发布/恢复。`internal/cli`、`internal/tui` 只消费 `internal/app` 类型；所有运行期写入经 daemon capability 和共享 Coordinator。

在 `internal/ruleset/catalog.json` 保存默认 source、固定 ref、相对资产路径、behavior 和 SHA-256。Go 通过 `go:embed` 读取，`scripts/install.sh` 通过 `jq` 读取同一文件，删除 Shell/Go 重复常量。自定义 `MM_RULESET_*` 只覆盖本次目标，不修改嵌入清单。

在 `<CONFIG_DIR>/rulesets/.mihomo-manager.json` 保存 schema、source、ref 和两份已安装摘要，不保存代理或凭据。目录、资产和 metadata 权限为 `0700/0600`；三者作为一个发布事务处理。旧 `install-state` 继续供卸载和升级兼容，升级安装器会在离线验证旧缓存成功后生成新 metadata；运行期状态不再只依赖 installer state。

## 2. 状态模型

应用 DTO `RuleSetStatus` 包含：

- `state`: `installed|missing|invalid|outdated`
- 当前期望 source/ref、已安装 source/ref
- domain/IP 的 path、exists、digestValid
- `formatValid`、`reused`、`nextStart`、`runtimeReloaded`
- typed warnings，不含完整代理 URL、凭据或未经投影的底层错误

判定顺序：路径安全与文件类型、pair 是否完整、metadata/旧状态兼容、两份摘要、`mihomo -t -d <temp> -f <temp-config>` 格式校验、目标版本匹配。两份都不存在为 `missing`；单份、权限/摘要/格式错误为 `invalid`；旧记录下 pair 完整有效但不等于本次目标为 `outdated`；全部匹配为 `installed`。

`status` 不把缺失/过期当命令错误。危险路径或权限不可安全判断时返回 `PERMISSION_DENIED`。`install` 对 `installed` 只补齐兼容 metadata 后复用，不联网、不替换 `.mrs`。

## 3. 下载来源与代理选择

`app.DaemonCapabilities` 在调用进程内解析 `MM_RULESET_*` 和大小写代理环境，形成只含受校验字段的请求：

1. 首个已设置的 `HTTPS_PROXY`、`ALL_PROXY`、`HTTP_PROXY`（含小写别名）；
2. 未设置进程代理时，请求 daemon 自动选择 running Core 的 loopback mixed listener，其次 HTTP listener；
3. 两者均不可用时 direct。

选中一个路径后，连接失败不再静默尝试后续路径，避免绕开用户代理。支持无 userinfo 的 `http|https|socks5|socks5h`；认证代理在客户端和 daemon 双重拒绝为 `RULESET_PROXY_AUTH_UNSUPPORTED`，不把原 URL 放入 DTO/错误。为 SOCKS 使用与 Go 1.22 兼容的固定 `golang.org/x/net/proxy` 依赖。`file://` 只作为带可信摘要的规则集 source override，用于本地镜像和测试，不参与代理选择。

下载器使用 context、有限重试、连接/整体超时、响应状态检查和单资产大小上限；临时文件在规则集目录内以 `0600` 创建。任何下载、摘要或格式失败都只清理临时文件。

## 4. IPC 与应用契约

新增 daemon 路由：

- `GET /v1/rulesets/status`：query 携带非秘密的目标 source/ref/digests，返回 `RuleSetStatus`。
- `POST /v1/rulesets/install`：必须带 `MM-Request-ID`，body 为目标与可选脱敏代理 endpoint，响应为 NDJSON progress。

安装事件阶段固定为 `checking|downloading_domain|downloading_ip|validating|waiting_to_publish|publishing|reloading|verifying|rolling_back|succeeded|failed`。事件只携带阶段、最终 typed status、warnings 或结构化 terminal error；不携带下载 URL path、代理原值或文件内容。流式 request ID 继续遵循“不缓存、重放会重新执行”的现有协议，业务幂等由安装前/锁内二次状态检查保证。

`app.CapabilityAPI` 增加 `RuleSetStatus` 与 `InstallRuleSets`；app 层唯一解析 NDJSON 并投影 typed event，CLI/TUI 不自行转换 raw JSON。

## 5. 事务与取消

下载和格式校验在 Coordinator 外使用调用方 context，避免长时间阻塞其他写操作。校验完成后：

1. 使用调用方 context 获取 Coordinator；取消或冲突时零副作用。
2. 锁内重新检查状态，处理并发安装已完成的情况。
3. 确认 context 尚未取消，随后切换到独立有界事务 context。
4. 捕获旧 domain/IP/metadata 内容与权限，成对发布新资产和 metadata。
5. 若 running Core 当前配置引用两个 manager providers，执行 typed reload 并核验 Rule runtime；否则标记下次相关启动/切换生效。
6. 任一步失败恢复三个旧文件；运行中再 reload/verify 旧状态。恢复任一侧失败返回 `RESTORE_FAILED` 并标记 Core failed。

发布前 `Esc`/`Ctrl+C` 取消请求并保持文件/runtime 不变。发布阶段事件到达后，CLI/TUI 不再取消底层独立 context，等待成功或回滚 terminal；TUI 同时消费 `q`，避免退出后误以为事务中止。规则集安装不主动关闭连接。

## 6. 禁止隐式下载与操作门禁

`internal/mihomo/routing_policy.go` 继续作为规则顺序和 DNS 合成器，但 manager providers 改为 `type: file`，移除 URL/interval；只有 `rule` policy 注入 manager providers、两条 RULE-SET 和 Rule DNS。`global|direct` 清理 manager-owned rules/providers 后不依赖规则集，保持可用。

增加统一的“配置是否引用 manager providers”解析与 `internal/ruleset` readiness 检查。以下候选/操作在引用 manager providers 且状态非 `installed` 时返回 `RULESET_NOT_READY`，details 带脱敏 status 和 `mm ruleset install` 提示，且不执行 mihomo：

- Core start/restart/reload 和显式 config validate/replace；
- `mode set rule`、CN preset；
- Rule policy 下会重新合成配置的白名单、订阅等写操作。

不引用这两个 provider 的自定义 Rule 配置和最小 `MATCH,DIRECT` 配置仍可验证、启动。其他自定义 HTTP providers 不受此门禁影响。

## 7. 安装器行为

`scripts/install.sh` 默认不调用任何规则集网络下载。它只离线检查并保留既有有效 cache/metadata；缺失或无效时继续安装，并在摘要显示“待安装”及 `mm ruleset install`。不得输出空 ref 伪装成功。

新增 `--with-rulesets` 与 `MM_INSTALL_RULESETS=1`。显式选择才解析 `MM_RULESET_*` 并执行既有 Shell 安全事务；失败保持非零退出。两条路径均读取嵌入 catalog 文件并维护新 metadata 与兼容 install-state。`make install` 无参数行为保持默认跳过，bootstrap 原样透传新 flag。

## 8. CLI 与 TUI

CLI：

- `mm ruleset status [--output table|json]`，成功 kind `RuleSetStatus`。
- `mm ruleset install [--output table|json]`，成功 kind `RuleSetInstall`；table/json 最终结果共用 typed status，JSON stdout 始终只有一个 envelope。
- `RULESET_NOT_READY` 映射 conflict/退出码 4；source/proxy 输入错误映射 2；摘要/格式错误映射 6；下载失败映射 8；恢复失败映射 1。

TUI 在“配置管理 > CN 规则集”增加独立状态页。进入页异步加载状态；“安装/修复”先显示来源/ref、当前状态和可能热重载提示，确认后订阅 typed progress。页面只在 `tea.Cmd` 中调用 capability，渲染 `installed|missing|invalid|outdated`、两份校验与当前阶段；终态刷新 status。窄终端使用既有换行/截断约束。

## 9. 兼容、发布与回滚

- 旧有效默认 cache 可仅凭固定摘要和格式识别；旧自定义 cache 通过 installer state 一次性迁移到 metadata。
- 不修改 SQLite schema、JSON API version 或 mihomo core 固定版本。
- 升级后的 `type:file` provider 不再由 mihomo 每日自动更新；唯一更新入口为显式 ruleset install/修复，这是消除隐式网络的有意变化。
- 发布回滚以旧二进制和旧配置兼容为界：新 metadata 是附加文件，旧版本会忽略；旧 `.mrs` 路径不变。

## 10. 风险控制

- Shell 与 Go 行为漂移：共享 catalog，并用同一组 fixtures 断言来源/ref/摘要。
- 大文件/慢网络：限制大小、超时、有限重试、可取消临时下载。
- 客户端取消竞态：锁内最后一次取消检查后才进入独立事务 context。
- 运行态和磁盘分裂：发布后 reload/verify，失败恢复文件与 runtime，恢复失败显式 failed。
- 代理泄密：userinfo 在投影前拒绝，DTO/事件/错误只保留 scheme/host/port 或枚举来源。
