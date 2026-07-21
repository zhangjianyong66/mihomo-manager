# Mihomo Manager（终端 CLI）

`mm` 是使用 Go 编写的终端优先 Mihomo 管理器，提供脚本化 CLI 和 Bubble Tea TUI。

## 一键安装

首版安装器支持 Ubuntu/Debian 的 `amd64` 和 `arm64`，请以普通用户执行，不要对整个脚本使用 `sudo`。

```bash
curl -fsSL https://raw.githubusercontent.com/zhangjianyong66/mihomo-manager/master/scripts/bootstrap.sh | bash
```

安装器会：

- 检查并按需通过 `apt` 安装 `curl`、`tar`、`gzip`、`procps`、`jq` 等依赖；
- 复用 Go 1.22+，或安装隔离的官方 Go 1.26.4；
- 从源码构建独立的 `$HOME/.local/bin/mm`；
- 下载并校验官方 mihomo core，默认固定为 `v1.19.28`；
- 下载并校验固定版本的 CN domain/IP `.mrs` 到 `$CONFIG_DIR/rulesets`；
- 在缺少配置时创建最小 `DIRECT` 配置；
- 安装并启用 systemd user manager daemon，但保持 mihomo core 为停止状态；
- 仅对本次新建的配置自动执行 legacy 注册，已有配置只给出迁移提示；
- 为 Bash/Zsh 幂等配置 `$HOME/.local/bin`。

安装过程不会启动 mihomo，也不会修改系统代理。完成后重开终端并运行：

```bash
mm
```

### 审阅脚本后安装

直接执行远程脚本存在供应链风险。也可以先下载、审阅，再运行：

```bash
curl -fsSLo /tmp/mm-bootstrap.sh \
  https://raw.githubusercontent.com/zhangjianyong66/mihomo-manager/master/scripts/bootstrap.sh
less /tmp/mm-bootstrap.sh
bash /tmp/mm-bootstrap.sh
```

### 无交互安装

```bash
curl -fsSL https://raw.githubusercontent.com/zhangjianyong66/mihomo-manager/master/scripts/bootstrap.sh \
  | bash -s -- --yes
```

### 从本地仓库安装

```bash
make install
```

本地入口与远程入口复用同一个安装器。安装后的 `mm` 是独立文件，移动或删除源码仓库不会使其失效。

## 安装选项

```bash
./scripts/install.sh --help
```

- `--yes`：跳过 apt 安装确认。
- `--force-core`：强制重新安装目标 mihomo core。
- `MM_REF`：远程安装使用的源码分支、标签或提交，默认 `master`。
- `MIHOMO_VERSION`：指定 core 版本，默认 `v1.19.28`。
- `MM_GITHUB_BASE_URL`：覆盖 GitHub 下载基地址。
- `MM_GITHUB_API_BASE_URL`：覆盖 GitHub API 基地址。
- `MM_GO_DOWNLOAD_BASE_URL`：覆盖 Go 下载基地址。
- `MM_RULESET_BASE_URL`、`MM_RULESET_REF`：覆盖 CN 规则集来源；覆盖任一项时必须同时提供下面两个可信摘要。
- `MM_RULESET_DOMAIN_SHA256`、`MM_RULESET_IP_SHA256`：成对覆盖 domain/IP 规则集 SHA-256。
- `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`：标准下载代理变量。

安装器不会自动切换第三方镜像。覆盖下载源时，应自行确认来源可信。
首次下载或校验失败且没有 install-state 可验证缓存时，安装直接失败；升级失败时只会保留摘要和格式仍与 install-state 一致的旧缓存。

## 卸载

默认卸载 `mm`、安装器专用 Go 工具链和安装器写入的 PATH 片段，保留 mihomo core 与用户配置：

```bash
make uninstall
```

可选清理：

```bash
./scripts/uninstall.sh --purge          # 删除可确认由安装器管理的 core
./scripts/uninstall.sh --purge-config   # 显式删除 ~/.config/mihomo
```

## 使用

```bash
mm
mm tui
mm daemon run
mm daemon status --output json
mm daemon enable
mm migrate plan --output json
mm migrate status
mm core status --output json
mm config validate
mm group list
mm node test --group GLOBAL --output ndjson
mm subscription show
mm route whitelist list
```

两个入口打开相同的交互式终端界面。非交互冒烟检查：

```bash
mm --help
mm tui --help
mm daemon --help
```

`mm daemon run` 是不依赖 systemd 的前台 manager daemon。daemon 会装配 mihomo adapter 和单实例 supervisor，但不会自动启动 mihomo；CLI 在 daemon 不可用时不会回退为直接写 SQLite、配置或控制 core。managed generation 默认写入 `${XDG_DATA_HOME:-~/.local/share}/mihomo-manager/generations`，core 日志与 runtime metadata 写入 `${XDG_STATE_HOME:-~/.local/state}/mihomo-manager/core`。可用 `XDG_DATA_HOME`、`XDG_STATE_HOME`、`XDG_RUNTIME_DIR`、`XDG_CONFIG_HOME` 隔离测试环境。

安装升级只有在受管 core 明确为 `stopped` 且 stop 前复核仍为 `stopped` 时才重启 daemon；其他状态不会停止现有 daemon，新二进制在后续 daemon 重启时生效。systemd user 会话不可用时只保留已安装 unit，并提示使用 `mm daemon run`，不会启用 linger 或创建临时后台进程。

通过安装器设置的 `CONFIG_DIR`、`MIHOMO_BIN` 和 `MIHOMO_API_PORT` 会写入受管 systemd service 环境，并在后续未显式覆盖的 `mm daemon enable` 中保留，确保 daemon 重启后继续使用同一 legacy 配置和 core 路径。

2.0 Alpha 的业务 CLI 默认作用于唯一活动 legacy 档案，也可用 `--profile <id>` 显式指定。查询命令使用 `--output table|json`；`node test` 和 `core logs --follow` 使用 `--output text|ndjson`。订阅地址、节点 URI、UUID、密码和日志凭据默认脱敏，只有显式 `--show-secrets` 才显示完整值。

当前脚本化命令包括：

- `core status|start|stop|restart|reload|logs`
- `config validate|backup|restore|edit`
- `group list|show|select`、`node list|select|test`
- legacy `subscription show|set|update`
- legacy `route whitelist list|add|edit|remove`、`route preset cn`、`route diagnose`

## 快捷键

- `↑/↓`：移动选择。
- `Enter`：确认或进入。
- `Esc`：返回。
- `q`：在主菜单退出。

## 功能

- 服务管理：状态、启动、停止、重启、重载、配置测试、日志。
- 节点管理：当前节点、切换、测速、切换最快节点。
- 订阅管理：保存、查看和更新订阅。
- 白名单：添加、删除和查看直连域名。
- 配置管理：备份、恢复、编辑及应用分流规则。

## 注意事项

- `migrate plan|apply|status|rollback` 负责注册活动 legacy 档案；所有迁移和 A6 业务写入均经 daemon，`rollback` 必须显式指定恢复点，旧 YAML 不会自动转换。
- 2.0 的领域模型、SQLite schema/迁移和事务仓储底座已接入 daemon；legacy profile 仅由迁移创建，无法确认归属的文件不会猜测为 managed。
- 2.0 的 mihomo adapter 已支持原生验证、loopback controller 就绪检查、精确进程停止和失败恢复；A6 已接入 legacy core/config/node/subscription/route/log 命令，多档案、多订阅和完整 managed 配置仍属于 Beta。
- YAML 写回后字段顺序和注释可能变化。
- 节点测速并发固定较低，以提高稳定性。
