# Go CLI 项目基础规范

本目录记录 `mihomo-manager` 当前真实的工程约定。项目是单仓库 Go CLI，主入口为 `cmd/mm`，已包含尚未接入运行入口的 SQLite 领域存储底座，没有 Web 服务端或独立前端层。

## 规范索引

| 文档 | 内容 |
|------|------|
| [目录结构](./directory-structure.md) | 包职责、文件归属和新增代码位置，包括 daemon/IPC/platform |
| [开发与验证命令](./development-commands.md) | 运行、构建、测试和配置检查命令 |
| [代码风格](./code-style.md) | Go、TUI、错误处理、配置写入和测试风格 |
| [CLI 与应用边界契约](./cli-contract.md) | 命令工厂、应用 ports、JSON、错误、退出码和脱敏 |
| [SQLite 领域存储契约](./storage.md) | 领域/SQL 边界、迁移、权限、错误和测试契约 |
| [daemon、Unix IPC 与 systemd](./daemon-ipc.md) | XDG 路径、协议、peer UID、生命周期、幂等和 unit 回滚契约 |
| [Core adapter 与 generation](./core-adapter.md) | mihomo 渲染/验证、只读 external、精确进程托管和切换恢复契约 |
| [安装与部署](./deployment.md) | 一键安装、独立 mm、mihomo core、daemon systemd unit、状态与安全卸载 |
| [重要项目约定](./project-conventions.md) | 配置路径、路由语义、订阅更新和兼容边界 |

## 开发前检查（Pre-Development Checklist）

1. 先阅读 [目录结构](./directory-structure.md) 和 [重要项目约定](./project-conventions.md)。
2. 修改 Go 代码时阅读 [代码风格](./code-style.md)；修改安装或运行方式时再读 [安装与部署](./deployment.md)。
3. 搜索现有实现后再新增辅助函数；核心业务优先放在 `internal/mihomo`，不要继续扩展遗留 Shell CLI。
4. 修改命令、应用服务、输出或错误时阅读 [CLI 与应用边界契约](./cli-contract.md)。
5. 修改领域持久化、SQLite schema、迁移或仓储时阅读 [SQLite 领域存储契约](./storage.md)。
6. 完成修改后至少执行 `gofmt` 检查和 `go test ./...`，具体命令见 [开发与验证命令](./development-commands.md)。
7. 修改 daemon、IPC、Unix socket、systemd unit 或 manager XDG 路径时阅读 [daemon、Unix IPC 与 systemd](./daemon-ipc.md)。
8. 修改 core adapter、generation、mihomo 进程/API 或档案切换时阅读 [Core adapter 与 generation](./core-adapter.md)。

## 质量检查（Quality Check）

- 确认改动落在正确包中，没有把业务逻辑放入 `cmd/mm` 或 TUI 渲染函数。
- 新命令必须验证 stdout/stderr、table/json、退出码和默认脱敏，不注册空壳命令。
- 确认 Go 文件经过 `gofmt`，并通过 `go test ./...`。
- 配置、订阅、白名单或路由变更应有临时目录驱动的回归测试，不得触碰用户真实配置。
- 安装脚本变更需核对独立构建产物、core 校验、PATH 幂等、旧软链接迁移和卸载保留配置的行为。
- 检查是否新增了路径、端口、环境变量或运行约定；如有，同步更新本目录和根 `AGENTS.md`。
- 存储改动需验证迁移追加性、恢复点、权限、来源事务隔离和双架构无 CGO 构建。

## 规范边界

- `.trellis/spec/guides/` 是共享思考指南，不代表本项目的具体代码结构。
- `scripts/lib/`、`bin/mihomo-manager` 和旧 Shell 测试仍保留在仓库中，但当前产品入口已经迁移到 Go 版交互式 `mm`。
- `internal/store` 仍只接收显式数据库路径；daemon 在运行时按 XDG 装配默认路径并拥有状态写入权，业务写 API 尚未在 A3 接入。
