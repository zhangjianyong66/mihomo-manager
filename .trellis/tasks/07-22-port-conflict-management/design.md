# 端口冲突检测与修改设计

## Architecture

```text
CLI / TUI
  -> app.CapabilityAPI
  -> versioned Unix IPC /v1/config/ports
  -> daemon CapabilityService
       -> legacy expected-digest config transaction
       -> CoreManager coordinated reconfiguration
            -> GenerationStore validate/check source
            -> Supervisor
                 -> mihomo Adapter.Start preflight
                 -> owned mihomo process
```

端口占用判断属于 mihomo 配置/进程适配边界，不由 CLI/TUI 或通用 daemon 解析 YAML。结构化配置修改由 daemon 编排，客户端只提交白名单字段和整数端口。

## Port model and parsing

在 `internal/mihomo` 定义稳定的端口字段集合和结构：

- `mixed-port`：TCP + UDP，可选。
- `port`：TCP，可选。
- `socks-port`：TCP + UDP，可选。
- `redir-port`：TCP，可选。
- `tproxy-port`：TCP + UDP，可选。
- `external-controller`：TCP，必填，保留 loopback host。

复用 `staticConfig` 解析配置，增加 `bind-address` 与 `allow-lan`。未显式绑定地址时，`allow-lan: false` 按 loopback 探测，允许 LAN 时按 wildcard 探测。纯函数负责读取端口设置、替换单个字段并再次执行静态校验；未知字段不进入 YAML。

## Preflight and error contract

`mihomo.Adapter.Start` 在 `startProcess` 前读取 RuntimeSpec 配置并尝试临时绑定每个所需 socket，成功后立即关闭。所有失败收集为 `core.PortConflictError`：

```text
PORT_CONFLICT
conflicts: [{field, network, host, port}]
```

该错误 unwrap 到 `core.ErrPortConflict`。Supervisor 在 failed status 中保存最近冲突，daemon 将其映射为 HTTP 409；app 映射为 conflict，CLI 因而返回退出码 4。错误文本列出全部字段/地址并提示修改配置或停止占用程序。预检不承诺消除检查后的 bind 竞争，真实启动错误仍走现有 start/rollback 分类。

## IPC and application contract

新增：

- `GET /v1/config/ports?profileId=...`
- `PUT /v1/config/ports`，必须有 `MM-Request-ID`，body 为 `{profileId, field, port}`。

响应 DTO 包含 profile、core state、`restarted`、`nextStart`、六个端口项和最近冲突。错误 details 保留 conflicts，客户端不得通过字符串重新解析。

CLI：

- `mm config ports [--profile] [--output table|json]`
- `mm config port set <field> <port> [--profile] [--output table|json]`

TUI 使用同一 app DTO/API；端口页异步加载，Enter 进入单字段数字输入，成功消息返回端口页并重新加载。

## Config transaction

配置修改继续复用 legacy 两阶段契约：

1. 在协调锁内读取旧内容和 expected SHA-256。
2. 通过 mihomo 纯函数构造候选内容并做静态校验。
3. `ReplaceConfig(oldDigest, candidate)` 原子写入、权限收紧、原生验证并刷新 migration expected 摘要。
4. core stopped：返回 `nextStart=true`，不创建进程。
5. core running：CoreManager 捕获旧 RuntimeSpec 后准备并启动新 RuntimeSpec。
6. 新配置 prepare/start/ready/commit 任一步失败：先 `ReplaceConfig(candidateDigest, oldContent)` 恢复文件和 expected 摘要，再核对并恢复旧 RuntimeSpec。
7. 两部分都恢复成功时保留原始错误；任一恢复失败返回 `RESTORE_FAILED` 并标记 core failed。

为避免 Coordinator 重入，受控配置重启必须作为 CoreManager 的单个公开事务实现，而不是 CapabilityService 依次调用 `Stop`、`ReplaceConfig`、`Activate`。同一操作锁覆盖摘要读取、配置写入、进程切换和恢复。

## Compatibility and safety

- 现有任意 `$EDITOR` config edit 保持原契约，不自动重启；本任务只为结构化端口入口提供受控重启。
- 订阅更新继续保留旧本地端口，缺失时使用 `7890/7891/9090`。
- 端口预检只在 daemon 托管的 adapter start 路径执行，旧 Shell 进程控制不接入。
- 不读取 `/proc`，不执行 `ss`/`lsof`，不输出 PID/进程名。
- 测试只在 `t.TempDir()` 和临时 listener 上运行，不占用固定开发机端口，不修改真实配置；真实切换放在仓库验证通过后执行。

## Rollback cases

| Failure | Required result |
|---|---|
| 候选字段/范围/重复非法 | 不写文件，旧 core 不动 |
| expected SHA-256 冲突 | 返回 CONFIG_CHANGED/CONFLICT，不写文件 |
| 原生验证失败 | legacy 恢复文件和 expected，旧 core 不动 |
| 新端口 preflight 冲突 | 恢复旧配置，再恢复旧 core |
| 新 core readiness/commit 失败 | 同上 |
| 旧配置恢复失败 | core failed，RESTORE_FAILED |
| 旧 RuntimeSpec 恢复失败 | core failed，RESTORE_FAILED |

## Trade-offs

- 使用临时 bind 探测可在进程创建前给出清晰冲突，但存在固有 TOCTOU；不通过保留 socket 解决，因为 mihomo 无法接收这些已打开 fd。
- 专用端口 API 比通用 YAML patch 多一组 DTO，但限制了输入面，能提供字段校验、结构化冲突和可审计恢复。
- 运行中自动重启比仅保存复杂，但避免配置和监听运行态长期不一致，符合用户已确认的生效语义。
