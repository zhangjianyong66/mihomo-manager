# macOS 平台证据

## Go 与架构

- Go 1.26 release notes 明确说明：Go 1.26 是最后一个支持 macOS 12 Monterey 的版本，Go 1.27 将要求 macOS 13。来源：<https://go.dev/doc/go1.26#darwin>。
- Go 1.26.4 官方归档与 SHA-256：
  - `go1.26.4.darwin-amd64.tar.gz`: `05dc9b5f9997744520aaebb3d5deaa7c755371aebbfb7f97c2511a9f3367538d`
  - `go1.26.4.darwin-arm64.tar.gz`: `b62ad2b6d7d2464f12a5bcad7ff47f19d08325773b5efd21610e445a05a9bf53`
  - 来源：<https://go.dev/dl/?mode=json&include=all>。
- 本仓库已用 Go 1.26.4 成功执行 `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/mm`，产物为 Mach-O arm64；这只证明编译通过，不证明 daemon 运行时已可用。

## mihomo 资产

- MetaCubeX/mihomo `v1.19.28` Release API 同时提供：
  - `mihomo-darwin-amd64-v1-v1.19.28.gz`
  - `mihomo-darwin-arm64-v1.19.28.gz`
- 两者均由现有 Release API `assets[].digest` 模型提供 SHA-256，安装器无需引入第二套信任来源。
- 来源：<https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/v1.19.28>。

## launchd

- `launchctl` 的现代服务管理接口包括：
  - `bootstrap gui/<uid> <plist>`：把 LaunchAgent 加载进当前用户 GUI domain。
  - `bootout gui/<uid>/<label>`：移除已加载服务。
  - `enable|disable gui/<uid>/<label>`：持久启用/禁用服务。
  - `kickstart -k gui/<uid>/<label>`：重启已加载服务。
  - `print gui/<uid>/<label>`：探测服务是否已加载及当前状态。
- GUI domain 是与登录用户关联的 user-login domain；system domain 修改需要 root，不适合本项目普通用户安装契约。
- `launchd.plist` 要求稳定唯一的 `Label`，`Program` 必须为绝对路径；`KeepAlive=true` 隐含 `RunAtLoad`，异常快速退出会被 launchd 节流。
- 来源：<https://keith.github.io/xcode-man-pages/launchctl.1.html>、<https://keith.github.io/xcode-man-pages/launchd.plist.5.html>（Xcode man pages 镜像）。

## Darwin 本地 IPC

- `golang.org/x/sys/unix v0.27.0` 的 Darwin 实现提供 `GetsockoptXucred(fd, SOL_LOCAL, LOCAL_PEERCRED)`，上游测试验证返回 UID 等于 `os.Getuid()`。
- Darwin 同时提供 `unix.Flock`、`O_NOFOLLOW`、Unix domain socket 与 `syscall.Stat_t.Uid`，可复用当前 Linux 的私有目录、拒绝 symlink、单实例锁、stale socket 和同 UID peer 安全目标。
- launchd 方案不使用 socket activation；Darwin 的 `ActivatedListener` 应返回“未激活且无错误”，daemon 再创建自己的受保护 Unix socket。

## 当前阻塞点

- `internal/platform/unix_other.go`、`lock_other.go`、`activation_other.go` 在所有非 Linux 平台直接返回 `ErrUnsupported`。
- `internal/app/daemon_adapters.go` 固定创建 systemd controller；组合重启错误和诊断文案也写死 systemd。
- `internal/config.ManagerPaths` 只有 systemd user unit 路径，没有 LaunchAgents 路径。
- `scripts/install.sh` 固定检查 `/etc/os-release`、apt/dpkg、GNU `sha256sum`、Linux Go/core 资产。
- `scripts/uninstall.sh` 使用 macOS 不支持的 `chmod --reference`，且仅把 launchd 当作旧监控 plist 的兼容清理。
