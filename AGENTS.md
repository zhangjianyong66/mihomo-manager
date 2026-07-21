# 项目说明

- 本项目是 Go CLI 项目，模块名为 `github.com/zhangjianyong66/mihomo-manager`。
- 当前开发机为 Ubuntu 26.04 LTS x86_64。
- Go 版本要求见 `go.mod`：`go 1.22`；当前本机用户目录安装了 Go `1.26.4`，入口为 `~/.local/bin/go`。
- 一键安装首版支持 Ubuntu/Debian amd64/arm64；远程入口为 `curl -fsSL https://raw.githubusercontent.com/zhangjianyong66/mihomo-manager/master/scripts/bootstrap.sh | bash`，本地入口为 `make install`。
- 安装器需以普通用户运行，只在安装缺失 apt 包时局部使用 sudo；支持 `--yes`/`MM_ASSUME_YES=1` 无交互确认。
- 安装产物是独立的 `~/.local/bin/mm` 普通文件，不再软链接仓库 `bin/mm`；移动或删除仓库不会影响已安装命令。
- 安装器会自动安装并校验 mihomo core，默认固定 `v1.19.28`，支持 `MIHOMO_VERSION` 覆盖；默认 core 路径为 `~/.local/bin/mihomo`，可用 `MIHOMO_BIN` 覆盖。
- 系统 Go 低于 1.22 或缺失时，安装器会把官方 Go 1.26.4 安装到 `~/.local/share/mihomo-manager/toolchains/go1.26.4`，不替换系统 Go。
- 当前本机已安装 MetaCubeX/mihomo `v1.19.28` Linux amd64 v1 构建到 `~/.local/bin/mihomo`。
- 运行命令：`mm` 或 `mm tui` 打开相同 TUI；A6 已提供 `core`、`config`、`group`、`node`、legacy `subscription`、`route` 业务命令，可用 `mm --help` 和各二级 `--help` 做非交互冒烟验证。
- CLI 命令工厂、table/json 输出、结构化错误、退出码和秘密值位于 `internal/cli`；`cmd/mm` 只做真实依赖/IO 装配和进程退出。
- 稳定 ID、mihomo core、档案、订阅、节点、操作和设置领域模型位于 `internal/domain`；按 core/profile/node/group/subscription/route/config/log 拆分的应用 ports 位于 `internal/app`，现有 TUI 尚未迁移到这些 ports。
- `internal/store` 使用 `database/sql` 与固定的 `modernc.org/sqlite v1.36.1`（无 CGO），提供 profile/subscription/node/operation/settings/legacy migration 仓储和三条嵌入式迁移；store 只接收显式数据库路径，默认用户路径由 daemon/config 装配。
- `internal/core` 定义类型化 adapter/process/runtime 契约并负责 generation 发布；managed generation 位于 `${XDG_DATA_HOME:-~/.local/share}/mihomo-manager/generations`，core 日志和 runtime metadata 位于 `${XDG_STATE_HOME:-~/.local/state}/mihomo-manager/core`，目录/文件权限为 `0700/0600`。
- 2.x mihomo adapter 位于 `internal/mihomo/adapter.go`、`render.go`、`validation.go`、`process.go`、`runtime.go`；只接受 loopback external-controller，原生验证参数为 `-t -d <dir> -f <file>`，Linux 进程使用 `Setsid`，停止只作用于 daemon 持有的精确进程句柄。
- external/legacy 配置在 2.x adapter 中只读，validate 与 start 前核对 SHA-256；managed 配置写入独立 generation，绝不覆盖 `~/.config/mihomo/config.yaml`。
- SQLite store 使用单连接、rollback journal、`foreign_keys=ON`、`synchronous=FULL`；状态目录/数据库权限分别收紧为 `0700`/`0600`，迁移历史以 SHA-256 防改写，已有 schema 升级前创建同目录恢复点。
- CLI 退出码契约为：`1` 内部错误、`2` 输入错误、`3` 不存在、`4` 冲突、`5` daemon/协议不可用、`6` 校验失败、`7` 权限拒绝、`8` 上游失败；JSON API 版本为 `mm/v1`。
- A6 业务 CLI 默认解析唯一活动 legacy profile，可用 `--profile` 显式指定；查询支持 table/json，`node test` 与 `core logs --follow` 支持 text/NDJSON，订阅 URL、节点 URI、UUID、密码和日志凭据默认脱敏。
- A6 daemon 路由位于 `/v1/core/*`、`/v1/config/*`、`/v1/groups*`、`/v1/nodes*`、`/v1/subscription`、`/v1/routes/*`、`/v1/logs*`；CLI 不可用 daemon 时不会回退到旧的 `pgrep/pkill`、配置直写或 mihomo API 直连。
- `mm config edit` 在 CLI 本地以 `0600` 临时文件启动 `EDITOR`，再携带 expected SHA-256 回传 daemon；daemon 核对摘要、原子写入并验证，systemd daemon 不直接占用终端。
- daemon 前台入口为 `mm daemon run`，状态/控制入口为 `mm daemon status|start|stop|enable|disable`；默认使用 XDG 下的 `~/.local/share/mihomo-manager/state.db`、`~/.local/state/mihomo-manager/run/mm.sock`，有 `XDG_RUNTIME_DIR` 时运行目录改为 `$XDG_RUNTIME_DIR/mihomo-manager`。
- daemon 启动会装配 CoreManager，但 core 初始状态始终为 `stopped`，不会自动启动代理；后续显式切换使用 operation 阶段记录，失败时恢复旧 RuntimeSpec，恢复失败进入明确 `failed`。
- daemon 只监听 Unix socket，不监听 TCP；socket 父目录为 `0700`、socket/锁为 `0600`，Linux 通过 `SO_PEERCRED` 限制为当前 UID，root daemon 被拒绝。IPC 使用 `/v1/`、`MM-Protocol-Min/Max` 和 `MM-Request-ID`，流式扩展采用 NDJSON。
- A5 legacy 恢复点位于 `${XDG_DATA_HOME:-~/.local/share}/mihomo-manager/backups/<restore-point-id>/files`，目录/快照文件权限为 `0700/0600`；`migrate rollback` 必须显式指定 `--restore-point`，daemon 会核对 expected SHA-256 后才恢复。
- systemd user unit 模板位于 `internal/platform/systemd/units`；`mm daemon enable` 在无 systemd 用户会话时只安装并报告“已安装未启用”，不会启用 linger、sudo 或启动 mihomo core。
- 测试命令：`go test ./...`；存储/领域变更还需执行 `GOTOOLCHAIN=go1.22.12 go test -race ./...`、`go vet ./...` 和 Linux amd64/arm64 的 `CGO_ENABLED=0` 构建；安装流程测试为 `bash scripts/tests/test_install.sh`；Shell 语法检查为 `bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh`。
- Go 版路径和端口可通过环境变量覆盖：`CONFIG_DIR` 修改配置目录，`MIHOMO_API_PORT` 修改 external-controller 端口，`EDITOR` 修改配置编辑器；`MIHOMO_BIN` 修改 core 路径。
- `tests/test.sh` 与 `scripts/tests/test_manager.sh` 面向旧非交互式 Shell 实现或依赖本机运行状态，不作为当前 Go TUI 的默认验收命令。
- 安装脚本会创建 `~/.config/mihomo`，仅在 `config.yaml` 缺失时生成最小 `DIRECT` 配置并校验，已有配置不会覆盖。当前本机 `config.yaml` 是包含 256 个代理的订阅配置。
- 配置测试命令：`~/.local/bin/mihomo -t -d ~/.config/mihomo -f ~/.config/mihomo/config.yaml`。当前配置已修复 10 组重复 VLESS 节点名并通过校验，文件权限为 `600`；修复前备份为 `~/.config/mihomo/config.yaml.20260714_214929.bak`。
- Go 版订阅更新逻辑支持完整 YAML 配置，也支持纯文本或 base64 编码的节点 URI 列表；当前覆盖 `vless://`、`vmess://`、`trojan://`、`ss://`。
- 订阅更新默认保留本地端口配置，并生成不依赖 Geo 数据下载的规则：白名单域名直连，其余走 `🌐 代理`；需要大陆直连时再手动执行“配置管理 -> 应用分流规则（大陆直连/其他走GLOBAL）”。
- 当前本机 mihomo 未运行；`10808` 当前由 xray 进程监听，`7891` 与 `9090` 未监听。启动 mihomo 前需先处理 `10808` 端口冲突。
- 当前 GNOME 系统代理地址已配置为 HTTP/HTTPS/SOCKS 均指向 `127.0.0.1:10808`，且 `org.gnome.system.proxy mode` 为 `manual`。由于当前监听该端口的是 xray，Chrome 等遵循系统代理的应用目前会走 xray；本地忽略地址为 `localhost`、`127.0.0.0/8`、`::1`。
- `~/.config/mihomo/config.yaml` 已写入 `mode: rule`；当前 mihomo 未运行，因此不存在可查询的 mihomo 运行态。
- mihomo 的 `global` 模式会绕过 `rules`，因此白名单域名直连规则不会生效；当前采用 `rule` 模式，并在规则末尾保留 `MATCH,GLOBAL`，实现“白名单直连，其余全部走 GLOBAL 分组”。
- Go 版服务启动逻辑位于 `internal/mihomo/client.go`，启动 mihomo 时会设置新 session，避免父进程退出时清理 mihomo 子进程。
- 仓库仍包含旧 macOS `launchd` 配置，但首版一键安装不支持 macOS，也不会安装 LaunchAgent；卸载器仅保留旧 plist 的兼容清理。
- 卸载命令：`make uninstall`，默认删除 `mm`、隔离 Go 和安装器 PATH 块，保留 core 与配置；`scripts/uninstall.sh --purge` 删除可确认归属的 core，`--purge-config` 显式删除配置。
- 安装状态记录在 `~/.local/share/mihomo-manager/install-state`，只保存路径、版本和归属等非敏感信息。
- 远程安装默认使用 `master`，可用 `MM_REF` 固定源码引用；下载源可通过 `MM_GITHUB_BASE_URL`、`MM_GITHUB_API_BASE_URL`、`MM_GO_DOWNLOAD_BASE_URL` 显式覆盖，脚本不会自动切换第三方镜像。

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
