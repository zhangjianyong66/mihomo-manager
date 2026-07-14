# 安装与部署

## 支持范围

- 一键安装首版仅支持 Ubuntu/Debian 的 `amd64` 与 `arm64`。
- 安装器必须由普通用户运行；只在安装缺失 apt 包时局部使用 sudo。
- macOS launchd 不再属于受支持的安装流程；卸载器仅保留旧 plist 的兼容清理。

## 安装入口

远程一行入口：

```bash
curl -fsSL https://raw.githubusercontent.com/zhangjianyong66/mihomo-manager/master/scripts/bootstrap.sh | bash
```

`scripts/bootstrap.sh` 下载 `MM_REF` 指定的 GitHub 源码归档到临时目录，再调用归档内的 `scripts/install.sh`。默认 `MM_REF=master`，退出时必须清理临时源码。

本地入口：

```bash
make install
```

`Makefile` 只做薄包装，安装事实来源是 `scripts/install.sh`。不要重新引入只影响帮助文本、不影响真实路径的 `PREFIX` 参数。

## 依赖与 Go

- 安装器检查并按需安装 `ca-certificates`、`curl`、`tar`、`gzip`、`procps`、`jq`、`coreutils`。
- `procps` 提供 Go 版运行时使用的 `pgrep`/`pkill`。
- 系统 Go 版本不低于 1.22 时直接复用。
- 否则下载并校验官方 Go 1.26.4 到 `~/.local/share/mihomo-manager/toolchains/go1.26.4`，不覆盖系统 Go。
- 构建使用 `CGO_ENABLED=0`，不要求 C 编译器。

## mm 与 mihomo core

- `mm` 从当前源码构建，经 `--help` 冒烟后原子替换为 `~/.local/bin/mm` 普通文件。
- 安装产物不得软链接到仓库 `bin/mm`；移动或删除仓库不应影响已安装命令。
- mihomo core 默认固定为 `v1.19.28`，默认路径 `~/.local/bin/mihomo`，可用 `MIHOMO_VERSION`/`MIHOMO_BIN` 覆盖。
- amd64 使用 `mihomo-linux-amd64-v1-<version>.gz`，arm64 使用 `mihomo-linux-arm64-<version>.gz`。
- 必须从 MetaCubeX/mihomo Release API 读取资产 URL 与 SHA-256 digest，校验、版本冒烟通过后才能替换。
- 已有 core 版本不低于目标版本时不降级；替换旧版或无法识别的 core 前写入 `mihomo.bak`。

## 配置与 PATH

- 默认配置目录为 `~/.config/mihomo`，仍可通过 `CONFIG_DIR` 覆盖。
- `config.yaml` 不存在时创建最小 `DIRECT` 配置并执行 mihomo 配置测试；已有配置只验证、不改写。
- 安装器不启动 core、不修改系统代理。
- Bash/Zsh 缺少等效 PATH 配置时，安装器使用稳定标记块写入 `~/.bashrc` 或 `~/.zshrc`；重复安装不得重复追加。
- 脚本不能修改父 shell，安装结束后应提示重开终端或临时 export PATH。

## 状态与卸载

安装状态位于 `~/.local/share/mihomo-manager/install-state`，只记录路径、版本和归属等非敏感信息，用于安全卸载。

```bash
make uninstall
./scripts/uninstall.sh --purge
./scripts/uninstall.sh --purge-config
```

- 默认删除 `mm`、隔离 Go 和安装器 PATH 块，保留 core 与配置。
- `--purge` 仅删除状态确认由安装器管理且位于默认路径的 core。
- `--purge-config` 才删除配置目录，并需交互确认或 `--yes`。
- 卸载器兼容删除旧版 `~/.local/bin/mm` 软链接。

## 网络与下载源

