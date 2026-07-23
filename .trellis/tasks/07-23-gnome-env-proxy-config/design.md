# 技术设计：GNOME 与 Bash 代理配置

## 1. 边界与目标

新增一条由 daemon 统一拥有写权限的代理配置能力。CLI 和 TUI 只调用 `internal/app.CapabilityAPI`，不直接执行 `gsettings`、读取/写入 `~/.bashrc` 或读取 SQLite。能力分为两个相互独立的层：

- `system`：GNOME `org.gnome.system.proxy` 及 HTTP/HTTPS/SOCKS schema。
- `env`：用户 `~/.bashrc` 中唯一的 mihomo-manager 标记区块。

两层共享当前 legacy profile 的 listener 默认值，但不共享快照或启用状态。设置代理不启动、停止或重启 Core。

## 2. 数据流与 IPC

CLI/TUI -> `app.CapabilityAPI` -> Unix IPC -> daemon `CapabilityService` -> `platform.ProxyConfigurator` -> gsettings/文件。

新增路由：

- `GET /v1/proxy/system`
- `PUT /v1/proxy/system`
- `GET /v1/proxy/env`
- `PUT /v1/proxy/env`

PUT 必须携带 `MM-Request-ID`，请求体为 `{profileId?, action, target?, host?, port?}`。`action` 为 `set` 或层专属的 `restore`/`disable`；`target` 为 `http|https|socks|all`。写响应使用 `ProxyConfigStatus`，接入现有 request cache；流式能力不涉及本功能。

`ProxyConfigStatus` 只包含层、配置状态、managed 状态、快照是否存在、是否待下次 Core 启动、各协议规范化 endpoint、warnings 和 operation 元数据。endpoint 只保留 protocol/host/port，不保留 URL、userinfo 或 shell 原文。

## 3. 默认端点与输入

`internal/legacy.Compatibility.ListenerPorts` 是 listener 事实来源。选择规则：

- HTTP/HTTPS：`mixed-port` > `port`，只选启用且有合法 host 的端口。
- SOCKS：`socks-port` > `mixed-port`。
- 无可用默认端口时，无参数 `set` 返回 `INVALID_REQUEST`，不会启动 Core。
- 显式参数必须同时出现，host 只接受合法 IPv4/IPv6 字面量，port 为 1-65535。
- `all` 无参数逐协议推导；`all host port` 三协议共用 host/port。
- GNOME HTTP/HTTPS 写 host/port；Bash HTTP/HTTPS 生成 `http://host:port`，SOCKS/ALL_PROXY 生成 `socks5://host:port`。IPv6 URL 使用 `net.JoinHostPort`。

## 4. GNOME 适配器

在 `internal/platform` 新增可注入 runner 的 `GNOMEProxyConfigurator`，复用现有 `GNOMEProxyInspector` 的 `gsettings` 调用边界，但写操作单独封装：

1. 读取并规范化 manager 会修改的快照：根 schema 的 `mode`、`use-same-proxy`、`ignore-hosts`，HTTP/HTTPS/SOCKS 的 `host`/`port`。仅读取 HTTP 的 `use-authentication`；为 `true` 时在任何写入前返回 `PROXY_AUTH_UNSUPPORTED`，不读取用户名或密码。自动配置、FTP 和认证键保持不变，无需写入快照。
2. 首次 set 前保存快照；后续 set 只更新预期值，不覆盖快照。
3. 修改目标协议和 `ignore-hosts`，将 mode 设置为 `manual` 并将 `use-same-proxy` 设为 `false`。单项 set 保留其他协议当前值；all 按规则更新三项。
4. 每次写入后重新 `gsettings get`，核对所有目标键。任一写入、核对或快照持久化失败，按逆序恢复本次已改键，并返回失败；恢复失败返回 `RESTORE_FAILED`。
5. restore 前重新读取当前键，与持久化的 manager expected 快照比较。发现外部改动返回 `CONFLICT`，不覆盖；一致时按原始 GVariant 文本逆序恢复全部快照键，并在成功后清除快照/expected。
6. 无快照的 restore 明确返回 `PROXY_SNAPSHOT_NOT_FOUND`，不写 `mode none`。

