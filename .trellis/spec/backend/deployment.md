# 安装与部署

## 本地安装流程

`make install` 调用 `scripts/install.sh`，实际步骤是：

1. 创建 `~/.local/bin` 和 `~/.config/mihomo`。
2. 执行 `go build -o <仓库>/bin/mm ./cmd/mm`。
3. 将 `<仓库>/bin/mm` 标记为可执行文件。
4. 创建软链接 `~/.local/bin/mm -> <仓库>/bin/mm`。
5. 如果存在 `~/Library/LaunchAgents`，生成并加载 macOS 监控服务。

安装是“从当前工作树构建并软链接”，不是复制独立发行包；移动或删除仓库会使软链接失效。`bin/mm` 是安装脚本覆盖的构建产物，不应手工编辑。

## 安装边界

- 安装脚本只构建管理器，不下载 mihomo core。
- Go 版默认寻找 `~/.local/bin/mihomo`，可用 `MIHOMO_BIN` 覆盖。
- 默认配置目录为 `~/.config/mihomo`，可用 `CONFIG_DIR` 覆盖。
- external-controller 默认地址为 `http://127.0.0.1:9090`，端口可用 `MIHOMO_API_PORT` 覆盖；非数字端口值回退到 `9090`。
- 配置编辑器使用 `EDITOR`，未设置时回退到 `vi`。
- `Makefile` 中的 `PREFIX` 目前只影响帮助文本；`scripts/install.sh` 实际仍固定安装到 `~/.local/bin`。不要假设 `make PREFIX=... install` 会改变安装目录。

## mihomo 运行方式

- Go 客户端使用 `mihomo -d <config-dir> -f <config-file>` 启动 core，并把 stdout/stderr 追加到 `mihomo.log`。
- `internal/mihomo/client.go` 为子进程设置 `SysProcAttr.Setsid = true`，保证 TUI 退出后 core 不随父会话被清理；修改启动代码时必须保留这一约定。
- 运行配置位于 `<CONFIG_DIR>/config.yaml`，发布或安装前不应把本机配置、订阅地址或节点凭据写入仓库。

## macOS launchd

- `launchd/mihomo-monitor.plist` 每 300 秒运行一次 `scripts/mihomo-monitor.sh`，并在加载时立即执行。
- 安装脚本只在 `~/Library/LaunchAgents` 已存在时安装 LaunchAgent；Linux 环境不会安装该服务。
- 模板中的 `{{PROJECT_DIR}}`、`{{CONFIG_DIR}}` 和 `{{HOME}}` 由安装脚本替换，因此移动仓库后需要重新安装。

## 卸载

`make uninstall` 调用 `scripts/uninstall.sh`：卸载并删除 macOS plist，删除 `~/.local/bin/mm` 软链接，但保留 `~/.config/mihomo`。删除用户配置必须由用户显式执行，不能作为默认卸载步骤。
