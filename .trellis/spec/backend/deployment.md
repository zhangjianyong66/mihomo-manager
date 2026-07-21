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
- 安装器固定下载 `MetaCubeX/meta-rules-dat` commit `32ae0e8658ca541374b721efcee84955e8a59755` 的 `geo/geosite/cn.mrs` 与 `geo/geoip/cn.mrs`，SHA-256 分别为 `52c146262ef51dc23a84533a0d13f8addd031c61708a863d17cdb75cc3089ee4`、`206ad4cc22005976e8bfb50a869e5483cb81cc174a56c9a79c8a13e3e64e2eea`，安装路径为 `<CONFIG_DIR>/rulesets/cn-domain.mrs` 和 `cn-ip.mrs`。
- 规则集目录/文件权限为 `0700/0600`；两份文件先全部下载、SHA-256 与 mihomo provider 原生校验，再成对发布。第二份发布失败必须恢复完整旧版本。
- 首次无有效缓存失败时安装非零退出；升级仅可复用路径、摘要和格式均与 install-state 一致的旧缓存。
- `config.yaml` 不存在时创建最小 `DIRECT` 配置并执行 mihomo 配置测试；已有配置只验证、不改写。
- 安装器不启动 core、不修改系统代理。
- Bash/Zsh 缺少等效 PATH 配置时，安装器使用稳定标记块写入 `~/.bashrc` 或 `~/.zshrc`；重复安装不得重复追加。
- 脚本不能修改父 shell，安装结束后应提示重开终端或临时 export PATH。

## daemon 与 systemd user service

- `mm daemon run` 以普通用户前台运行 manager daemon；只打开 XDG manager database、Unix socket 和单实例锁，不启动 mihomo core。
- IPC socket 默认位于 `$XDG_RUNTIME_DIR/mihomo-manager/mm.sock`，缺失时回退到 `${XDG_STATE_HOME:-$HOME/.local/state}/mihomo-manager/run/mm.sock`；父目录 `0700`，socket/lock `0600`。
- systemd unit 模板 v2 由 Go 二进制嵌入并写入 `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/mm.socket` 与 `mm.service`。`mm.socket` 使用 `%t`、`Accept=no`、`RemoveOnStop=yes`，service 设置 `NoNewPrivileges=yes`、`UMask=0077`、失败退避，且只启动 `mm daemon run`。
- 安装器调用正式 daemon CLI 时显式传入 `CONFIG_DIR`、`MIHOMO_BIN`、`MIHOMO_API_PORT`；controller 将其写入受校验的 service `Environment=` 块，未显式带环境的后续 enable 保留已有块，避免 systemd 重启后回落到默认 legacy 路径。
- `mm daemon enable` 原子写入并保存未知/本地修改 unit 的备份；daemon-reload 或 enable 失败会恢复原文件。systemd 用户会话不可用时保留已校验 unit，返回“已安装未启用”和 `mm daemon run` 提示，不启用 linger 或 sudo。
- 一键安装在正式 `mm` 发布后调用 `daemon enable/start/status`。本次新建配置且 daemon 可用时自动执行 `migrate apply`；已有配置只提示 `migrate plan/apply`。
- 升级前通过旧 `mm daemon status --output json` 探测 core，并在 stop 前再次核对：只有 core stopped 才允许重启 daemon 加载新二进制，running/starting/degraded/未知状态均保持现有进程。

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
| `MM_RULESET_BASE_URL` | 是 | CN 规则集下载基地址，默认 MetaCubeX raw；覆盖时必须成对提供可信摘要 |
| `MM_RULESET_REF` | 是 | CN 规则集固定引用；覆盖时必须成对提供可信摘要 |
| `MM_RULESET_DOMAIN_SHA256` | 条件必填 | 自定义来源的 domain `.mrs` 64 位十六进制 SHA-256 |
| `MM_RULESET_IP_SHA256` | 条件必填 | 自定义来源的 IP `.mrs` 64 位十六进制 SHA-256 |
| `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY` | 是 | 由 curl/Go 自然继承，不自动改写 |
| `install-state` | 内部 | 只允许非敏感 `key=value`；记录 mm、Go、PATH、core 归属、配置路径，以及两份规则集的来源、引用、路径和摘要 |

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
| 任一规则集下载/摘要/格式失败且无有效缓存 | 不发布任一新规则集，安装失败 |
| 第二份规则集发布失败 | 恢复两份旧规则集，不留下混合版本 |
| 已有 `config.yaml` | 只验证，不改写 |
| PATH 标记块不完整 | 卸载时保留文件并警告，不允许截断 shell 配置 |
| `--purge` 无归属或路径非默认 | 保留 core 与状态并警告 |

### 5. Good / Base / Bad 用例

- Good：Ubuntu amd64、系统 Go 1.22+、无既有 core；安装器校验并安装 core，构建独立 mm，创建 DIRECT 配置。
- Base：已有更高版本 core 和有效配置；安装器保留二者，只更新 mm 和必要 PATH。
- Bad：Release API 返回的 digest 与下载文件不一致；安装器非零退出，旧 mm/core/config 保持原样。

### 6. 必需测试与断言点

- `bash scripts/tests/test_install.sh`：断言预检无变更、Go 选择、core 不降级/强制备份、规则集来源/摘要/事务回滚/缓存降级、daemon 分支、fresh migrate、PATH 幂等与卸载归属。
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
