# 支持 macOS 一键安装

## Goal

让 macOS 用户通过现有本地入口 `make install` 和远程 bootstrap 入口安装可实际运行的 `mm`、mihomo core、规则集与初始配置，同时不破坏现有 Ubuntu/Debian 安装、升级和卸载契约。

## Background

- 当前 `scripts/install.sh` 在持久化变更前只接受 Ubuntu/Debian，并强依赖 apt、`dpkg-query`、GNU `sha256sum` 和 Linux mihomo 资产（`scripts/install.sh:192`、`scripts/install.sh:219`、`scripts/install.sh:243`、`scripts/install.sh:639`）。
- MetaCubeX/mihomo v1.19.28 同时提供 Darwin amd64 与 arm64 `.gz` 资产及 GitHub Release digest，现有“通过 Release API 发现资产并校验摘要”的安全模型可复用。
- `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/mm` 当前成功，但不等于运行时可用。
- daemon 当前固定装配 systemd controller（`internal/app/daemon_adapters.go:22`），路径固定生成 systemd user unit 目录（`internal/config/config.go:97`）。
- 非 Linux 的 Unix listener 与文件锁当前返回 `platform.ErrUnsupported`（`internal/platform/unix_other.go:7`、`internal/platform/lock_other.go:5`），所以 macOS 上连 `mm daemon run` 都无法正常启动。
- 仓库 `launchd/mihomo-monitor.plist` 是旧 Shell 监控任务，不是当前 Go manager daemon，不能直接作为新安装方案。
- GNOME 系统代理能力依赖 `gsettings`；Bash 环境代理只管理 `~/.bashrc`。macOS 的系统代理和默认 Zsh 环境目前没有对应产品实现。

## Requirements

### R1 平台与入口

- 保留现有 Ubuntu/Debian amd64/arm64 安装行为。
- 新增支持 macOS 12 Monterey 及以上的 amd64（Intel）与 arm64（Apple Silicon），不支持更低版本或其他 Darwin 架构。
- macOS 支持必须覆盖 `make install` 与 `scripts/bootstrap.sh` 两个现有入口，二者继续共用 `scripts/install.sh` 作为事实来源。
- 安装器继续要求普通用户执行，不使用整段 root 权限。
- 不支持的平台或架构必须在任何持久化变更前失败并给出准确提示。

### R2 依赖、Go 与下载完整性

- macOS 使用系统已有工具或 Homebrew 补齐缺失依赖；只在确有缺失且用户确认（或 `--yes`）后执行包管理器操作。
- macOS 上必须兼容系统 `shasum -a 256`，不得以 GNU `sha256sum` 为前提。
- 系统 Go 低于 1.22 或缺失时，继续使用隔离工具链；归档必须按 Darwin/架构选择并使用固定可信 SHA-256 校验。
- mihomo core 必须按 Darwin/架构选择固定 v1.19.28 资产，通过 Release API digest、版本冒烟和原子替换后发布；不得降低已有更高版本。
- `mm` 继续以 `CGO_ENABLED=0` 构建、`--help` 冒烟并原子安装为 `~/.local/bin/mm` 普通文件。

### R3 配置、规则集和 PATH

- 继续使用 `~/.config/mihomo`、`~/.local/bin` 与 XDG fallback 路径，保持环境变量覆盖契约。
- 规则集下载、双文件验证、缓存复用、成对原子发布和权限契约在 macOS 上保持一致。
- 新建配置、已有配置保留、mihomo 原生校验与“不自动启动 core、不修改系统代理”保持一致。
- macOS 默认 Zsh，PATH 配置必须幂等写入 `~/.zshrc`；不得重复追加或破坏用户内容。

### R4 daemon 可用性

