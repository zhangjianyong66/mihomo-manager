# 项目说明

- 本项目是 Go CLI 项目，模块名为 `github.com/zhangjianyong66/mihomo-manager`。
- Go 版本要求见 `go.mod`：`go 1.22`；当前本机用户目录安装了 Go `1.26.4`，入口为 `~/.local/bin/go`。
- 安装命令：`make install`，实际执行 `scripts/install.sh`。
- 安装产物：`bin/mm`，并软链接到 `~/.local/bin/mm`。当前 `~/.local/bin` 已在 PATH 中。
- `mm` 只安装管理器，不会自动安装 mihomo core。默认 core 路径为 `~/.local/bin/mihomo`，可用 `MIHOMO_BIN` 环境变量覆盖。
- 当前本机已安装 MetaCubeX/mihomo `v1.19.27` Linux amd64 v1 构建到 `~/.local/bin/mihomo`。
- 运行命令：`mm`，默认打开交互式 TUI；可用 `mm --help` 做非交互冒烟验证。
- 测试命令：`go test ./...`。
- 安装脚本会创建 `~/.config/mihomo`。当前本机有最小可运行配置 `~/.config/mihomo/config.yaml`，默认 `DIRECT` 出口；导入订阅后可由管理器更新配置。
- 配置测试命令：`~/.local/bin/mihomo -t -f ~/.config/mihomo/config.yaml`。
- Go 版订阅更新逻辑支持完整 YAML 配置，也支持纯文本或 base64 编码的节点 URI 列表；当前覆盖 `vless://`、`vmess://`、`trojan://`、`ss://`。
- 订阅更新默认保留本地端口配置，并生成不依赖 Geo 数据下载的规则：白名单域名直连，其余走 `🌐 代理`；需要大陆直连时再手动执行“配置管理 -> 应用分流规则（大陆直连/其他走GLOBAL）”。
- 当前本机 mihomo 正在监听 `127.0.0.1:10808`（mixed-port）、`127.0.0.1:7891`（socks-port）和 `127.0.0.1:9090`（external-controller）。
- 当前 GNOME 系统代理地址已配置为 HTTP/HTTPS/SOCKS 均指向 `127.0.0.1:10808`，且 `org.gnome.system.proxy mode` 为 `manual`，Chrome 等遵循系统代理的应用会走 mihomo。本地忽略地址为 `localhost`、`127.0.0.0/8`、`::1`。
- 当前 mihomo 运行态为 `rule`，`~/.config/mihomo/config.yaml` 已写入 `mode: rule`。
- mihomo 的 `global` 模式会绕过 `rules`，因此白名单域名直连规则不会生效；当前采用 `rule` 模式，并在规则末尾保留 `MATCH,GLOBAL`，实现“白名单直连，其余全部走 GLOBAL 分组”。
- Go 版服务启动逻辑位于 `internal/mihomo/client.go`，启动 mihomo 时会设置新 session，避免父进程退出时清理 mihomo 子进程。
- 仓库包含 macOS `launchd` 配置，但在 Linux 环境不会安装该 LaunchAgent。
- 卸载命令：`make uninstall`，实际执行 `scripts/uninstall.sh`。

# 协作约定

- 永远使用中文回答问题，除非用户明确要求英文。
- 永远使用中文制定计划方案，写入计划方案。
- git commit 的 message 使用中文描述。
- 处理项目时，如果发现新的目录结构、环境配置、部署方式、运行约定或下次可复用信息，应及时更新本文件。
