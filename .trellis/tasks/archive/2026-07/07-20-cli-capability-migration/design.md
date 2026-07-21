# A6 技术设计：现有能力 CLI 等价迁移

## 1. 边界与目标

A6 是 legacy 单 profile 的纵向迁移，不引入新的 managed 领域模型。CLI 只负责解析参数、选择输出投影和映射错误；daemon 负责组合应用服务并成为配置、SQLite、mihomo API 和进程的唯一写者；legacy 兼容层复用已有 `internal/mihomo.Client` 行为并通过 A5 的摘要/备份/恢复规则保护写入。

目标数据流：

```text
mm CLI -> app service client -> Unix socket /v1/* -> daemon handler
       -> legacy/core/route/config service -> mihomo API 或 CONFIG_DIR
       <- JSON envelope 或 NDJSON stream
```

不允许 CLI 直接打开 `state.db`、读写 `CONFIG_DIR`、启动 mihomo 或调用 external-controller。

## 2. 服务与装配

### 2.1 应用 ports

以 `internal/app/ports.go` 中已有接口为契约，补充实际 IPC client 和 daemon implementation：

- `CoreService`：状态与生命周期；`CoreStatus` 的 profile、PID、generation 和错误码来自 daemon `CoreManager`。
- `ConfigService`：validate、backup、restore、edit；legacy 写操作进入 `legacy.Compatibility`，managed/external 明确拒绝。
- `GroupService`/`NodeService`：调用 mihomo runtime API，保留组/节点名称和当前选择；`NodeService.Test` 返回可取消事件流。
- `SubscriptionService`：legacy 单来源 URL 读写/更新，更新成功后由 daemon 执行 reload/校验策略。
- `RouteService`：白名单列表/变更、CN 预设和诊断；白名单写入沿用 `internal/mihomo` 的规范化与回滚。
- `LogService`：有限 tail 和 follow；follow 以 NDJSON 事件返回，断流/取消可识别。

每个服务提供 daemon 端实现和 IPC client 两侧适配器。CLI 依赖接口而非具体 mihomo 类型；测试使用 fake service。

### 2.2 默认 profile 解析

A6 的 `profileID` 解析器按以下顺序工作：显式 `--profile`（存在时校验 ID）→ daemon 返回的唯一活动 profile → legacy 固定 ID `legacy-mihomo`。没有活动 legacy profile 时返回 `NOT_FOUND`；不猜测 external 或 managed profile。解析器属于 app 层，CLI 不读取数据库。

## 3. IPC API

所有路径以 `/v1/` 开头，修改请求携带 request ID 并通过现有 request cache 做短期幂等。错误使用已有 `ipc.WriteError`，成功响应使用 `mm/v1` envelope。

| 能力 | 方法/路径 | 载荷/响应 | 流式 |
|---|---|---|---|
| core status | GET `/v1/core/status` | `CoreStatus` | 否 |
| core action | POST `/v1/core/{start,stop,restart,reload}` | profile、operation result | 否 |
| config validate | POST `/v1/config/validate` | profile、validation result | 否 |
| group/node | GET `/v1/groups`, `/v1/groups/{id}`, `/v1/nodes` | 列表/详情 | 否 |
| group/node select | POST `/v1/groups/{id}/select` | node ID | 否 |
| node test | POST `/v1/nodes/test` | concurrency、limit、group | 是 NDJSON |
| subscription | GET/PUT/POST `/v1/subscription` | URL metadata/update result | 否 |
| whitelist | GET/POST/PUT/DELETE `/v1/routes/whitelist` | domain/result | 否 |
| route | POST `/v1/routes/preset`, GET `/v1/routes/diagnose` | preset/diagnosis | 否 |
| config file | POST `/v1/config/{backup,restore,edit}` | operation result | 否 |
| logs | GET `/v1/logs`, GET `/v1/logs/follow` | lines/events | follow NDJSON |

