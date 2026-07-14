# 一键安装依赖并注册 mm 命令

## 目标

为 Ubuntu/Debian 用户提供可审计、可重复执行的一键 Shell 安装入口，自动准备 Mihomo Manager 的必要依赖、安装 mihomo core 和独立的 `mm` 命令，并确保新终端可直接调用 `mm`。

## 背景

- 项目是 Go CLI，`go.mod` 要求 Go 1.22。
- 当前 `make install` 调用 `scripts/install.sh`，要求系统预装 Go，只构建管理器，不安装 mihomo core。
- 当前安装脚本把 `mm` 构建到仓库 `bin/mm`，再软链接到 `$HOME/.local/bin/mm`；移动或删除仓库后命令失效。
- 当前脚本不保证 `$HOME/.local/bin` 已加入 PATH，`Makefile` 展示的 `PREFIX` 也未实际传递给安装脚本。
- Go 版运行时依赖 mihomo core，以及 `pgrep`/`pkill` 提供的进程管理能力；core 默认路径为 `$HOME/.local/bin/mihomo`。
- 当前仓库没有版本标签、预编译 Release 二进制或发布工作流。

## 需求

### R1 支持范围与安全边界

- 首版仅正式支持使用 `apt` 的 Ubuntu/Debian，CPU 架构支持 `amd64` 与 `arm64`。
- 不受支持的平台或架构必须在下载二进制或修改系统前明确报错退出。
- 安装器必须以普通用户运行；有效用户 ID 为 0 时无变更退出，不根据 `SUDO_USER` 自动降权。
- `sudo` 仅用于安装缺失的 apt 包，不得用于写入用户二进制、配置、工具链或 shell 启动文件。

### R2 安装入口与源码获取

- README 提供无需预先克隆仓库的一行 Shell 安装命令，并同时提供“下载、审阅、再执行”的方式及供应链风险说明。
- 保留仓库内 `make install` 本地入口；远程入口与本地入口最终复用同一安装逻辑。
- 远程入口默认安装 `master`，允许通过 `MM_REF` 指定分支、标签或提交哈希。
- 远程入口下载 GitHub 源码归档到临时目录，不要求 Git，并在退出时清理临时源码。
- 本次“一键按钮”仅指 Shell 安装入口，不包含桌面、网页或 TUI 图形按钮。

### R3 系统依赖与 Go 工具链

- 安装器检测并只安装缺失的系统包；执行 apt 变更前列出包名并默认确认一次。
- `--yes` 或 `MM_ASSUME_YES=1` 可跳过确认；无交互环境未显式同意时应失败并说明用法。
- 构建时优先复用系统中版本不低于 1.22 的 Go。
- Go 缺失或版本过低时，下载并校验官方 Go 1.26.4 到项目专用目录，支持 amd64/arm64，不覆盖系统 Go 或用户全局 Go 配置，并保留供后续更新复用。

### R4 mm 安装

- 从当前源码构建 `mm`，本次不建设预编译 Release 或 CI 发布链路。
- 编译结果必须以原子替换方式复制到 `$HOME/.local/bin/mm`，不得继续软链接到源码仓库。
- 重复安装应安全更新 `mm`；构建或冒烟验证失败时不得破坏已有可用命令。

### R5 mihomo core 安装

- 默认安装固定版本 `v1.19.28`，允许通过 `MIHOMO_VERSION` 覆盖。
- core 只能从 MetaCubeX/mihomo 官方 GitHub Release 获取，必须根据目标发布信息执行 SHA-256 校验后再安装。
- amd64 使用官方 Linux amd64 v1 基线构建，arm64 使用官方 Linux arm64 构建。
- 已有 core 版本等于或高于目标版本时默认保留；版本更低或无法识别时先备份为 `mihomo.bak`，再原子替换。
- `--force-core` 可强制重新安装目标版本；校验或下载失败时不得覆盖已有 core。

### R6 配置与运行行为