- macOS 安装完成后，`mm` 的 daemon-backed CLI/TUI 不能因 `platform.ErrUnsupported` 而整体不可用。
- Darwin 的 Unix socket、单实例锁、文件归属检查和精确 core 进程控制必须保留与 Linux 等价的安全目标；不允许通过关闭所有本地身份/路径检查来换取可运行。
- macOS 必须使用 launchd user LaunchAgent 提供 daemon 后台生命周期管理，正式支持 `mm daemon enable|disable|start|stop|restart|status`。
- LaunchAgent 在用户登录域运行，不使用 root/system daemon；安装后自动启用并启动 manager daemon，但保持 mihomo core `stopped`。
- 新 manager plist 必须有独立、稳定的 label，不复用旧 `com.mihomo.monitor` 监控任务。

### R5 升级与卸载

- macOS 重复安装必须保留现有配置和更高版本 core，并遵守“仅在 core 明确 stopped 时自动重启 daemon”的升级约束。
- 默认卸载删除 `mm`、隔离 Go、安装器 PATH 区块和本项目受管 daemon 资产，保留 core、配置和规则集。
- `--purge` 与 `--purge-config` 的归属校验、确认和保留策略在 macOS 上保持一致。
- 旧 `com.mihomo.monitor.plist` 等兼容清理不得误删新的 manager daemon plist。

### R6 文档与兼容性

- README、安装帮助、错误提示和部署规范必须准确描述 Linux/macOS 支持矩阵、依赖管理和 daemon 行为。
- Linux 安装测试、Go 测试和 Linux 构建不得回归；新增 Darwin amd64/arm64 无 CGO 交叉构建和 macOS 安装脚本夹具测试。
- macOS 尚不支持的 GNOME 专属能力必须明确返回平台不支持或展示准确警告，不能误称可用或尝试运行 `gsettings`。

## Acceptance Criteria

- [x] AC1：Darwin arm64 与 amd64 均能通过安装器预检，其他 Darwin 架构在任何持久化变更前失败。
- [x] AC2：在隔离 HOME 和命令夹具中，macOS fresh install 能安装独立 `mm`、正确 Darwin mihomo core、两份已校验规则集、最小配置和幂等 Zsh PATH 区块。
- [x] AC3：macOS 安装产物可启动 manager daemon，`mm daemon status` 能完成协议握手，core 初始状态为 `stopped`。
- [x] AC4：macOS 重复安装不覆盖已有配置、不降级 core、不重复 PATH；daemon 升级行为不打断 running/degraded/未知 core。
- [x] AC5：macOS 默认卸载保留 core/config/rulesets，purge 只删除 install-state 可确认归属的资产，并安全清理受管 daemon 配置。
- [x] AC6：Linux 原有安装与卸载测试全部通过，新增 macOS 分支测试不依赖真实 Homebrew、launchd、网络或用户目录。
- [x] AC7：`GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/mm` 和 `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/mm` 均成功。
- [x] AC8：macOS 上 GNOME 专属系统代理操作不会执行 `gsettings`，并返回准确、可操作的平台提示。

## Out of Scope

- Windows、FreeBSD 和 Linux 的其他发行版/包管理器。
- 将 mihomo core 内嵌到 `mm`。
- macOS 系统代理的读取、设置和恢复；`mm proxy system` 在 macOS 上只返回明确的平台不支持提示，不执行 `gsettings` 或 `networksetup`。
- 将现有 Bash 环境代理管理扩展到 Zsh；本任务只保证安装器的 Zsh PATH 配置。
- 创建图形化 `.app`、Homebrew formula、签名、公证或 App Store 分发。
- 改写用户已有 mihomo 配置、自动启动 mihomo core 或接管其他代理进程。

## Technical Notes

- Go 1.26 是最后一个支持 macOS 12 的 Go 版本；当前固定隔离工具链为 Go 1.26.4，因此 macOS 12 是可验证且不额外扩大工具链矩阵的最低版本。
- `golang.org/x/sys/unix` 在 Darwin 提供 `GetsockoptXucred`、`LOCAL_PEERCRED` 与 `Flock`，可以实现与 Linux 同目标的 peer UID 和单实例约束。
- launchd 采用当前用户 GUI domain（`gui/<uid>`）的 LaunchAgent；若当前没有 GUI login domain，只安装受管 plist 并给出前台启动提示，不使用 sudo 或 system domain。
