# macOS 一键安装技术设计

## 1. 设计目标与边界

本任务把 macOS 12+ amd64/arm64 提升为与 Ubuntu/Debian 并列的一键安装平台。完成标准不是“脚本不再拒绝 Darwin”，而是安装后的 daemon-backed CLI/TUI、launchd 生命周期、core 管理、升级与卸载均可用。

本任务不实现 macOS 系统代理或 Zsh 环境代理。GNOME 专属入口在 Darwin 必须以 typed unsupported 结果结束，不能尝试调用不存在的 `gsettings`。

这是一条端到端安装协议，Darwin runtime、launchd controller 和 installer 相互依赖并由同一组验收场景验证，因此保留为一个复杂任务，不再拆父子任务；实施计划内部按可回滚阶段推进。

## 2. 平台矩阵

| 平台 | 架构 | 依赖管理 | daemon backend | mihomo 资产 |
|---|---|---|---|---|
| Ubuntu/Debian | amd64 | apt | systemd user service/socket | `mihomo-linux-amd64-v1-<version>.gz` |
| Ubuntu/Debian | arm64 | apt | systemd user service/socket | `mihomo-linux-arm64-<version>.gz` |
| macOS 12+ | amd64 | 系统工具 + Homebrew 补缺 | launchd user LaunchAgent | `mihomo-darwin-amd64-v1-<version>.gz` |
| macOS 12+ | arm64 | 系统工具 + Homebrew 补缺 | launchd user LaunchAgent | `mihomo-darwin-arm64-<version>.gz` |

安装器把 `TARGET_OS=linux|darwin` 与 `TARGET_ARCH=amd64|arm64` 作为一次预检后的内部事实，后续 Go、core、依赖、checksum 和 daemon 文案都只消费这两个值，避免每个步骤重新判断平台。

## 3. Darwin runtime

### 3.1 Unix socket 与 peer UID

把 Linux listener 中可移植部分抽为 `linux || darwin` 的 Unix 实现：绝对路径、父目录 `0700`、socket `0600`、拒绝 symlink/非 socket/非当前用户目标、探测并只删除同 UID stale socket。peer credential 保持平台文件分离：

- Linux：`SO_PEERCRED` / `GetsockoptUcred`。
- Darwin：`SOL_LOCAL` + `LOCAL_PEERCRED` / `GetsockoptXucred`。

两者都在 `Accept` 后、HTTP 解析前拒绝非预期 UID。获取 credential 失败也拒绝连接，不降级为仅依赖文件权限。

### 3.2 单实例、归属与 activation

- 将当前 `flock + O_NOFOLLOW` 文件锁实现扩展到 Darwin，保持 lock `0600` 与 `ErrAlreadyLocked` 语义。
- Darwin 使用 `syscall.Stat_t.Uid` 实现目录和 socket 归属检查。
- 新增 Darwin activation 实现返回 `(nil, false, nil)`；launchd 不传 systemd socket activation FD，daemon 随后自行创建 Unix socket。
- 其他非 Linux/Darwin 平台继续返回 `ErrUnsupported`，不意外扩展支持范围。

### 3.3 core 子进程

Darwin 与 Linux 一样为 mihomo 子进程创建独立 session，并只通过 daemon 持有的精确 `exec.Cmd` 句柄发送 TERM/kill；不引入 `pgrep/pkill` 回退，不识别或接管其他代理进程。

## 4. daemon backend 抽象与 launchd

### 4.1 应用边界

保留 `app.DaemonController` 作为统一接口，通过 build-tag factory 装配：Linux 选择现有 systemd controller，Darwin 选择新增 `internal/platform/launchd.Controller`，其他平台返回 unsupported controller。`cmd/mm`、CLI 和 TUI 不直接判断 GOOS。

`DaemonControlResult` 增加 additive 字段：

```go
Backend string `json:"backend,omitempty"` // systemd | launchd
Ready   bool   `json:"ready"`
```

- systemd 的 `Ready` 表示受管 service/socket 均完整且 active。
- launchd 的 `Ready` 表示受管 job 已加载并运行。
- 现有 `ServiceActive`/`SocketActive` 保留以兼容现有 JSON；组合重启改用 `Backend`、`Ready` 和 `Managed`，不再用 systemd 字段解释 launchd。

组合重启事务的 Core 状态机、PID/startedAt 握手和补偿顺序不变，只把 systemd 专属消息改成“后台服务管理器/受管 daemon”。

### 4.2 LaunchAgent 资产

新增稳定 label `com.zhangjianyong.mihomo-manager.daemon`，plist 路径为 `~/Library/LaunchAgents/<label>.plist`。plist 由 controller 使用绝对路径和 XML escaping 渲染，包含：

- `ProgramArguments`: `<absolute mm> daemon run`
- `RunAtLoad=true`、`KeepAlive=true`、`ProcessType=Background`
- `Umask=63`（0077）和有界 `ThrottleInterval`
- `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT` 的受校验 EnvironmentVariables
- state 目录下的 stdout/stderr 路径

受管 plist 使用内容 SHA-256 header 判定归属；未知 plist 不继承内容，受管但被修改的 plist 在覆盖前创建带 UTC 时间戳的备份。写入使用私有目录、`0600` 临时文件和同目录原子替换；测试做结构断言，真实 macOS 可用 `plutil -lint` 复核。

### 4.3 launchctl 状态与动作

目标固定为 `gui/<uid>` 与 `gui/<uid>/<label>`：

