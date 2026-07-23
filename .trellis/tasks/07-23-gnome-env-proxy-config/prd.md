# 支持修改 GNOME 与环境代理配置

## Goal

让用户通过 `mm` 安全地将 GNOME 桌面应用和命令行程序的 HTTP、HTTPS、SOCKS 代理入口指向 mihomo，并能明确关闭或恢复，避免用户手工同步多套配置。

## Background

- 现有 `mode status` 已会诊断 mihomo listener、GNOME HTTP/HTTPS/SOCKS 系统代理和当前 CLI 进程的 `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY`，但全部只读。
- GNOME 诊断由 daemon 通过受控的 `gsettings get` 执行；环境代理由 CLI 客户端从自身进程环境读取，两者的作用域不同。
- 已归档的 2.3 规划提供了能力边界基线：GNOME 启用前保存全部原设置，关闭时精确恢复；SSH/无桌面会话不自动修改。本任务将环境代理扩展为 `~/.bashrc` 的受管理标记区块。
- 当前项目只声明 Ubuntu/Debian 支持；KDE、其他桌面和 PAC 在既有路线中均不属于首个 GNOME 适配器。
- 当前 GNOME schema 的 HTTP 代理还包含 `use-authentication`、`authentication-user`、`authentication-password`；处理已启用认证的原系统代理会跨越凭据持久化边界。

## Requirements

- 提供 HTTP、HTTPS、SOCKS 和覆盖所有支持入口的代理配置能力。
- 复用现有 mihomo mixed/http/socks listener 与 loopback 端点诊断模型，不引入另一套端口事实源。
- GNOME 修改必须可诊断，且失败不得留下无法识别的部分配置。
- 环境代理由 `mm` 直接持久化到 `~/.bashrc`，使新建 Bash 终端及其子进程自动获得代理环境变量。
- 首版只管理 Bash 的 `~/.bashrc`，不自动检测或修改 `.zshrc`、`.profile`、`environment.d` 等其他环境文件。
- `mm` 必须明确提示：写入 `~/.bashrc` 不会改变已打开的父 shell，当前终端需重新打开或执行 `source ~/.bashrc` 才能生效。
- `~/.bashrc` 中的内容必须使用 mihomo-manager 专属标记区块幂等、原子地更新；关闭时只删除该区块，不改写用户其他配置。
- 未提供显式 IP/端口时，默认值取当前活动 mihomo 档案的 listener；用户显式提供的只有合法 IPv4/IPv6 地址和端口，由程序按入口自动生成协议。
- 对不匹配 mihomo listener 的明确 IP/端口仍保留 mismatched 诊断和警告，而不在写入前强制禁止。
- GNOME 启用前保存的原始快照在同一层重复设置时保留，直到成功恢复或用户明确清理；不因每次更换地址而覆盖原始快照。
- GNOME 快照、当前 manager 预期值和版本元数据必须保存在 manager 状态目录的受权限文件中，不写入 SQLite 业务表或普通输出。
- 没有有效 GNOME 快照时，`system disable/restore` 不得猜测原设置或直接写入 `mode none`；应返回“无可恢复快照”的结构化错误。
- 若 GNOME HTTP 代理现有 `use-authentication=true`，首版拒绝接管并返回 `PROXY_AUTH_UNSUPPORTED`，不读取、复制或修改认证密码。
- 用户只输入 IP 和 1-65535 端口；GNOME 存储 host/port，Bash 环境变量由程序生成 `http://host:port` 或 `socks5://host:port`，不接受包含 userinfo、path、query 或 fragment 的 URL 输入。首版仅接受合法 IPv4/IPv6 地址，不接受域名。
- `all` 只改写调用命令所属层的所有协议；GNOME 与 Bash 两层之间不共享持久化状态。
- CLI 按层提供 `mm proxy system ...` 与 `mm proxy env ...`；每层支持 HTTP、HTTPS、SOCKS 单项和 `all`，并提供状态与关闭/恢复。
- CLI 命令草案为：`mm proxy system status|set <http|https|socks|all> [ip port]|restore`，以及 `mm proxy env status|set <http|https|socks|all> [ip port]|disable`；`set` 无参数时使用 listener 默认值，仅 `all` 与单项都可以显式传入 IP/端口。
- 单项设置只修改指定协议的当前值，GNOME 以整层原始快照作为最终恢复边界，不提供单协议关闭。
- 代理设置操作不自动启动、停止或重启 mihomo Core；Core 停止时仍可从已保存的配置 listener 推导默认值，并在结果中标明待下次启动生效。
- 每层的 `set all` 不带参数时，HTTP/HTTPS/SOCKS 分别从当前活动 mihomo listener 推导，可以使用不同端口；`set all <ip> <port>` 时三类入口共用同一 IP/端口，由程序分别生成 HTTP 与 SOCKS5 协议值。
- Bash 标记区块同时写入大写与小写的 HTTP/HTTPS/ALL proxy 变量，确保不同命令行程序的环境变量兼容性。
- Bash 标记区块同时写入 `NO_PROXY` 与 `no_proxy`，至少排除 `localhost` 和 `127.0.0.1`；GNOME 设置时在保留用户现有 `ignore-hosts` 的基础上补齐同等的本机绕过项。
- 默认本机绕过集合为 `localhost`、`127.0.0.1`、`::1`，同时覆盖 IPv4 与 IPv6 loopback。
- Bash 启用时将用户已有 `NO_PROXY/no_proxy` 与三个本机值合并并去重，不覆盖企业域名或内网规则。
- GNOME 恢复时还原启用前的 `ignore-hosts`；Bash 关闭时只删除 manager 标记区块中的 `NO_PROXY`/`no_proxy`，不删除区块外的用户配置。
- TUI 在配置管理中提供 GNOME 代理与 Bash 环境代理分层入口，可选择单项、`all`、设置地址、关闭和恢复，所有写操作经注入的 daemon capability 执行。
- TUI 在“配置管理”下增加“GNOME 系统代理”和“Bash 环境代理”两个分层页面；页面提供状态、HTTP/HTTPS/SOCKS/all 选择、IP 输入、端口输入、应用和整层恢复/禁用。
- TUI 的所有代理设置、恢复和禁用写操作必须在执行前显示目标层、影响的协议、IP/端口和恢复边界，由用户明确确认后才调用 capability。
- 状态 DTO 或普通输出只包含规范化的协议、IP 和端口。
- 设置与恢复操作需提供明确的 table/json 结果、结构化错误和稳定退出码。