- 确保 `$HOME/.config/mihomo` 存在。
- `config.yaml` 不存在时创建仅使用 `DIRECT`、仅绑定本机控制端口的最小安全配置，并执行 `mihomo -t` 校验。
- 已有 `config.yaml` 时不得覆盖或重写；新建配置校验失败时移除无效文件。
- 安装成功后不自动启动 core，不修改系统代理，只执行配置校验和 `mm --help` 冒烟验证，并提示用户运行 `mm`。
- 保留 `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT`、`EDITOR` 等现有运行时覆盖能力。

### R7 PATH 配置

- 安装器检测 Bash/Zsh，只在缺少等效配置时向对应启动文件写入带明确项目标记的 `$HOME/.local/bin` PATH 片段。
- 重复安装不得重复追加；安装结束后提示重开终端或在当前终端临时导出 PATH。
- 无法识别的 shell 不擅自修改配置，需明确给出手动 PATH 指令。

### R8 网络与下载源

- 安装器遵循 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`，但网络失败时不得自动切换第三方镜像。
- 允许通过 `MM_GITHUB_BASE_URL`、`MM_GO_DOWNLOAD_BASE_URL` 显式覆盖下载源。
- 下载失败信息应给出代理或显式下载源配置提示。

### R9 卸载

- 默认卸载移除 `mm`、项目专用 Go 工具链和安装器写入的 PATH 片段，保留 mihomo core 与 `$HOME/.config/mihomo`。
- `--purge` 仅删除可确认由安装器管理的 core；无法确认归属时必须保留并警告。
- `--purge-config` 才允许删除用户配置。
- 安装器应保存最小必要的安装状态，以支持归属判断和幂等卸载。

### R10 文档与项目约定

- README 说明支持平台、安装内容、权限、安装路径、环境变量、更新方式、卸载方式和远程脚本风险。
- 更新 Makefile 帮助、安装部署规范和根 `AGENTS.md`，使其与新流程一致。
- macOS 安装与 launchd 调整不属于首版范围；不得声称受支持。

## 验收标准

- [x] AC1：Ubuntu/Debian amd64 与 arm64 能通过远程一行入口完成依赖准备、core 安装和 `mm` 源码构建；其他平台或架构无变更失败。
- [x] AC2：root 整体执行无变更失败；普通用户安装产生的文件均归属当前用户，只有缺失 apt 包时才按需 sudo。
- [x] AC3：系统 Go ≥1.22 时复用现有工具链；否则安装已校验的隔离 Go 1.26.4，且不替换系统 Go。
- [x] AC4：安装后 `$HOME/.local/bin/mm` 是独立可执行文件，源码仓库删除后 `mm --help` 仍成功。
- [x] AC5：mihomo core 下载经过 SHA-256 校验；失败不覆盖旧文件，高版本不降级，替换旧版本前生成备份，`--force-core` 行为明确。
- [x] AC6：首次安装生成可通过 `mihomo -t` 的最小 DIRECT 配置；重复安装不改变已有配置。
- [x] AC7：安装过程不启动代理、不修改系统代理；完成后提示用户运行 `mm`。
- [x] AC8：Bash/Zsh PATH 修改幂等；未知 shell 给出手动配置指令。
- [x] AC9：重复执行可安全更新；任一关键步骤失败时保留已有可用 `mm`、core 和配置。
- [x] AC10：默认卸载保留 core 与配置；显式清理参数遵循归属判断，不误删用户原有 core。
- [x] AC11：`MM_REF`、`MIHOMO_VERSION`、代理变量和显式下载源覆盖行为有文档且可验证。
- [x] AC12：本地 `make install`、`make uninstall`、Shell 语法检查、`go test ./...` 和安装后冒烟验证通过。

## 不在范围内

- Fedora、Arch、macOS、Windows 或其他包管理器的一键安装支持。
- 桌面图标、网页按钮或 TUI 安装按钮。
- 预编译 `mm` Release、自动发布 CI 或版本标签体系。
- 自动启动 mihomo、自动设置系统代理或安装 Linux 系统服务。
- 自动使用第三方 GitHub、Go 或 mihomo 镜像。
- macOS launchd 新增或调整。
