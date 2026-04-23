# Mihomo Manager

一个简洁的 Mihomo 代理管理工具，用于管理代理服务、节点切换、订阅更新等。

## 功能

- 服务管理：启动、停止、重启、状态查看
- 节点管理：列出节点、切换节点、测试延迟、自动选择最快节点
- 订阅管理：更新订阅、保存订阅 URL
- 白名单管理：添加/移除直连域名
- 分流配置：一键设置大陆直连、其他走代理
- 配置管理：备份、恢复、编辑配置
- 监控服务：自动重启异常退出的服务

## 安装

```bash
# 克隆项目
git clone https://github.com/zhangjianyong66/mihomo-manager.git
cd mihomo-manager

# 安装
make install
# 或
./scripts/install.sh
```

安装后目录结构：
```
~/.local/bin/mm              # 命令符号链接
~/.config/mihomo/            # 配置目录
├── config.yaml              # 主配置文件
├── mihomo.log               # 运行日志
├── mihomo-monitor.log       # 监控日志
├── subscription.url         # 订阅 URL
└── node_speed.txt           # 节点速度记录
~/Library/LaunchAgents/
└── com.mihomo.monitor.plist # 监控服务
```

## 卸载

```bash
make uninstall
# 或
./scripts/uninstall.sh
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
| `test-current` | `tc` | 测试当前生效节点延迟 |
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

### 分流配置

| 命令 | 说明 |
|------|------|
| `rcn` | 一键配置中国大陆流量直连，其他流量走代理组 `GLOBAL` |

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

## 快速分流

执行以下命令即可启用「中国大陆直连，其他流量走代理」：

```bash
mm rcn
```

该命令会自动：
- 备份当前配置
- 写入 geodata 在线规则源（MetaCubeX）
- 写入 CN 直连规则
- 校验并热重载配置

## 环境变量

可通过环境变量自定义端口：

```bash
export MIHOMO_MIXED_PORT=10808  # 混合代理端口
export MIHOMO_SOCKS_PORT=7891   # SOCKS 端口
export MIHOMO_API_PORT=9090     # API 端口
```

## 监控服务

监控服务每 5 分钟检查一次：
- 检查 mihomo 进程是否存在
- 检查代理端口是否监听
- 服务异常时自动重启

查看监控日志：
```bash
tail -f ~/.config/mihomo/mihomo-monitor.log
```

## 依赖

- `curl` 或 `wget` - 下载订阅
- `python3` - 解析配置
- `lsof` - 端口检测

## License

MIT