## Acceptance Criteria

- [ ] 用户可查看 GNOME 与环境代理当前状态，并区分 matched、mismatched、disabled 和 unknown。
- [ ] 用户可对 HTTP、HTTPS、SOCKS 单独配置，也可使用一个“all”操作配置全部支持入口。
- [ ] `mm` 可在 `~/.bashrc` 中持久化代理环境变量，重复设置不会累加重复内容，且不覆盖用户的其他 shell 配置。
- [ ] Bash 标记区块中的 HTTP、HTTPS、ALL 变量同时包含大写和小写形式，对应值保持一致。
- [ ] Bash 的 `NO_PROXY`/`no_proxy` 与 GNOME `ignore-hosts` 都保证本机访问不进入代理，且不丢失用户现有 GNOME 绕过项。
- [ ] Bash 启用代理时不丢失用户已有 `NO_PROXY/no_proxy` 规则，并保证 `localhost`、`127.0.0.1`、`::1` 始终在绕过集合中。
- [ ] 设置完成后的输出明确说明生效范围，并给出当前 Bash 终端的生效方式。
- [ ] 未提供 IP/端口时能从活动 mihomo listener 推导默认 HTTP/HTTPS/SOCKS 地址；显式 IP/端口可覆盖并通过输入校验。
- [ ] 状态、表格输出、TUI 和错误均不显示明文凭据；首版不接受或写入 userinfo。
- [ ] GNOME 设置失败时返回可操作的错误，且不遗留静默的部分成功状态。
- [ ] GNOME HTTP 代理已开启认证时，任何写操作在没有副作用前返回 `PROXY_AUTH_UNSUPPORTED`，不读取或持久化认证凭据。
- [ ] GNOME 没有有效快照时，关闭/恢复操作拒绝并返回可操作错误，不覆盖用户现有系统代理。
- [ ] GNOME 恢复前若发现当前设置与 manager 最近预期值不一致，返回冲突错误并保留现地配置，不覆盖外部修改。
- [ ] 关闭或恢复后的行为与最终确认的所有权模型一致，并有回归测试。
- [ ] 代理设置操作在 Core stopped 时不启动 Core，可使用已验证配置推导默认端口并明确生效时机。
- [ ] CLI 按稳定命令树提供 system/env 两层的 status、set、restore/disable，并遵循现有 table/json 输出和退出码契约。
- [ ] `~/.bashrc` 的 manager 标记区块重复、手改或结构损坏时，`set/disable` 拒绝并保留原文件，不强制重建或删除。
- [ ] TUI 在配置管理中提供 GNOME 与 Bash 两个分层入口，操作经 daemon capability 完成，并显示设置成功、待生效、冲突、无快照和失败状态。
- [ ] TUI 在代理写操作前要求明确确认，取消确认不产生任何 gsettings 或 `.bashrc` 副作用。
- [ ] 重复启用或更换代理地址不会覆盖首次启用时的 GNOME 快照；最终关闭可回到该快照。
- [ ] CLI 与 TUI 的设置、状态、关闭和恢复操作使用同一个 daemon capability 契约，不在 TUI `Update`/`View` 中直接执行 `gsettings` 或写 shell 文件。
- [ ] 无 GNOME/gsettings/DBus 会话的环境不会误报成功或修改其他桌面配置。
- [ ] table/json/TUI 不泄漏代理凭据或 URL 细节。

## Out of Scope (provisional)

- KDE 或其他桌面环境适配器。
- PAC 生成与托管。
- 探测、停止或重配置 xray/v2rayN 等其他代理进程。
- 为已经启动的任意第三方进程注入新环境变量。

## Notes

- 本任务属于跨 platform/daemon/app/CLI/TUI 的复杂功能，规划完成前需新增 `design.md` 与 `implement.md`。
