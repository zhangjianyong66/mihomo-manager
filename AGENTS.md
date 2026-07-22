# 项目说明

- 本项目是 Go CLI 项目，模块名为 `github.com/zhangjianyong66/mihomo-manager`。
- 当前开发机为 Ubuntu 26.04 LTS x86_64。
- Go 版本要求见 `go.mod`：`go 1.22`；当前本机用户目录安装了 Go `1.26.4`，入口为 `~/.local/bin/go`。
- 一键安装首版支持 Ubuntu/Debian amd64/arm64；远程入口为 `curl -fsSL https://raw.githubusercontent.com/zhangjianyong66/mihomo-manager/master/scripts/bootstrap.sh | bash`，本地入口为 `make install`。
- 安装器需以普通用户运行，只在安装缺失 apt 包时局部使用 sudo；支持 `--yes`/`MM_ASSUME_YES=1` 无交互确认。
- 安装产物是独立的 `~/.local/bin/mm` 普通文件，不再软链接仓库 `bin/mm`；移动或删除仓库不会影响已安装命令。
- 安装器会自动安装并校验 mihomo core，默认固定 `v1.19.28`，支持 `MIHOMO_VERSION` 覆盖；默认 core 路径为 `~/.local/bin/mihomo`，可用 `MIHOMO_BIN` 覆盖。
- 安装器会把固定 commit `32ae0e8658ca541374b721efcee84955e8a59755` 的 CN domain/IP `.mrs` 安装到 `<CONFIG_DIR>/rulesets/{cn-domain,cn-ip}.mrs`，目录/文件权限为 `0700/0600`；支持 `MM_RULESET_BASE_URL`、`MM_RULESET_REF` 覆盖，但必须成对提供 `MM_RULESET_DOMAIN_SHA256`、`MM_RULESET_IP_SHA256`。
- 规则集首次无有效缓存失败时安装失败；升级失败只复用与 install-state 路径、摘要和 mihomo 格式校验一致的旧缓存，两份资产成对原子发布并在第二份失败时恢复旧版本。
- 系统 Go 低于 1.22 或缺失时，安装器会把官方 Go 1.26.4 安装到 `~/.local/share/mihomo-manager/toolchains/go1.26.4`，不替换系统 Go。
- 当前本机已安装 MetaCubeX/mihomo `v1.19.28` Linux amd64 v1 构建到 `~/.local/bin/mihomo`。
- 运行命令：`mm` 或 `mm tui` 打开相同 TUI；已提供 `mode`、`core`、`config`、`group`、`node`、legacy `subscription`、`route` 业务命令，可用 `mm --help` 和各二级 `--help` 做非交互冒烟验证。
- CLI 命令工厂、table/json 输出、结构化错误、退出码和秘密值位于 `internal/cli`；`cmd/mm` 只做真实依赖/IO 装配和进程退出。
- 稳定 ID、`RoutingMode(global|rule|direct)`、mihomo core、档案、订阅、节点、操作和设置领域模型位于 `internal/domain`；按 core/profile/node/group/subscription/route/mode/connection/config/log 拆分的应用 ports 位于 `internal/app`，CLI 与 TUI 共用同一个 daemon-backed `CapabilityAPI`。
- `internal/store` 使用 `database/sql` 与固定的 `modernc.org/sqlite v1.36.1`（无 CGO），提供 profile/subscription/node/operation/settings/legacy migration 仓储和三条嵌入式迁移；store 只接收显式数据库路径，默认用户路径由 daemon/config 装配。
- `internal/core` 定义类型化 adapter/process/runtime 契约并负责 generation 发布；managed generation 位于 `${XDG_DATA_HOME:-~/.local/share}/mihomo-manager/generations`，core 日志和 runtime metadata 位于 `${XDG_STATE_HOME:-~/.local/state}/mihomo-manager/core`，目录/文件权限为 `0700/0600`。
- 2.x mihomo adapter 位于 `internal/mihomo/adapter.go`、`render.go`、`validation.go`、`process.go`、`runtime.go`；只接受 loopback external-controller，原生验证参数为 `-t -d <dir> -f <file>`，Linux 进程使用 `Setsid`，停止只作用于 daemon 持有的精确进程句柄。
- daemon 托管的每次 core 启动都会在创建进程前检查 `mixed-port`、`port`、`socks-port`、`redir-port`、`tproxy-port` 和 `external-controller`；mixed/socks/tproxy 检查 TCP+UDP，HTTP/redir/controller 检查 TCP，冲突一次返回全部字段/网络/主机/端口并以 `PORT_CONFLICT` 阻止启动，不识别或停止占用进程。
- external/legacy 配置在 2.x adapter 中只读，validate 与 start 前核对 SHA-256；managed 配置写入独立 generation，绝不覆盖 `~/.config/mihomo/config.yaml`。
- SQLite store 使用单连接、rollback journal、`foreign_keys=ON`、`synchronous=FULL`；状态目录/数据库权限分别收紧为 `0700`/`0600`，迁移历史以 SHA-256 防改写，已有 schema 升级前创建同目录恢复点。
- CLI 退出码契约为：`1` 内部错误、`2` 输入错误、`3` 不存在、`4` 冲突、`5` daemon/协议不可用、`6` 校验失败、`7` 权限拒绝、`8` 上游失败；JSON API 版本为 `mm/v1`。
- A6/M5 业务 CLI 默认解析唯一活动 legacy profile，可用 `--profile` 显式指定；查询支持 table/json，`node test`、`core logs --follow` 与 `route connections --follow` 支持 text/NDJSON，订阅 URL、节点 URI、UUID、密码和日志凭据默认脱敏。
- A6/M5 daemon 路由位于 `/v1/core/*`、`/v1/mode`、`/v1/config/*`、`/v1/groups*`、`/v1/nodes*`、`/v1/subscription`、`/v1/routes/*`、`/v1/logs*`、`/v1/connections*`；CLI/TUI 不可用 daemon 时不会回退到旧的 `pgrep/pkill`、配置直写或 mihomo API 直连。
- `/v1/config/ports` 提供六类监听端口 typed GET/PUT；CLI 为 `mm config ports` 和 `mm config port set <field> <port>`，TUI 入口为“配置管理 > 监听端口”。五类代理端口允许 `0` 禁用，controller 必须 `1-65535` 且保留 loopback host；running 修改经 CoreManager 受控重启并可恢复，stopped 只保存到下次启动。
- CLI/TUI 配置编辑都在客户端本地以 `0600` 临时文件启动 `EDITOR`，再携带 expected SHA-256 回传 daemon；daemon 核对摘要、原子写入并验证，systemd daemon 不直接占用终端。
- legacy capability 从活动配置动态读取 `external-controller`，不把 systemd 中初始 `MIHOMO_API_PORT` 当作端口修改后的运行时事实来源。
- daemon 前台入口为 `mm daemon run`，状态/控制入口为 `mm daemon status|start|stop|restart|enable|disable`；默认使用 XDG 下的 `~/.local/share/mihomo-manager/state.db`、`~/.local/state/mihomo-manager/run/mm.sock`，有 `XDG_RUNTIME_DIR` 时运行目录改为 `$XDG_RUNTIME_DIR/mihomo-manager`。
- `mm daemon restart` 与 TUI“重启全部”由客户端侧 `app.DaemonService` 编排，只支持摘要有效且 service/socket 均 active 的 systemd user `mm.service/mm.socket`；按 running/stopped/degraded/failed 状态停止并复核 Core、只重启 `mm.service`、验证新 PID/startedAt 与协议握手后恢复 Core，失败执行有界尽力恢复。前台 daemon、过渡态 Core 或非受管 unit 均在副作用前拒绝。
- TUI“服务管理”明确区分“重启 Core”和“重启全部”；组合重启确认后 Esc/q/Ctrl+C 不会取消，执行期间代理短暂中断且 Core 内存中的测速 history、实时连接不保留。此手动能力不改变安装器仅在 Core 明确 stopped 时自动重启 daemon 的无人值守策略。
- 一键安装会通过正式 CLI 安装/启用/启动 manager daemon，但保持 core stopped；仅本次新建配置自动 `migrate apply`，已有配置只提示显式迁移。升级只有在 core 明确为 stopped 且 stop 前复核仍为 stopped 时才重启 daemon，其他状态均保持现有进程。
- daemon 启动会装配 CoreManager，但 core 初始状态始终为 `stopped`，不会自动启动代理；后续显式切换使用 operation 阶段记录，失败时恢复旧 RuntimeSpec，恢复失败进入明确 `failed`。
- M3 模式事务仅支持活动 legacy profile；`GET/PUT /v1/mode` 返回配置/runtime 模式、core、有效组/节点、规则集、连接数和 warnings。PUT 必须带 `MM-Request-ID`，core stopped 时只保存已验证配置并报告下次启动生效，不启动或探测 controller。
- 模式、core、subscription、config 和 route 写操作共享 daemon Coordinator；运行中模式发布失败会恢复旧配置、权限、expected 摘要和 runtime mode，恢复失败返回 `RESTORE_FAILED` 并标记 core failed。默认不关闭连接，显式关闭失败不回滚已生效模式。
- M4 提供 `mm mode status` 与 `mm mode set global|rule|direct [--close-connections]` 的 table/json 输出；TUI 增加“运行模式”页面，显示配置/runtime 模式、有效路径、规则集、连接数和 warnings。TUI 的 core/group/node/subscription/route/config/log 写操作均经注入的 daemon capability，Bubble Tea `Update`/`View` 不直接执行业务副作用。
- M5 提供 `mm route connections [--follow]` 和 TUI“实时连接”页；mihomo `/connections` 在 `internal/mihomo` 一次性类型化，`connections:null` 视为空列表，单响应仍限 1 MiB，活动/follow map 最多 4096 条。follow 每秒轮询并输出 `open|update|closed` 与 NDJSON terminal event，连接详情和短关闭提示仅驻留有界内存，不写 SQLite 或持久日志。
- M5 的 mode 状态还展示配置中的 mixed/http/socks listener、daemon 只读采集的 GNOME system proxy，以及 CLI/TUI 进程本地的 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`。只按协议、等价 loopback 和端口匹配；userinfo/path/query 在进入 DTO 前丢弃，检测只调用 `gsettings get`，不会设置系统代理、探测/停止 xray 或占用端口。
- daemon 只监听 Unix socket，不监听 TCP；socket 父目录为 `0700`、socket/锁为 `0600`，Linux 通过 `SO_PEERCRED` 限制为当前 UID，root daemon 被拒绝。IPC 使用 `/v1/`、`MM-Protocol-Min/Max` 和 `MM-Request-ID`，流式扩展采用 NDJSON。
- A5 legacy 恢复点位于 `${XDG_DATA_HOME:-~/.local/share}/mihomo-manager/backups/<restore-point-id>/files`，目录/快照文件权限为 `0700/0600`；`migrate rollback` 必须显式指定 `--restore-point`，daemon 会核对 expected SHA-256 后才恢复。
- legacy 文件发现会合并固定允许列表与动态 `config.yaml.*.bak` 候选，按相对路径统一去重并稳定排序；标准备份和时间戳备份必须分别且仅纳入一次。
- systemd user unit 模板位于 `internal/platform/systemd/units`；`mm daemon enable` 在无 systemd 用户会话时只安装并报告“已安装未启用”，不会启用 linger、sudo 或启动 mihomo core。
- systemd user unit 模板版本为 2；安装器把 `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT` 作为受校验的 `Environment=` 写入 service，未显式带环境的后续 `mm daemon enable` 会保留已有受管环境块，保证 daemon 重启后继续使用同一 legacy 路径。
- 测试命令：`go test ./...`；存储/领域变更还需执行 `GOTOOLCHAIN=go1.22.12 go test -race ./...`、`go vet ./...` 和 Linux amd64/arm64 的 `CGO_ENABLED=0` 构建；安装流程测试为 `bash scripts/tests/test_install.sh`；Shell 语法检查为 `bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh`。
- Go 版路径和端口可通过环境变量覆盖：`CONFIG_DIR` 修改配置目录，`MIHOMO_API_PORT` 修改 external-controller 端口，`EDITOR` 修改配置编辑器；`MIHOMO_BIN` 修改 core 路径。
- `tests/test.sh` 与 `scripts/tests/test_manager.sh` 面向旧非交互式 Shell 实现或依赖本机运行状态，不作为当前 Go TUI 的默认验收命令。
- 安装脚本会创建 `~/.config/mihomo`，仅在 `config.yaml` 缺失时生成最小 `DIRECT` 配置并校验，已有配置不会覆盖。当前本机 `config.yaml` 是包含 256 个代理的订阅配置。
- 新建最小配置和旧 Shell 订阅补全的默认监听端口为 `mixed-port: 7890`、`socks-port: 7891`；兼容层可用 `MIHOMO_MIXED_PORT`、`MIHOMO_SOCKS_PORT` 覆盖，安装器不会自动改写已有配置。
- 配置测试命令：`~/.local/bin/mihomo -t -d ~/.config/mihomo -f ~/.config/mihomo/config.yaml`。当前配置已修复 10 组重复 VLESS 节点名并通过校验，文件权限为 `600`；修复前备份为 `~/.config/mihomo/config.yaml.20260714_214929.bak`。
- Go 版订阅更新逻辑支持完整 YAML 配置，也支持纯文本或 base64 编码的节点 URI 列表；当前覆盖 `vless://`、`vmess://`、`trojan://`、`ss://`。
- Rule 路由与 DNS 的唯一合成器位于 `internal/mihomo/routing_policy.go`：固定顺序为本机/局域网、自定义规则、白名单、CN domain/IP `.mrs` providers、唯一 `MATCH,🌐 代理`；兼容 `ApplyRouteCN()`、白名单和订阅更新均复用该实现。
- Rule DNS 固定 `respect-rules`、国内 bootstrap/代理服务器直连解析、境外加密解析及 CN provider 的国内 policy；保留 fake-IP、IPv6 和不冲突的用户 nameserver policy，已通过 mihomo v1.19.28 原生验证。
- 订阅更新只替换节点和两个 manager 组候选，保留 mode、本地端口、custom rules/providers、DNS 与白名单；运行中会分别恢复 `GLOBAL`、`🌐 代理`选择，节点消失时按名称排序回退并产生 typed warning。
- 当前本机 manager daemon 已由 systemd user socket/service 启用并运行，schema v3；`~/.config/mihomo/rulesets` 已安装固定版本两份规则集并通过摘要/权限校验。
- 当前本机 legacy 配置已成功迁移为活动档案 `legacy-mihomo`，恢复点为 `legacy-1784708196601152953`；真实配置已通过 `mm config port set mixed-port 7890` 从 `10808` 切换到 `7890`，权限为 `0600` 且通过 mihomo v1.19.28 原生校验。
- 当前本机 mihomo core 为 `running`，监听 `127.0.0.1:7890` TCP+UDP、`127.0.0.1:7891` TCP+UDP 和 `127.0.0.1:9090` TCP；xray PID 689289 继续独占 `10808`，未被 manager 停止或修改。
- 已在真实 running core 上尝试把 mixed 临时改回 `10808` 验证冲突事务：CLI 返回 `PORT_CONFLICT`/退出码 4，配置摘要恢复为 `b31003f69ee4111772d78acc945fcc4632c27b6195171c6a3cb37b6fcd8509b1`，旧配置/core 成功恢复并保留最近冲突详情。
- 当前 GNOME 系统代理地址已配置为 HTTP/HTTPS/SOCKS 均指向 `127.0.0.1:10808`，且 `org.gnome.system.proxy mode` 为 `manual`。由于当前监听该端口的是 xray，Chrome 等遵循系统代理的应用目前会走 xray；本地忽略地址为 `localhost`、`127.0.0.0/8`、`::1`。
- `~/.config/mihomo/config.yaml` 与 mihomo runtime 当前均为 `mode: rule`，`mm-cn-domain`/`mm-cn-ip` 已加载，活动连接为 0；GNOME 与当前 CLI 代理环境仍指向 xray 的 `127.0.0.1:10808`，未随 mihomo mixed 端口修改。
- mihomo 的 `global` 模式会绕过 `rules`；当前本机配置仍是迁移前的 `rule + MATCH,GLOBAL` 形态，M2 只交付合成器并未自动改写真实用户配置，后续显式模式事务由 M3 接入。
- Go 版服务启动逻辑位于 `internal/mihomo/client.go`，启动 mihomo 时会设置新 session，避免父进程退出时清理 mihomo 子进程。
- 仓库仍包含旧 macOS `launchd` 配置，但首版一键安装不支持 macOS，也不会安装 LaunchAgent；卸载器仅保留旧 plist 的兼容清理。
- 卸载命令：`make uninstall`，默认删除 `mm`、隔离 Go 和安装器 PATH 块，保留 core 与配置；`scripts/uninstall.sh --purge` 删除可确认归属的 core，`--purge-config` 显式删除配置。
- 安装状态记录在 `~/.local/share/mihomo-manager/install-state`，只保存路径、版本和归属等非敏感信息。
- 远程安装默认使用 `master`，可用 `MM_REF` 固定源码引用；下载源可通过 `MM_GITHUB_BASE_URL`、`MM_GITHUB_API_BASE_URL`、`MM_GO_DOWNLOAD_BASE_URL` 显式覆盖，脚本不会自动切换第三方镜像。
- 节点列表从 mihomo `/proxies` 的运行时 `history` 读取最新测速状态，不写 SQLite；进入列表和节点选择成功后都不自动测速。TUI 空闲时用 `t` 单测光标节点，单测中 `t` 将其他节点有序去重入串行队列，批测中 `t` 抢占为光标单测；任意状态按 `a` 清队列并以并发 5、`limit=0` 重启整组批测，且在首个结果前按已加载的 `Testable` 状态立即显示 `0/N`，后续以 stream 进度为准。测速中 Esc 清队列、取消并保留部分结果，空闲 Esc 才返回；每项新流使用 generation 隔离迟到事件。
- 单节点测速固定使用 `/v1/nodes/test-single`；`mm node test --node <id> [--group <group>]` 不得回退到旧 `/v1/nodes/test` 批量路由。节点测速 NDJSON 追加 `status` 与 `testedAt`，新客户端仍兼容旧事件缺少这两个字段。

# 协作约定

- 永远使用中文回答问题，除非用户明确要求英文。
- 永远使用中文制定计划方案，写入计划方案。
- git commit 的 message 使用中文描述。
- 处理项目时，如果发现新的目录结构、环境配置、部署方式、运行约定或下次可复用信息，应及时更新本文件。
<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->