具体 JSON 字段使用 app DTO 的稳定字段名（`profileId`、`groupId`、`nodeId`、`subscriptionId` 等），不把 `map[string]any` 或 mihomo 原始响应暴露到领域层。不存在的可选 profile 参数不影响 legacy 默认行为。

流式端点每行一个 JSON 对象，包含 `apiVersion`、`kind`、`data` 或 `error`；客户端检测 EOF、上下文取消和 daemon 断流，避免把半行当作成功结果。

## 4. legacy 兼容写入

- `subscription set/update`、白名单变更、CN 预设、backup/restore/edit 均先加载 A5 migration 元数据和 expected digest。
- 写入前创建临时备份，执行操作，运行 mihomo 配置校验；校验失败恢复备份并返回 `VALIDATION_FAILED`。
- 成功后更新 legacy file expected digest；所有路径必须是 `CONFIG_DIR` 内的常规文件，拒绝符号链接、路径越界和不安全权限。
- `config edit` 使用两阶段事务：daemon 通过 `/v1/config/edit` GET 返回内容和 SHA-256，CLI 在本地 `0600` 临时文件中调用 `EDITOR`，再通过 PUT 携带 expected SHA-256 回传；daemon 核对摘要后执行写入、校验和失败恢复。不自动编辑 external/managed 配置，也不让 systemd daemon 直接持有终端。
- `config restore` 只使用显式 legacy backup 文件，不猜测最近恢复点；恢复前后均校验内容摘要和权限。

## 5. CLI 命令与输出

命令按 PRD 注册，公共 flag 只挂在实际使用的命令上：

- 查询：`--output table|json`，默认 table。
- 流式：`--output text|ndjson`，默认 text；`--lines`、`--concurrency`、`--limit`、`--timeout` 做输入校验。
- 写入：返回 operation/result envelope，不向 stdout 写进度；人类消息走 table presenter。
- 订阅 URL、节点 URI、日志消息和错误 details 均经过 `cli.Secret`；`--show-secrets` 只改变投影，不改变 service 返回值。
- `core logs` 和 `logs follow` 支持 `--filter` 时在客户端对已脱敏行过滤，避免把正则传入文件层。

## 6. 失败、取消与并发

- daemon coordinator 的 operation lock 保护 core 生命周期、配置写入和订阅更新；冲突返回退出码 4。
- core start/restart 继续复用 A4 supervisor 的 readiness、进程退出检测和失败状态；停止旧实例失败不得启动新实例。
- 节点测速通过带缓冲 channel 和 context，消费者取消后 producer 停止 HTTP 请求并关闭 channel；超时按单项事件报告，不伪造成功延迟。
- 日志 follow 在客户端断开时取消 daemon context；daemon 不保留长期订阅。
- daemon 不可用、协议不兼容或 peer UID 校验失败均使用退出码 5/7，不降级为旧的直接控制路径。

## 7. 测试设计

- `internal/daemon`：每条 `/v1` 路由的 HTTP 方法、参数、错误码、request ID、同一活动 profile 和流式断开测试。
- `internal/app`：fake legacy/core/runtime service 的默认 profile 解析、错误分类和取消传播测试。
- `internal/cli`：命令树、flag、table/json/text/ndjson、stdout/stderr、退出码和 `--show-secrets` 测试。
- `internal/legacy`/`internal/mihomo`：订阅/白名单/路由/backup/restore 失败恢复、摘要、权限、无符号链接和临时目录测试。
- 端到端：隔离 HOME/XDG、fake mihomo、真实 Unix socket daemon；不读取本机用户配置，不使用 TCP。

## 8. 兼容与回滚

A6 新增路由和命令注册是可独立回退的边界。任一能力未通过端到端门时，可以移除对应 CLI/IPC 注册而保留 A5 迁移和旧 TUI；不得删除 `mihomo.Client` 旧方法或改变 legacy 文件格式。A6 完成后 A7 才能把 TUI 页面逐项切换到 app service。
