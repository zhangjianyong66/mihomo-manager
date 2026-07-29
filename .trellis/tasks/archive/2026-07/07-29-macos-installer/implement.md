# macOS 一键安装实施计划

## 1. Darwin runtime 基础

- [x] 为 Darwin 实现 Unix listener、stale socket、同 UID `LOCAL_PEERCRED` 校验、文件归属与 `flock` 单实例锁。
- [x] 让 Darwin activation 返回“无 socket activation”而非 unsupported，其他未支持平台保持拒绝。
- [x] 为 Darwin mihomo 子进程使用独立 session 和精确进程句柄终止。
- [x] 补平台 build-tag 测试和 Darwin 测试二进制编译，确认 Linux 行为未改变。

验证：

```bash
go test ./internal/platform ./internal/mihomo
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/mm-platform-darwin-amd64.test ./internal/platform
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c -o /tmp/mm-platform-darwin-arm64.test ./internal/platform
```

回滚点：本阶段不改持久格式；Darwin build-tag 文件可独立回滚，Linux 测试必须持续通过。

## 2. launchd controller 与应用装配

- [x] 扩展 `config.ManagerPaths`，集中提供 LaunchAgents 和 launch log 路径。
- [x] 新增 `internal/platform/launchd` controller、受管 plist、环境校验、摘要归属、备份和原子恢复。
- [x] 用 build-tag factory 在 Linux/Darwin 选择 systemd/launchd，保留统一 `app.DaemonController`。
- [x] 为 `DaemonControlResult` 增加 backend/ready，泛化 status 诊断与组合重启的 systemd 专属判断和文案。
- [x] 覆盖 launchctl 动作、幂等、GUI domain unavailable、未知/修改 plist、失败回滚和固定 label 安全测试。

验证：

```bash
go test ./internal/config ./internal/platform/systemd ./internal/platform/launchd ./internal/app ./internal/cli ./internal/tui
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c -o /tmp/mm-launchd-darwin-arm64.test ./internal/platform/launchd
```

回滚点：launchd Enable 在加载前保存原 plist；失败必须恢复磁盘内容且不得 bootout 已运行旧 job。

## 3. macOS 安装路径

- [x] 重构安装器预检为 Linux/Darwin 明确分支，加入 macOS 12+ 与 amd64/arm64 检查。
- [x] 拆分 apt/Homebrew 依赖路径；Homebrew 只补缺失包，不自动安装自身、不调用 sudo。
- [x] 统一 SHA-256 helper，兼容 `sha256sum` 与 `shasum -a 256`；清理 GNU-only shell 调用。
- [x] 增加 Darwin amd64/arm64 Go 1.26.4 固定归档摘要与隔离安装。
- [x] 增加 Darwin mihomo v1.19.28 资产映射，复用 Release digest、版本、不降级和原子替换契约。
- [x] 保持正式 `mm daemon` 编排、fresh migrate、core stopped-only daemon restart 和 Zsh PATH 幂等。
- [x] 升级 install-state，记录非敏感 platform/backend/plist 路径并兼容旧状态。

验证：

```bash
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
```

回滚点：每个资产保持“临时下载 -> 摘要/原生验证 -> 原子替换”；失败不得覆盖旧 mm/core/config/ruleset。

## 4. macOS 卸载与兼容清理

- [x] 在删除 mm 前通过正式 CLI 停用 daemon；无可用 CLI 时只清理 install-state 可确认的受管 plist。
- [x] 校验新 manager plist 的路径、label、摘要和归属，未知或外部修改资产只告警保留。
- [x] 保留旧 monitor plist 兼容清理，确保不误删新 manager plist。
- [x] 替换 `chmod --reference` 等 macOS 不兼容调用，保持 shell 文件 mode 与内容安全。
- [x] 覆盖默认保留 core/config/rulesets、purge 归属和 install-state round-trip。

验证：

```bash
bash scripts/tests/test_install.sh
bash -n scripts/uninstall.sh
```

回滚点：先停后台服务再删除二进制；任何归属不确定均选择保留并告警。

## 5. macOS 平台能力提示

- [x] 为 Darwin 的 system proxy inspector/configurator 返回 typed unsupported，不执行 `gsettings` 或 `networksetup`。
- [x] 调整 CLI/TUI/状态诊断文案，避免在 macOS 宣称 GNOME 系统代理可用。
- [x] 保持 Bash 环境代理现有行为，不在本任务扩展 Zsh 环境代理。

验证：

```bash
go test ./internal/platform ./internal/daemon ./internal/cli ./internal/tui
```

## 6. 文档与项目规范

- [x] 更新 README、安装/卸载帮助和 Makefile 文案，写明 macOS 12+、双架构、Homebrew 补缺、launchd 与不支持的系统代理能力。
- [x] 更新 deployment、daemon IPC、directory structure、development commands 和 project conventions 中的 Linux-only 契约。
- [x] 按项目 AGENTS 维护规则只更新已经由代码和测试验证的长期平台约定。

## 7. 最终质量门

- [x] 执行格式化、全量测试、race 和 vet。
- [x] 执行安装测试与全部 Shell 语法检查。
- [x] 执行 Linux/Darwin amd64/arm64 四组合无 CGO 构建。
- [x] 审查 install-state、plist、日志和测试夹具，不得写入真实配置、凭据或用户目录。
- [x] 审查 Linux installer/systemd 与现有组合重启没有行为回归。

验证：

```bash
gofmt -w <changed-go-files>
go test ./...
GOTOOLCHAIN=go1.22.12 go test -race ./...
go vet ./...
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/mm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/mm
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./cmd/mm
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./cmd/mm
```

## 完成判定

- [x] PRD 的 AC1-AC8 均有自动化测试或明确的 macOS 实机验证证据。
- [x] 安装器输出准确说明 manager daemon 已启动、mihomo core 仍 stopped、系统代理未修改。
- [x] 所有失败路径都保留用户已有配置、非受管 core 与未知 launchd 资产。
