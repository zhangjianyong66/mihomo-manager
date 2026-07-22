# 检测并修复 mihomo 端口冲突

## Goal

避免 mihomo 与本机 v2rayN/xray 等程序争用监听端口：新安装默认不再使用已被 v2rayN 占用的 `10808`，core 启动前一次检测所有配置监听端口，冲突时向 CLI/TUI 返回可执行提示并阻止启动；用户可在 TUI 或非交互 CLI 中安全修改端口。

## Background

- 当前本机 `10808` 由 v2rayN 的 xray 占用，legacy 配置仍包含 `mixed-port: 10808`。
- 仓库 Go 订阅兼容逻辑缺失端口时已使用 `7890/7891/9090`，但安装器和旧 Shell 兼容层仍把 `10808` 作为 mixed 默认值。
- 既往会话已验证：端口冲突可能让 mihomo 进程和 controller 看似正常，但某个代理监听器实际未建立，因此仅依赖 readiness 不足以判定入口完整。
- 现有配置编辑使用 daemon GET 返回内容和 SHA-256，客户端本地编辑后 PUT expected SHA-256；daemon 原子写入、原生校验并在失败时恢复。
- core 切换和 restart 已由 daemon CoreManager 监督，并与其他写操作共享 Coordinator。

## Requirements

### Default ports

- 新建最小配置、旧 Shell 公共默认值和旧 Shell 完整 YAML 订阅缺失端口补全统一使用 `mixed-port: 7890`。
- `socks-port` 继续默认 `7891`，`external-controller` 继续默认 `127.0.0.1:9090`。
- 旧 Shell 兼容层继续支持 `MIHOMO_MIXED_PORT`、`MIHOMO_SOCKS_PORT` 和 `MIHOMO_API_PORT` 覆盖。
- 安装器不得因默认值变化自动覆盖已有配置。

### Startup preflight

- daemon 托管的每次 core start/restart/rollback start 都必须在创建 mihomo 进程前执行端口占用检查。
- 检查配置中的 `mixed-port`、`port`、`socks-port`、`redir-port`、`tproxy-port` 和 `external-controller`；值为 `0` 的可选代理端口视为禁用。
- 检查使用配置的有效绑定地址；HTTP/redir/controller 检查 TCP，mixed/socks/tproxy 检查其需要的 TCP/UDP 监听。
- 一次收集并返回全部冲突，至少包含配置字段、网络、主机和端口；不依赖或显示 PID、进程名，不调用 `ss`/`lsof`。
- 任一端口冲突必须阻止启动，错误码为稳定的 `PORT_CONFLICT`，CLI 退出码按冲突类返回 `4`。
- 提示必须明确建议修改配置端口或停止占用端口的程序；不得自动选择空闲端口或修改配置。
- 探测和实际进程 bind 之间仍存在不可消除的竞争窗口；mihomo 后续启动失败继续按现有 START/RESTORE 失败语义处理。

### Structured port management

- daemon 提供读取和修改 legacy 监听端口的版本化 IPC；所有读取默认解析唯一活动 legacy profile，也支持显式 profile。
- 读取返回六个字段的当前端口、是否必填/启用、core 状态、是否下次启动生效，以及最近一次启动留下的结构化冲突。
- 修改请求一次只更新一个白名单字段，不接受任意 YAML path。
- `mixed-port`、`port`、`socks-port`、`redir-port`、`tproxy-port` 接受 `0-65535`，其中 `0` 表示禁用；`external-controller` 只接受 `1-65535`，只替换端口并保留现有 loopback 主机。
- 修改后必须校验配置内端口范围、字段间重复和 controller 冲突，并执行 mihomo 原生验证。
- 写入必须沿用 expected SHA-256、`0600`、同目录原子发布、迁移 expected 摘要刷新和失败恢复契约。
- core stopped 时只保存配置并返回“下次启动生效”，不得启动 core 或探测 controller。
- core running 且运行 profile 匹配时，在共享 Coordinator 内执行受控重启；新配置启动失败时先恢复旧配置/expected 摘要，再恢复旧 RuntimeSpec。
- 恢复成功时返回原始修改/启动错误且 core 恢复 running；配置或旧 core 任一恢复失败时返回 `RESTORE_FAILED` 并把 core 标记为 failed。

### CLI and TUI

- CLI 提供 `mm config ports` 查询和 `mm config port set <field> <port>` 修改，支持既有 `--profile`、table/json 输出和退出码契约。
- TUI“配置管理”增加常驻“监听端口”入口，列表展示六个字段、当前端口/禁用状态和最近冲突标记。
- TUI 选择字段后使用现有输入控件逐项修改；提交成功后刷新列表，展示“已重启并生效”或“已保存，下次启动生效”。
- core start/restart 冲突结果必须直接显示可理解提示，并指引进入“配置管理 > 监听端口”。
- TUI `Update`/`View` 不直接读文件、探测端口、控制进程或发原始 HTTP 请求。

## Acceptance Criteria

- [ ] 新安装生成 `mixed-port: 7890`，安装测试断言默认值，已有配置保持不变。
- [ ] 端口被其他进程占用时 `mm core start` 不创建 mihomo 进程，返回全部冲突、`PORT_CONFLICT` 和退出码 `4`。
- [ ] 无冲突时现有 start/restart/ready/rollback 流程行为不变；端口探测不依赖外部命令或进程信息权限。
- [ ] 六个字段均可查询和修改；可选字段 `0` 禁用，非法范围、未知字段和配置内重复端口在写入前被拒绝。
- [ ] stopped 修改只保存并报告下次启动生效，core 保持 stopped。
- [ ] running 修改成功后受控重启并监听新端口；新启动失败会恢复旧配置和旧 core，恢复失败进入明确 failed/`RESTORE_FAILED`。
- [ ] `mm config ports` 和 `mm config port set` 的 table/json、结构化错误及退出码测试通过。
- [ ] TUI 可进入常驻监听端口页、逐项编辑并展示刷新后的状态、冲突标记和生效提示，业务调用保持异步。
- [ ] `go test ./...`、race、vet、Linux amd64/arm64 无 CGO 构建、安装测试和 Shell 语法检查通过。
- [ ] 当前真实配置通过受管接口从 `mixed-port: 10808` 调整为 `7890`，配置校验通过；v2rayN/xray 继续占用 `10808`，不得停止或修改该进程。

## Out of Scope

- 自动选择、扫描或写入替代端口。
- 自动修改 GNOME 系统代理、shell 代理环境变量或 v2rayN/xray 配置。
- 停止、杀死或识别占用端口的进程。
- 提供 bind host、`allow-lan`、认证或其他 mihomo 配置的结构化编辑 UI。
- 将本次专用端口事务扩展为所有 YAML 字段的通用表单编辑器。
