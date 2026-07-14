# 项目说明

- 本项目是 Go CLI 项目，模块名为 `github.com/zhangjianyong66/mihomo-manager`。
- Go 版本要求见 `go.mod`：`go 1.22`；当前本机用户目录安装了 Go `1.26.4`，入口为 `~/.local/bin/go`。
- 一键安装首版支持 Ubuntu/Debian amd64/arm64；远程入口为 `curl -fsSL https://raw.githubusercontent.com/zhangjianyong66/mihomo-manager/master/scripts/bootstrap.sh | bash`，本地入口为 `make install`。
- 安装器需以普通用户运行，只在安装缺失 apt 包时局部使用 sudo；支持 `--yes`/`MM_ASSUME_YES=1` 无交互确认。
- 安装产物是独立的 `~/.local/bin/mm` 普通文件，不再软链接仓库 `bin/mm`；移动或删除仓库不会影响已安装命令。
- 安装器会自动安装并校验 mihomo core，默认固定 `v1.19.28`，支持 `MIHOMO_VERSION` 覆盖；默认 core 路径为 `~/.local/bin/mihomo`，可用 `MIHOMO_BIN` 覆盖。
- 系统 Go 低于 1.22 或缺失时，安装器会把官方 Go 1.26.4 安装到 `~/.local/share/mihomo-manager/toolchains/go1.26.4`，不替换系统 Go。
- 当前本机已安装 MetaCubeX/mihomo `v1.19.27` Linux amd64 v1 构建到 `~/.local/bin/mihomo`。
- 运行命令：`mm`，默认打开交互式 TUI；可用 `mm --help` 做非交互冒烟验证。
- 测试命令：`go test ./...`；安装流程测试为 `bash scripts/tests/test_install.sh`；Shell 语法检查为 `bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh`。
- Go 版路径和端口可通过环境变量覆盖：`CONFIG_DIR` 修改配置目录，`MIHOMO_API_PORT` 修改 external-controller 端口，`EDITOR` 修改配置编辑器；`MIHOMO_BIN` 修改 core 路径。
- `tests/test.sh` 与 `scripts/tests/test_manager.sh` 面向旧非交互式 Shell 实现或依赖本机运行状态，不作为当前 Go TUI 的默认验收命令。
- 安装脚本会创建 `~/.config/mihomo`，仅在 `config.yaml` 缺失时生成最小 `DIRECT` 配置并校验，已有配置不会覆盖。当前本机有最小可运行配置 `~/.config/mihomo/config.yaml`，默认 `DIRECT` 出口；导入订阅后可由管理器更新配置。
- 配置测试命令：`~/.local/bin/mihomo -t -f ~/.config/mihomo/config.yaml`。
- Go 版订阅更新逻辑支持完整 YAML 配置，也支持纯文本或 base64 编码的节点 URI 列表；当前覆盖 `vless://`、`vmess://`、`trojan://`、`ss://`。
- 订阅更新默认保留本地端口配置，并生成不依赖 Geo 数据下载的规则：白名单域名直连，其余走 `🌐 代理`；需要大陆直连时再手动执行“配置管理 -> 应用分流规则（大陆直连/其他走GLOBAL）”。
- 当前本机 mihomo 正在监听 `127.0.0.1:10808`（mixed-port）、`127.0.0.1:7891`（socks-port）和 `127.0.0.1:9090`（external-controller）。
- 当前 GNOME 系统代理地址已配置为 HTTP/HTTPS/SOCKS 均指向 `127.0.0.1:10808`，且 `org.gnome.system.proxy mode` 为 `manual`，Chrome 等遵循系统代理的应用会走 mihomo。本地忽略地址为 `localhost`、`127.0.0.0/8`、`::1`。
- 当前 mihomo 运行态为 `rule`，`~/.config/mihomo/config.yaml` 已写入 `mode: rule`。
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
