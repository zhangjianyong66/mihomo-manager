# Mihomo Manager

一个简洁的 Mihomo 代理管理工具，用于管理代理服务、节点切换、订阅更新等。

## 功能

- 服务管理：启动、停止、重启、状态查看
- 节点管理：列出节点、切换节点、测试延迟、自动选择最快节点
- 订阅管理：更新订阅、保存订阅 URL
- 白名单管理：添加/移除直连域名
- 配置管理：备份、恢复、编辑配置

## 安装

```bash
# 克隆或下载项目
cd ~/project
git clone <repo-url> mihomo-manager

# 运行安装脚本
cd mihomo-manager
./scripts/install.sh

# 或手动安装
ln -sf $(pwd)/bin/mihomo-manager ~/.local/bin/mm
```

## 使用方法

```bash
mm <命令> [选项]
```

### 服务管理

| 命令 | 别名 | 说明 |
|------|------|------|
| `status` | `st` | 查看服务状态 |
| `start` | - | 启动服务 |
| `stop` | `sp` | 停止服务 |
| `restart` | `r` | 重启服务 |
| `reload` | `rl` | 热重载配置 |
| `logs` | `l` | 查看实时日志 |
| `test` | `t` | 测试配置文件 |

### 节点管理

| 命令 | 别名 | 说明 |
|------|------|------|
| `list` | `ls` | 列出所有节点 |
| `current` | `c` | 显示当前节点 |
| `switch <节点>` | `swc` | 切换到指定节点 |
| `test-nodes` | `tn` | 测试所有节点延迟 |
| `fastest` | `f` | 自动切换到最快节点 |

### 订阅管理

| 命令 | 别名 | 说明 |
|------|------|------|
| `update-sub -u <URL>` | `us` | 更新订阅配置 |
| `update` | `upd` | 从保存的 URL 更新 |
| `save-url <URL>` | `su` | 保存订阅 URL |
| `show-url` | `sw` | 显示保存的订阅 URL |

### 白名单管理

| 命令 | 说明 |
|------|------|
| `whitelist add <域名>` | 添加域名到白名单 |
| `whitelist remove <域名>` | 从白名单移除域名 |
| `whitelist list` | 列出白名单 |

### 配置管理

| 命令 | 别名 | 说明 |
|------|------|------|
| `backup` | `bk` | 备份当前配置 |
| `restore` | `rs` | 恢复备份配置 |
| `edit` | `e` | 编辑配置文件 |

## 选项

| 选项 | 说明 |
|------|------|
| `-u, --url URL` | 指定订阅链接 |
| `--save-url` | 同时保存订阅 URL |
| `--proxy` | 使用代理下载订阅 |
| `-h, --help` | 显示帮助 |

## 环境变量

可通过环境变量自定义端口：

```bash
export MIHOMO_MIXED_PORT=10808  # 混合代理端口
export MIHOMO_SOCKS_PORT=7891   # SOCKS 端口
export MIHOMO_API_PORT=9090     # API 端口
```

## 配置文件

- 配置目录: `~/.config/mihomo/`
- 主配置文件: `config.yaml`
- 订阅 URL: `subscription.url`
- 节点速度: `node_speed.txt`

## 依赖

- `curl` 或 `wget` - 下载订阅
- `python3` - 解析配置
- `lsof` - 端口检测

## License

MIT