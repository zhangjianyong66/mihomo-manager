# Mihomo Manager（交互式 CLI）

`mm` 是使用 Go 编写的交互式 Mihomo 管理器。

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
- 在缺少配置时创建最小 `DIRECT` 配置；
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
- `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`：标准下载代理变量。

安装器不会自动切换第三方镜像。覆盖下载源时，应自行确认来源可信。

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
```

两个入口打开相同的交互式终端界面。非交互冒烟检查：

```bash
mm --help
mm tui --help
```

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

- 当前只发布 TUI 入口；旧的非交互式业务子命令仍不支持，新的脚本化业务命令会按 2.0 路线逐步接入。
- 2.0 的领域模型、SQLite schema/迁移和事务仓储底座已进入代码库，但尚未接入 CLI/TUI 或创建默认用户数据库；现有 1.x 配置和运行行为不变。
- YAML 写回后字段顺序和注释可能变化。
- 节点测速并发固定较低，以提高稳定性。
