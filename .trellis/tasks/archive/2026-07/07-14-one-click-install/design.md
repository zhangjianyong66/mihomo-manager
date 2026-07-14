# 技术设计：一键安装依赖并注册 mm 命令

## 1. 设计概览

采用“远程引导脚本 + 仓库内统一安装器 + 状态化卸载器”的三段结构：

```text
README 一行命令
    -> scripts/bootstrap.sh
        -> 下载 MM_REF 源码归档到临时目录
        -> 调用归档内 scripts/install.sh
            -> 环境/依赖/Go/core/配置/PATH/mm 安装与验证

make install
    -> scripts/install.sh（复用同一逻辑）

make uninstall
    -> scripts/uninstall.sh（读取安装状态，安全清理）
```

该任务是一个强耦合安装流程，不拆分父子任务：引导、安装、卸载、文档必须在同一验收周期内保持协议一致。

## 2. 文件边界

- `scripts/bootstrap.sh`：只负责远程源码归档获取、临时目录生命周期和参数透传，不复制依赖安装逻辑。
- `scripts/install.sh`：安装事实来源；本地与远程入口都调用它。
- `scripts/uninstall.sh`：按安装状态清理独立二进制、隔离工具链、PATH 标记和可选 core/config。
- `scripts/tests/test_install.sh`：以临时 HOME、命令替身和本地下载夹具验证平台检测、幂等、失败保护与卸载边界，不触碰真实用户配置。
- `Makefile`：保持薄包装，只暴露 install/uninstall/help，删除误导性的 PREFIX 描述。
- `README.md`、`.trellis/spec/backend/deployment.md`、`AGENTS.md`：同步新安装契约。

不修改 Go 业务包；安装能力属于 `scripts/` 边界。

## 3. 安装流程

### 3.1 预检顺序

在任何持久化变更前依次完成：

1. 参数解析：`--yes`、`--force-core`、`--help`。
2. root 拒绝。
3. 读取 `/etc/os-release`，仅允许 Ubuntu/Debian 或 `ID_LIKE` 包含 debian，且要求 `apt-get`。
4. 将 `dpkg --print-architecture` 或 `uname -m` 规范化为 `amd64`/`arm64`。
5. 计算用户目录：安装目录、配置目录、状态目录、隔离工具链目录。

预检失败不创建目录、不调用 sudo、不下载文件。

### 3.2 系统依赖

安装器按命令能力映射 apt 包，仅安装缺失项。首版最小集合预计为：

- `ca-certificates`：TLS 信任链。
- `curl`：源码、Go 和 core 下载。
- `tar`、`gzip`：归档解压。
- `procps`：Go 版运行时所需 `pgrep`/`pkill`。
- `jq`：可靠解析 GitHub Release API 中的资产 URL 与 SHA-256 digest。

系统通常自带的 `bash`、`coreutils`、`sed`、`awk` 仍在预检中验证；缺失时明确报错。缺失 apt 包统一展示并确认一次；无 TTY 且未传 `--yes` 时失败。

### 3.3 Go 工具链

- 用 `go version` 提取 major/minor，满足 `>=1.22` 时直接使用。
- 否则选择固定 `go1.26.4.linux-<arch>.tar.gz`。
- SHA-256 作为安装器常量按架构维护：
  - amd64：`1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f`
  - arm64：`ef758ae7c6cf9267c9c0ef080b8965f453d89ab2d25d9eb22de4405925238768`
- 下载到状态目录内临时文件，校验后解压到版本化临时目录，再原子替换目标工具链目录。
- 构建时显式使用选定的 Go 路径，并设置 `CGO_ENABLED=0`，避免引入 C 编译器依赖。

## 4. mihomo core 获取与版本策略

### 4.1 资产映射

- amd64：`mihomo-linux-amd64-v1-<version>.gz`，使用兼容性更高的 v1 基线构建。
- arm64：`mihomo-linux-arm64-<version>.gz`。

`MIHOMO_VERSION` 统一规范为带 `v` 前缀的语义版本。安装器查询 MetaCubeX/mihomo 对应 Release API，用 `jq` 按完整资产名选择 `browser_download_url` 和 `digest`；digest 必须是 `sha256:<hex>`。

### 4.2 替换规则

- `--force-core`：无条件下载、校验并替换目标版本。
- 非强制：解析现有 `mihomo -v` 的第一个 `vX.Y.Z`。
  - 现有版本 `>=` 目标版本：保留。
  - 现有版本更低或无法解析：复制为固定备份 `mihomo.bak` 后替换。
- 新文件先写到与目标相同文件系统的临时路径，设置执行权限并执行 `mihomo -v` 冒烟，再用 `mv` 原子替换。
- 下载、摘要缺失、校验或冒烟失败均只删除临时文件，不触碰现有 core。

## 5. mm 构建与安装