`ignore-hosts` 解析为字符串集合，仅在启用时并集加入 `localhost`、`127.0.0.1`、`::1`；保留原有顺序和其他值，恢复使用原始快照。GVariant 字符串和字符串数组使用受限解析器，不调用 shell，不拼接未校验输入。

## 5. Bash 适配器

在 `internal/platform` 新增 `BashProxyConfigurator`：

- 路径由 `config.ManagerPaths.Bashrc` 注入，默认 `$HOME/.bashrc`；状态由 `ManagerPaths.ProxyState` 注入，避免业务包拼接 HOME。
- 标记为：`# >>> mihomo-manager proxy >>>` 与 `# <<< mihomo-manager proxy <<<`。文件中必须恰好零或一组；重复、缺少一端、内容 hash 与 state 不符均为 `CONFLICT`。
- 写入前使用同目录 `0600` 临时文件、保留原权限/拥有者、fsync 后 rename；目录不存在或权限不安全返回 `PERMISSION_DENIED`。
- 区块写入六个大小写 proxy 变量和两个大小写 `NO_PROXY` 变量（大写、小写各一份）。代理值由已校验 host/port 生成；NO_PROXY 通过 shell `case` 守卫在 source 时合并已有值并去重，再保证三个 loopback 值存在，避免覆盖用户 `.bashrc` 中的企业/内网规则。
- set 是幂等替换区块；disable 校验区块后原子删除。没有区块的 disable 是成功的 no-op；冲突时拒绝。
- 结果明确提示新 shell 或 `source ~/.bashrc` 才生效；daemon 不尝试改变调用 CLI 的父 shell。

## 6. 状态文件与事务

扩展 `config.ManagerPaths`：`ProxyState` 位于 `${XDG_STATE_HOME:-~/.local/state}/mihomo-manager/proxy.json`，`Bashrc` 为 `$HOME/.bashrc`。状态文件目录 `0700`、文件 `0600`，JSON 原子发布，包含版本、GNOME snapshot/expected、Bash block hash、更新时间；不进 SQLite。

daemon 新增 `ProxyService`，复用现有 `Coordinator`，写操作按 `proxy.system`/`proxy.env` 加锁。状态读取失败、摘要冲突、外部修改和状态文件损坏均不做部分写入。GNOME 多键写入采用“读取快照 -> 写入 -> 核对 -> 持久化 expected”的事务；Bash 采用“读取/校验 -> 临时文件 -> rename -> 持久化 hash”。持久化 state 失败时回滚已发布的 GNOME/file 内容。

## 7. CLI 与 TUI

`internal/cli/proxy.go` 注册：

```text
mm proxy system status|set <http|https|socks|all> [ip port]|restore
mm proxy env status|set <http|https|socks|all> [ip port]|disable
```

写命令通过 capability 生成 request ID；table/json 使用现有 presenter，错误映射新增 `PROXY_*` code 和既有退出码分类。CLI 不做确认提示。

`internal/tui` 在“配置管理”增加两个分层页面。页面状态只保存 typed status、选中的 target、IP/port 输入和异步 busy/error；Enter 在任何写操作前进入确认页，确认后发 `tea.Cmd` 调用 capability，View 不执行副作用。展示 success/nextStart/conflict/no snapshot/failed，并保留 Esc 零副作用。

## 8. 兼容与安全

- 只支持 GNOME gsettings 和 Bash `.bashrc`；不修改 zsh/profile/environment.d/KDE/PAC。
- 不写入 userinfo，不支持域名和 URL 输入，不记录代理原文或凭据。
- daemon 无 DBus/GNOME 会话、gsettings 缺失或非 GNOME 时返回结构化失败；只读 status 可返回 unknown，不把诊断失败伪报成功。
- 不探测/停止 xray、v2rayN 或任意端口占用进程，不自动启动 Core。
