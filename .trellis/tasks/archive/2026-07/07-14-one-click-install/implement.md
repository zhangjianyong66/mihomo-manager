# 实施计划：一键安装依赖并注册 mm 命令

## 1. 安装脚本基础结构

- [x] 重构 `scripts/install.sh` 为函数化安装器，加入参数解析、日志、临时文件清理和统一错误处理。
- [x] 实现 root、Ubuntu/Debian、apt、amd64/arm64 的无变更预检。
- [x] 实现缺失命令到 apt 包的映射、一次确认及 `--yes`/`MM_ASSUME_YES=1`。
- [x] 保证 sudo 只包裹 apt 命令，所有用户文件操作保持当前用户身份。

验证：

```bash
bash -n scripts/install.sh
```

## 2. Go 工具链与 mm 独立安装

- [x] 实现 Go 版本解析与 `>=1.22` 判断。
- [x] 实现 Go 1.26.4 amd64/arm64 官方下载、固定 SHA-256 校验、版本化隔离安装和复用。
- [x] 使用选定 Go、`CGO_ENABLED=0` 和临时输出构建 `mm`。
- [x] 对临时 `mm` 执行 `--help`，通过后原子复制到 `$HOME/.local/bin/mm`。
- [x] 移除仓库 `bin/mm` 软链接安装语义，确保仓库删除后命令仍可运行。

验证：

```bash
go test ./...
go build -o /tmp/mm ./cmd/mm
/tmp/mm --help
```

## 3. mihomo core 与默认配置

- [x] 实现 core 版本规范化、架构资产映射和 Release API 解析。
- [x] 实现 SHA-256 校验、版本比较、不降级、`mihomo.bak` 备份、`--force-core` 和原子替换。
- [x] 实现仅在缺失时创建最小 DIRECT 配置，并用 core 校验后原子落盘。
- [x] 已有配置不改写；配置无效时输出明确的部分成功提示。

验证：

```bash
"${MIHOMO_BIN:-$HOME/.local/bin/mihomo}" -t -f "${CONFIG_DIR:-$HOME/.config/mihomo}/config.yaml"
```

## 4. PATH、状态与卸载

- [x] 实现 Bash/Zsh PATH 等效检测、标记块写入和幂等处理。
- [x] 设计并写入 `$HOME/.local/share/mihomo-manager/install-state`，不记录敏感数据。
- [x] 重构 `scripts/uninstall.sh`，兼容普通文件和旧软链接，默认删除 mm/隔离 Go/PATH 块并保留 core/config。
- [x] 实现 `--purge`、`--purge-config`、`--yes`，按状态归属保护用户原有 core。
- [x] 保留旧 macOS launchd 卸载清理的兼容行为，但不新增 macOS 安装逻辑。

验证：

```bash
bash -n scripts/uninstall.sh
```

## 5. 远程引导入口

- [x] 新增 `scripts/bootstrap.sh`，实现 `MM_REF` 源码归档下载、解压、参数透传和 trap 清理。
- [x] 支持标准代理变量、`MM_GITHUB_BASE_URL`、`MM_GITHUB_API_BASE_URL`、`MM_GO_DOWNLOAD_BASE_URL`。
- [x] 下载失败时给出显式代理/下载源提示，不自动切换第三方镜像。

验证：

```bash
bash -n scripts/bootstrap.sh
```

## 6. 自动化测试

- [x] 新增 `scripts/tests/test_install.sh`，所有场景使用临时 HOME、命令替身和本地夹具。
- [x] 覆盖 root/平台/架构拒绝，且断言预检失败无持久化变更。
- [x] 覆盖系统 Go 复用与隔离 Go 选择。
- [x] 覆盖错误 SHA 不覆盖 core、core 高版本不降级、强制替换及备份。
- [x] 覆盖 PATH 幂等、已有配置不覆盖、独立 mm 安装和卸载归属判断。
- [x] 确保测试不会访问真实 `$HOME/.config/mihomo`、不会调用真实 apt/sudo、不会启动 core。

验证：

```bash
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
go test ./...
```

## 7. 文档与项目知识

- [x] 更新 `README.md`：一行安装、审阅后安装、本地安装、支持范围、权限、版本覆盖、代理、卸载和风险说明。
- [x] 更新 `Makefile` 帮助，移除未生效的 PREFIX 表述。
- [x] 更新 `.trellis/spec/backend/deployment.md`，记录新的安装、core、状态和卸载契约。
- [x] 更新根 `AGENTS.md`，替换旧软链接、manager-only 安装和 PREFIX 等过时说明，记录新增环境变量与验证方式。

## 8. 最终质量门

- [x] 执行全部 Shell 语法与安装测试。
- [x] 执行 `go test ./...` 和 `go build -o /tmp/mm ./cmd/mm`。
- [x] 在隔离 HOME 中执行本地安装与默认卸载全流程，核对文件归属、PATH 幂等、配置保留和 core 归属。
- [x] 审查 `git diff`，确认未写入真实订阅、配置、令牌或本机绝对敏感路径。
- [x] 核对失败回滚点：已有 mm、core、配置在构建/下载/校验失败时保持可用。

## 回滚点

- `scripts/install.sh` 改造前保留现有行为的 diff 参考；若统一安装器无法稳定通过测试，先恢复本地构建能力，再独立修复远程入口。
- mm/core/config 只在验证通过后原子替换；运行时失败可保留旧文件并清理 `.new`/临时归档。
- 文档更新必须与最终脚本参数一致，若参数调整需在同一变更中同步修改。