- `Status`：`launchctl print` 探测 GUI domain 和 service；不扫描任意进程。
- `Enable`：原子安装 plist，`launchctl enable` 后仅在 job 未加载时 `bootstrap`。已加载升级只更新磁盘 plist，不自动重启。
- `Start`：已加载且 running 时幂等成功；未加载时 `enable + bootstrap`。
- `Stop`：`bootout` service target，保留 plist 和 enabled 状态。
- `Restart`：仅对已加载受管 job 执行 `kickstart -k`。
- `Disable`：`bootout` 后 `disable`，保留 plist供显式重新启用。

GUI domain 不可用时，Enable 仍安装并校验 plist，但返回 `enabled=false/ready=false` 与 `mm daemon run` 提示；不切换到 system domain、不使用 sudo。

## 5. 路径与配置

`config.ManagerPaths` 增加 LaunchAgents 和 daemon launch log 路径，继续由 `ResolveManagerPaths` 生成绝对路径。Linux 的 `UserUnitDir` 保留，避免已有 systemd 契约和测试变化。launchd controller 只接收显式路径、mm binary 与环境 map，不自行读取 HOME。

诊断 details 从写死的 `systemd` key 改为带 `backend` 的 service-manager 结构；JSON 只做 additive 扩展，daemon IPC protocol version 和 SQLite schema 不变。

## 6. 安装器

### 6.1 预检与依赖

- `uname -s` 识别 Linux/Darwin；Linux 继续验证 Debian family，Darwin 用 `sw_vers -productVersion` 验证 `>=12`。
- 保留测试专用 OS/版本/架构覆盖，与 `MM_EUID_OVERRIDE` 一样不写入公开安装契约。
- Linux 依赖路径保持 apt；Darwin 优先系统命令，只在缺少 `jq` 等必要命令时要求已安装 Homebrew，经一次确认后执行 `brew install`。安装器不自动安装 Homebrew，不调用 sudo。
- 把 SHA-256 封装为统一函数：优先平台原生命令，Linux 支持 `sha256sum`，Darwin 支持 `shasum -a 256`。所有 Go/core/ruleset 校验只调用该函数。
- 移除 `chmod --reference` 等 GNU-only 调用，改为读取/保存原 mode 或采用明确安全 mode。

### 6.2 Go 与 core

Go 归档名改为 `go${GO_VERSION}.${TARGET_OS}-${TARGET_ARCH}.tar.gz`，四个平台/架构组合各有固定官方摘要。解压、工具链原子替换和最低版本判断保持现有语义。

mihomo 资产选择变为 `(TARGET_OS, TARGET_ARCH)` 的显式映射；仍通过 Release API 精确匹配单一资产及 `digest=sha256:*`，再做版本冒烟、备份、不降级和原子替换。

### 6.3 daemon、PATH 与状态

安装器仍通过正式 `mm daemon enable/start/status` 编排，不直接运行 launchctl。现有升级状态矩阵不变：只有旧 core 两次确认均为 stopped 才 stop/start daemon；running/degraded/failed/过渡/未知均不自动中断。

macOS 默认 Zsh 时幂等管理 `~/.zshrc` 的 PATH 标记块。install-state 升级为新版本并增加 `platform`、`daemon_backend`、`daemon_config_path`；旧状态读取保持兼容，新增字段不得包含敏感信息。

## 7. 卸载与回滚

卸载顺序调整为：先用仍存在的 `mm daemon disable` 停止受管后台 daemon，再按 install-state 校验并删除本项目 manager plist，最后删除 mm/toolchain/PATH。未知、越界、非受管或被外部修改的 plist 一律保留并告警。

旧 `com.mihomo.monitor.plist` 与 `com.openclaw.mihomo-monitor.plist` 继续作为独立兼容清理，不与新 label 共用判断。默认仍保留 core/config/rulesets；purge 归属约束不变。

失败回滚边界：

- runtime/platform 代码无持久迁移，代码回滚即可。
- launchd 安装失败恢复原 plist；已加载旧 job 不因磁盘更新自动重启。
- Go/core/mm/ruleset 仍只在校验完成后原子发布。
- 安装器发生部分完成时保留已验证 binary/config，并给出可执行修复命令，不删除用户原有资产。

## 8. macOS 专属能力降级

system proxy 的平台构造器在 Darwin 返回 typed `platform.ErrUnsupported`，读取状态展示“macOS 系统代理暂不支持”，写操作映射为可操作的 unsupported/invalid-capability 错误。代码路径不得执行 `gsettings` 或 `networksetup`。现有 Bash 环境代理能力不扩展到 Zsh。

## 9. 测试策略

- Darwin runtime：在 macOS runner 上验证 lock、socket mode/stale socket/peer UID；Linux CI 至少交叉编译 Darwin 包和测试二进制。
- launchd controller：fake runner 覆盖 fresh enable、幂等、已加载升级不重启、GUI domain unavailable、backup/rollback、每个动作只触及固定 label。
- app：systemd/launchd Ready 映射、组合重启通用消息、恢复与最终 backend 验证。
- installer：临时 HOME、fake `uname/sw_vers/brew/shasum/launchctl/mm` 与本地资产夹具，覆盖 macOS 版本/架构、依赖确认、Go/core 资产、Zsh PATH、升级矩阵和卸载归属。
- platform proxy：Darwin 构建路径不会调用 `gsettings`，返回明确 unsupported。
- 全量保持 Linux 测试、race、vet、Shell 语法与四种 GOOS/GOARCH 无 CGO 构建。