1. 在源码根目录运行 `CGO_ENABLED=0 <go> build -trimpath -o <临时 mm> ./cmd/mm`。
2. 对临时二进制执行 `--help`。
3. 创建 `$HOME/.local/bin`，将临时文件移动为 `$HOME/.local/bin/mm.new`。
4. 再次检查执行权限后原子重命名为 `mm`。

不再写仓库 `bin/mm`，也不建立到仓库的软链接。旧软链接在成功构建后由原子替换自然迁移为普通文件。

## 6. 默认配置

仅当目标 `config.yaml` 不存在时创建：

```yaml
mixed-port: 10808
socks-port: 7891
allow-lan: false
mode: rule
log-level: info
external-controller: 127.0.0.1:9090
rules:
  - MATCH,DIRECT
```

配置先写临时文件，使用目标 core 执行 `-t -f`，通过后再移动到 `config.yaml`。已有配置只测试、不改写；已有配置测试失败应警告，但不阻止 `mm` 二进制安装，最终安装结果明确标记“程序已安装、现有配置需修复”。

## 7. PATH 修改

- 目标 shell 由 `basename "$SHELL"` 判断。
- Bash 写 `~/.bashrc`，Zsh 写 `~/.zshrc`。
- 如果当前 PATH 已包含 `$HOME/.local/bin`，或目标文件已有等效表达式，则不写入。
- 写入块使用稳定标记：

```sh
# >>> mihomo-manager PATH >>>
export PATH="$HOME/.local/bin:$PATH"
# <<< mihomo-manager PATH <<<
```

- 状态文件记录实际修改的 rc 文件；卸载仅删除完整标记块。
- 未识别 shell 时仅提示手动命令。

## 8. 状态与卸载

状态目录：`$HOME/.local/share/mihomo-manager`。状态文件使用受控 `key=value` 格式，只记录非敏感信息：安装器版本、mm 路径、隔离 Go 路径、PATH rc 文件、core 是否由安装器管理及 core 路径/版本。

卸载行为：

- 默认删除 `mm`、隔离 Go 和安装器 PATH 块。
- 默认保留 core、`mihomo.bak` 和配置。
- `--purge` 仅在状态确认 `core_managed=1` 且路径为预期默认路径时删除 core。
- `--purge-config` 删除配置目录前再次展示路径并确认；`--yes` 可跳过确认。
- 对旧版本软链接安装兼容删除。
- macOS launchd 仅保留旧卸载清理的兼容代码，不新增安装行为，也不宣称支持 macOS。

## 9. 远程引导与网络

`bootstrap.sh` 使用 `MM_REF` 组装仓库归档 URL，下载后 `tar --strip-components=1` 解压，调用 `scripts/install.sh` 并透传参数。使用 `trap` 清理临时目录。

- 标准代理变量由 curl 自然继承。
- `MM_GITHUB_BASE_URL` 覆盖 GitHub 网页/下载基地址。
- `MM_GO_DOWNLOAD_BASE_URL` 覆盖 Go 下载基地址。
- GitHub API 默认独立使用 `https://api.github.com`；为便于受控环境，可增加 `MM_GITHUB_API_BASE_URL`，文档明确其用途。
- 不自动尝试任何第三方镜像。

## 10. 失败一致性与回滚

- 所有替换先生成临时文件、校验和冒烟，再原子移动。
- 配置、core、mm 互不以“已下载”作为成功标志；只有各自最终移动后才更新状态。
- 状态文件最后写入，并用临时文件原子替换。
- 安装中断时清理临时文件，但保留既有 mm、core、配置和备份。
- apt 安装属于系统包管理事务，无法由脚本回滚；错误信息需区分“系统依赖已安装”和“应用安装未完成”。

## 11. 测试策略

- `bash -n` 覆盖新增和修改的 Shell 脚本。
- 安装脚本按函数组织，并提供只加载函数/测试替身入口；测试使用临时 HOME、伪造 `/etc/os-release` 路径、架构、命令和本地下载文件。
- 覆盖：平台/架构/root 拒绝、Go 版本选择、SHA 失败不替换、core 不降级、PATH 幂等、独立 mm 替换、默认配置不覆盖、卸载归属判断。
- 运行 `go test ./...`，确保安装变更未影响 Go 产品。
- 在当前 Ubuntu amd64 环境执行一次受控本地安装验证；任何涉及真实 `$HOME` 的验证前先备份并确认不会覆盖现有配置/core，优先使用临时 HOME 和路径覆盖。

## 12. 兼容性与取舍

- 源码编译换取无需建设 Release 流程，但首次安装较慢且需下载 Go。
- 用户级安装降低权限风险，但当前父 shell 无法被脚本直接更新，需要重开终端或手动 export。
- 固定 core/Go 版本提升可复现性，版本升级需在仓库中显式更新常量和校验值。
- `jq` 只用于安装阶段的可信 JSON 解析，换取对 Release API 的稳健处理。
