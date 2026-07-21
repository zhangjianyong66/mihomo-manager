# M1 安装 CN 规则集与用户 daemon

## Goal

让本地与远程安装入口以同一套可审计、可恢复的流程准备 CN 专用规则资产和 manager daemon，使后续 Rule 模式不依赖首次启动时临时下载。

## Dependencies

- 依赖已完成的 systemd user、daemon、legacy migrate 基础能力；无前置 child milestone。

## Requirements

- `scripts/install.sh` 安装固定 commit 的 `cn-domain.mrs`、`cn-ip.mrs` 到 `CONFIG_DIR/rulesets`，使用固定 SHA-256 校验并原子替换。
- 支持 `MM_RULESET_BASE_URL`、`MM_RULESET_REF` 和可信摘要覆盖；自定义源不得绕过完整性校验。
- 首次无缓存下载/校验失败时安装失败；已有有效缓存时更新失败告警并保留旧文件。
- `install-state` 记录两份资产的来源、引用、路径、摘要；权限为目录 `0700`、文件 `0600`。
- 安装正式 `mm` 后安装、启用并启动 systemd user manager daemon，不启用 linger、不启动 core。
- 新建配置自动执行 legacy 注册；已有配置只提示 `migrate plan/apply`，不静默注册。
- systemd 用户会话不可用时保留 unit 并提示前台 daemon，不启动临时后台进程。
- 升级不得因重启 daemon 中断正在运行的受管 core；core running 时延后 daemon 重启并提示。

## Acceptance Criteria

- [x] M1-AC1：本地 `make install` 和远程 bootstrap 均只调用 `scripts/install.sh` 的同一规则集函数。
- [x] M1-AC2：固定资产下载、摘要匹配、权限、原子替换和 install-state 字段测试通过。
- [x] M1-AC3：无缓存失败为非零退出且不产生伪成功；有缓存失败保留文件内容和摘要。
- [x] M1-AC4：systemd 可用时 daemon 已启用/运行且 core 为 stopped；不可用时 unit 保留且输出可执行提示。
- [x] M1-AC5：安装器新建配置会注册活动 legacy profile；已有配置内容/摘要不变且只输出迁移提示。
- [x] M1-AC6：core running 的升级不会停止 core 或强制重启 daemon。
- [x] M1-AC7：安装/卸载测试、Shell 语法检查和 Go 构建通过，真实 HOME/systemd/core 未被测试访问。

## Out of Scope

- 在 config.yaml 中生成 provider/rules/DNS；由 M2 完成。
- 实现 mode API、CLI 或 TUI。
- 自动删除规则缓存；默认卸载继续保留 CONFIG_DIR。
