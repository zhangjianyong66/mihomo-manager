# v2rayN 能力对齐矩阵

调研日期：2026-07-20。

对照来源：

- v2rayN 仓库与 README：https://github.com/2dust/v2rayN
- v2rayN 支持内核列表：https://github.com/2dust/v2rayN/wiki/List-of-supported-cores
- v2rayN 7.23.4 发布说明：https://github.com/2dust/v2rayN/releases/tag/7.23.4
- 当前项目 `README.md`、`cmd/mm`、`internal/config`、`internal/mihomo`、`internal/tui` 和现有测试。

状态定义：

- **已有**：当前 Go 产品已经提供主要用户结果。
- **部分**：已有基础行为，但范围、稳定接口或可靠性未达到对齐目标。
- **缺失**：当前没有对应产品能力。
- **排除**：不适合终端定位，已明确永久不实现。

| 能力域 | v2rayN 基线 | 当前 mm | 状态 | 目标里程碑 | 对齐结果 |
|---|---|---|---|---|---|
| 稳定非交互接口 | GUI 操作与内部服务层 | 已有 A6 legacy 业务命令、`mm/v1` JSON/NDJSON、错误/退出码和脱敏契约 | 已有 | 2.0 Alpha | core/config/node/subscription/route/log 已提供稳定 CLI |
| 交互界面 | 完整桌面 GUI | Bubble Tea TUI | 部分 | 2.0 Alpha | TUI 保留，但只调用共享应用服务 |
| 后台管理 | 常驻客户端托管 core | mm 退出后 core 可独立运行，无统一 daemon | 部分 | 2.0 Alpha | 用户级 daemon、Unix socket、systemd user service |
| Core 生命周期 | 启停、重启、切换、日志 | `core status|start|stop|restart|reload|logs` 已经 daemon IPC | 已有 | 2.0 Alpha | 已纳入 daemon 单写者和精确进程托管模型 |
| 多内核 | Xray/v2fly、mihomo、sing-box 及其他 | 只支持 mihomo | 缺失 | 3.0 | 先 Xray，后 sing-box；其他按需求评估 |
| 配置档案 | 多服务器、分组和完整配置 | 单份 `config.yaml` | 缺失 | 2.0 Beta | 多命名档案、单活动实例、外部完整配置托管 |
| 多订阅 | 多订阅、附加订阅、更新策略 | 单 URL | 部分 | 2.0 Beta | 来源隔离、事务更新、稳定 ID、失败保留旧版 |
| 节点格式 | VMess/VLESS/SS/Trojan/AnyTLS/Hysteria/TUIC/WireGuard 等 | URI 支持 VLESS/VMess/Trojan/SS；完整 YAML 可导入 | 部分 | 2.0 Beta | 解析器注册表；优先覆盖 mihomo 可表达的常用协议 |
| 节点和代理组 | 列表、筛选、选择、分组 | A6 已有 group/node list/show/select；来源模型待 Beta | 部分 | 2.0 Alpha/Beta | Alpha 等价迁移完成；Beta 使用来源与稳定 ID 管理 |
| 延迟与速度测试 | 多种延迟/速度测试 | A6 已有并发延迟测试、取消、超时和 NDJSON | 部分 | 2.0 Alpha/2.1 | Alpha 等价迁移完成；2.1 增加真实吞吐测试 |
| 配置生成 | 按 core 生成运行配置 | 订阅直接重写 mihomo YAML | 部分 | 2.0 Alpha/Beta | 规范化模型、适配器生成、覆盖层、render/diff/validate |
| 完整配置 | 支持自定义完整配置 | 可直接使用完整 mihomo YAML，但更新可能重写 | 部分 | 2.0 Alpha | 外部配置只托管不回写，与生成配置隔离 |
| 路由规则 | 预设、规则集、自定义规则 | 白名单、CN 直连/其他 GLOBAL、路由诊断 | 部分 | 2.0 Beta | 规则 CRUD、优先级、预设、规则资源和命中诊断 |
| DNS | 可配置 Xray/sing-box/mihomo DNS | 没有一等 CLI/TUI 模型 | 缺失 | 2.0 Beta | 档案级 DNS 策略、预设、覆盖层和验证 |
| TUN | 多 core TUN | 缺失 | 缺失 | 2.1 | 普通用户 daemon，显式一次性提权配置，可撤销 |
| 系统代理 | 桌面系统代理、PAC | 缺失；本机当前由外部工具设置 GNOME 代理 | 缺失 | 2.3 | 通用 shell env、GNOME 适配器；其他桌面后续 |
| 连接观察 | 连接列表、关闭连接 | 缺失 | 缺失 | 2.1 | 实时内存视图，不持久化目标明细 |
| 流量统计 | 实时与累计流量 | 缺失 | 缺失 | 2.1 | 实时数据和 30 天档案/节点/日期聚合 |
| 日志 | core 与客户端日志 | A6 已有 tail/follow、正则过滤、取消和脱敏 NDJSON | 部分 | 2.0 Alpha/2.1 | Alpha 等价迁移完成；2.1 增加轮转和统一查询 |
| 自动恢复 | 客户端托管 core | 启动脚本和遗留 monitor 能力分散 | 部分 | 2.0 Alpha | daemon 退避重启、熔断、明确故障状态 |
| 应用更新 | 客户端自更新 | 安装器可更新 mm，但无应用内更新 | 部分 | 2.2 | 默认检查，显式验证更新和回滚 |
| Core 更新 | 支持主要 core 更新 | 安装器可校验并安装固定 mihomo 版本 | 部分 | 2.2 | daemon 协调更新、验证、原子切换、权限失效提示 |
| GEO/规则资源 | v2ray/sing-box 规则资源 | 安装器不管理规则资源 | 缺失 | 2.2 | 独立检查、更新、校验和回滚 |
| 定时任务 | 订阅和资源更新 | 缺失 | 缺失 | 2.2 | daemon 调度，默认只检查，逐项选择自动安装 |
| 备份恢复 | 配置备份与恢复 | 单配置 `.bak` | 部分 | 2.2 | 版本化加密逻辑备份、预览、恢复点和回滚 |
| WebDAV 同步 | 已支持 | 缺失 | 缺失 | 2.2 | 作为首个传输适配器，不绑定备份格式 |
| 二维码 | 分享与扫描 | 缺失 | 缺失 | 2.3 | 终端显示、图片文件解析；不做摄像头扫描 |
| 平台 | Windows/Linux/macOS，多架构 | Ubuntu/Debian amd64/arm64 | 部分 | 长期 | 首期保持现状；Fedora/RHEL 后续，Windows/macOS 不承诺 |
| 国际化 | 多语言资源 | TUI 中文为主、帮助文本中英混合 | 部分 | 未承诺 | 不作为 v2rayN 对齐完成门槛，按用户需求单独规划 |
| 托盘/窗口/主题 | 完整桌面体验 | 不适用 | 排除 | 永久排除 | 不实现 |
| GUI 热键/剪贴板监听 | 桌面快捷操作 | 不适用 | 排除 | 永久排除 | 不实现 |

## 总结

当前项目的核心优势是 mihomo 运行控制、节点切换、基础订阅和路由已经可用；主要差距不是协议数量，而是稳定 CLI、统一后台、规范化数据、配置生成、可观测性和安全升级。先解决这些横向基础，再接入第二个 core，能避免每项能力重复实现。
