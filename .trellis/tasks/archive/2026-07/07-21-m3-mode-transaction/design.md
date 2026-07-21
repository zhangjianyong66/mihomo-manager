# M3 技术设计：模式事务

## 1. 类型与端口

`internal/domain` 新增 `RoutingMode` 与 Validate/String/JSON 契约。`internal/app` 的 ModeService/CapabilityAPI 暴露：

```go
ModeStatus(context.Context, string) (RoutingModeStatus, error)
SetMode(context.Context, SetRoutingModeRequest) (RoutingModeStatus, error)
```

daemon DTO 只包含 domain/app 可表达字段，不透传 mihomo map。稳定错误至少包括：`INVALID_ROUTING_MODE`、`PROFILE_MODE_UNSUPPORTED`、`CONFIG_CHANGED`、`MODE_RUNTIME_MISMATCH`、`RESTORE_FAILED`。

## 2. runtime adapter

扩展 mihomo typed runtime client：

```go
Mode(context.Context) (domain.RoutingMode, error)
SetMode(context.Context, domain.RoutingMode) error
Reload(context.Context, string) error
LoadedRules(context.Context) ([]RuntimeRule, error)
ConnectionCount(context.Context) (int, error)
CloseConnections(context.Context) error
```

响应继续执行 loopback、1 MiB、完整单 JSON 值和状态码限制。SetMode 使用 mihomo `/configs` 的类型化 JSON 请求；需要加载新 provider/DNS 时才 reload 完整候选。Runtime API 不负责持久化。

## 3. daemon 事务

CapabilityService 调用 `legacy.Compatibility.SetRoutingMode`，但操作协调器必须与 core activate/reload、订阅和 config edit 共享同一 profile 写锁，避免 lock inversion。事务记录 operation kind `routing.mode.set` 和阶段：

```text
captured -> candidate_validated -> file_published
 -> runtime_updated -> runtime_verified -> succeeded
 -> restoring -> restored | restore_failed
```

候选始终包含 M2 Rule policy，即使目标是 global/direct，保证之后切回 rule 不需要重新获取网络资产。发布文件后刷新 expected 摘要与 runtime 更新必须被同一补偿流程覆盖。

core 状态来自 CoreManager，不通过 pgrep 猜测。stopped 时不访问 controller；若外部存在未托管 mihomo，返回 warning 留给 M5 入口诊断，不控制该进程。

## 4. 核验与恢复

- 配置核验：重新读取文件 mode/SHA，确认与候选一致。
- runtime 核验：`/configs.mode` 等于目标；Rule 时 `/rules` 含两个 RULE-SET 和唯一兜底。
- close connections 在 runtime 核验成功后执行；关闭失败返回 upstream failure，但模式已切换，响应必须以 operation phase 和 warning 明确“模式成功、连接关闭失败”，不能回滚模式造成更大扰动。
- runtime 更新/核验失败：恢复原文件并 reload/set 原 mode，再确认；恢复任一侧失败进入 RESTORE_FAILED。

## 5. IPC

注册 `GET /v1/mode` 与 `PUT /v1/mode`。PUT body：

```json
{"profileId":"legacy-mihomo","mode":"rule","closeConnections":false}
```

PUT 要求 `MM-Request-ID`，成功响应返回完整 status 和 warnings；幂等缓存防止客户端超时重试造成二次切换。GET 不产生 operation。

## 6. 测试矩阵

用临时 config/repository、fake native validator、httptest runtime 和真实 Unix IPC 覆盖 stopped/running、三模式、非法 profile、摘要冲突、每一失败阶段、恢复失败、并发锁、request ID 重放和 close partial-success。
