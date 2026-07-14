# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Ubuntu/Debian amd64/arm64 一键安装入口。
- 自动准备系统依赖、隔离 Go 工具链和校验后的 mihomo core。
- 安全卸载参数、PATH 幂等配置与隔离安装测试。

### Changed

- `mm` 改为独立复制到 `~/.local/bin/mm`，不再软链接源码仓库。
- 本地 `make install` 与远程安装复用同一安装逻辑。

## [1.0.0] - 2026-04-03

### Added
- Initial release
- Service management: start, stop, restart, status, reload
- Node management: list, switch, test latency, auto-select fastest
- Subscription management: update, save URL, show URL
- Whitelist management: add, remove, list domains
- Config management: backup, restore, edit
- Monitoring service: auto-restart on failure (every 5 minutes)
- Port config preservation during subscription updates
- macOS launchd integration

### Fixed
- Process detection pattern to match both relative and absolute paths
- Port config merging during subscription updates (port -> mixed-port)
- External controller binding (0.0.0.0 -> 127.0.0.1)

### Security
- Removed hardcoded paths and domains
- Config file permissions set to 600