- curl 自然遵循 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`。
- `MM_GITHUB_BASE_URL`、`MM_GITHUB_API_BASE_URL`、`MM_GO_DOWNLOAD_BASE_URL` 可显式覆盖下载地址。
- 不允许在失败后静默切换第三方镜像。

## 验证

```bash
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
go test ./...
go build -o /tmp/mm ./cmd/mm
```

安装测试必须使用临时 HOME、命令替身和本地夹具，不得改写真实 `~/.config/mihomo`，不得启动真实 core 或调用真实 apt/sudo。

## 场景：维护一键安装协议

### 1. 范围 / 触发条件

修改 `scripts/bootstrap.sh`、`scripts/install.sh`、`scripts/uninstall.sh`，或变更安装版本、路径、参数、环境变量、状态字段时，必须按本节进行协议级检查。原因是这些文件跨越网络下载、系统包管理、用户 shell、二进制替换和配置持久化边界。

### 2. 命令签名

```text
scripts/bootstrap.sh [--yes] [--force-core]
scripts/install.sh [--yes] [--force-core]
scripts/uninstall.sh [--purge] [--purge-config] [--yes]
```

- 远程引导只透传安装参数，不独立实现依赖/core/mm 安装。
- `make install` 与 `make uninstall` 分别是上述本地脚本的无参数包装。

### 3. 环境与状态契约

| 名称 | 是否可选 | 契约 |
|------|----------|------|
| `MM_REF` | 是 | 远程源码引用，默认 `master` |
| `MIHOMO_VERSION` | 是 | `X.Y.Z` 或 `vX.Y.Z`，默认 `v1.19.28` |
| `MM_ASSUME_YES` | 是 | `1/true/yes` 等价于 `--yes` |
| `MM_GITHUB_BASE_URL` | 是 | GitHub 源码/资产下载基地址 |
| `MM_GITHUB_API_BASE_URL` | 是 | GitHub Release API 基地址 |
| `MM_GO_DOWNLOAD_BASE_URL` | 是 | Go 归档下载基地址 |
| `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY` | 是 | 由 curl/Go 自然继承，不自动改写 |
| `install-state` | 内部 | 只允许非敏感 `key=value`；记录 mm、Go、PATH、core 归属和配置路径 |

状态字段发生增删时，安装写入和卸载读取必须在同一变更中更新，并补回归测试。

### 4. 校验与错误矩阵

| 条件 | 必须行为 |
|------|----------|
| EUID 为 0 | 在持久化变更前失败，提示普通用户运行 |
| 非 Ubuntu/Debian 或非 amd64/arm64 | 无变更失败 |
| 缺失 apt 包且未确认 | 不调用 sudo，失败或由用户取消 |
| Go 下载摘要不匹配 | 不替换隔离工具链，不构建 mm |
| core 资产缺少 SHA-256 | 不下载或替换 core |
| core 校验/版本冒烟失败 | 保留已有 core，不生成备份替换 |
| mm 构建或 `--help` 失败 | 保留已有 mm |
| 已有 `config.yaml` | 只验证，不改写 |
| PATH 标记块不完整 | 卸载时保留文件并警告，不允许截断 shell 配置 |
| `--purge` 无归属或路径非默认 | 保留 core 与状态并警告 |

### 5. Good / Base / Bad 用例

- Good：Ubuntu amd64、系统 Go 1.22+、无既有 core；安装器校验并安装 core，构建独立 mm，创建 DIRECT 配置。
- Base：已有更高版本 core 和有效配置；安装器保留二者，只更新 mm 和必要 PATH。
- Bad：Release API 返回的 digest 与下载文件不一致；安装器非零退出，旧 mm/core/config 保持原样。

### 6. 必需测试与断言点

- `bash scripts/tests/test_install.sh`：断言预检无变更、Go 选择、core 不降级/强制备份、SHA 拒绝、PATH 幂等与异常块保护、卸载归属。
- 隔离 HOME 端到端：断言 `mm --help`、`mihomo -t` 成功；默认卸载后 mm 消失、core/config 保留、core 归属状态仍在。
- `bash -n ...`：覆盖所有 Shell 脚本。
- `go test ./...` 与临时路径 `go build`：保证安装改动不影响 Go 产品。

### 7. Wrong vs Correct

错误：下载后直接覆盖，失败会破坏现有安装。

```bash
curl -fsSL "$url" -o "$HOME/.local/bin/mihomo"
```

正确：临时下载、摘要校验、版本冒烟、备份后在同一文件系统原子替换。

```bash
download_file "$url" "$archive"
verify_sha256 "$archive" "$expected"
gzip -dc "$archive" >"$temp_bin"
"$temp_bin" -v
cp -p "$target" "$target.bak"
mv -f "$temp_bin" "$target"
```
