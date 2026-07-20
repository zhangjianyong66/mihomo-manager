# 重要项目约定

## 产品与兼容边界

- 当前产品是 Go 实现的交互式 TUI，无参数 `mm` 与显式 `mm tui` 打开相同界面。Go CLI 已有命令/JSON/错误/退出码基础契约，但业务子命令尚未迁移；旧的非交互式子命令仍不支持。
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
| 订阅地址 | `<CONFIG_DIR>/subscription.url` | 随配置目录变化 |
| 白名单 | `<CONFIG_DIR>/whitelist.yaml` | 随配置目录变化 |
| mihomo 日志 | `<CONFIG_DIR>/mihomo.log` | 随配置目录变化 |
| external-controller | `http://127.0.0.1:9090` | `MIHOMO_API_PORT` 仅覆盖端口 |

配置和订阅文件可能包含节点地址、密码或订阅凭据，不得写入测试夹具之外的仓库文件、日志或文档示例。

## 路由规则语义

- 白名单域名始终生成 `DOMAIN-SUFFIX,<domain>,DIRECT`，并排在最终兜底规则之前。
- mihomo 的 `global` 模式会绕过 `rules`。需要“白名单直连、其他走 GLOBAL”时，运行态必须是 `rule`，规则末尾使用 `MATCH,GLOBAL`。
- `ApplyRouteCN()` 生成的托管规则为 `GEOSITE,CN,DIRECT`、`GEOIP,CN,DIRECT,no-resolve`、`MATCH,GLOBAL`，写入后必须通过 mihomo 配置测试，否则恢复备份。
- 路由或白名单变更必须保持唯一的最终 `MATCH` 规则，并清理旧的冲突规则；回归测试在 `internal/mihomo/client_route_test.go`。

## 订阅更新

- `UpdateSubscription()` 支持完整 YAML，以及纯文本或 base64 编码的 URI 列表；Go 版当前解析 `vless://`、`vmess://`、`trojan://`、`ss://`。
- 更新前读取旧配置和白名单并备份主配置；下载默认绕过系统代理，最多重试 3 次。
- 订阅内容不能覆盖本地 `mixed-port`、`socks-port` 和 `external-controller`；缺失时分别使用 `7890`、`7891`、`127.0.0.1:9090`。
- 默认订阅更新不引入 Geo 规则，避免热重载时阻塞 Geo 数据下载；它创建 `🌐 代理`/`🎯 直连` 分组并以 `MATCH,🌐 代理` 兜底。
- YAML 重写可能改变字段顺序并丢失注释，这是当前实现的已知行为。

## 白名单

- `whitelist.yaml` 是白名单的持久化来源，结构为 `domains: []`。
- 如果白名单文件不存在，会从旧 `config.yaml` 中的直连域名规则迁移并写入新文件。
- 域名写入前需要去协议、路径、通配前缀，转为小写、去重并排序；`*.example.com` 最终按 `example.com` 存储。

## 系统与运行约定

- manager daemon 的默认 data/state/runtime/socket/lock/systemd user unit 路径由 `config.ResolveManagerPaths` 集中计算，优先使用 XDG 环境变量；测试通过 `ManagerEnvironment` 注入临时绝对路径。
- daemon 是 manager 状态写入唯一所有者，IPC 仅限同 UID Unix socket；无 daemon 时 CLI/TUI 不得回退直接写 SQLite、配置或控制 core。A3 只提供 health/status，不伪造 core 运行态。

- 服务状态和停止逻辑通过进程模式 `mihomo.*-f.*config.yaml` 查找 core，改动命令行参数时要同步评估进程检测。
- external-controller 默认只监听 `127.0.0.1`；不要在没有明确需求和安全评估时扩大到公网地址。
- 运行日志写入 `<CONFIG_DIR>/mihomo.log`；TUI 日志页只保留最近 500 行内存缓冲，并支持正则过滤。
- 配置管理、订阅更新和路由功能涉及用户真实网络环境；自动化测试必须隔离到临时目录，不能改写用户的 `~/.config/mihomo`。
