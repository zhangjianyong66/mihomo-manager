# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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