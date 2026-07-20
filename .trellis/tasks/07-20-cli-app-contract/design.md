# 技术设计：CLI 与应用服务契约

## 1. 设计目标

A1 要先稳定“调用什么、如何输出、如何失败”，而不是提前实现未来业务。最终依赖方向为：

```text
cmd/mm -> internal/cli -> internal/app -> internal/domain
   |             |               ^
   +------ 进程装配               |
                                 |
现有 internal/tui -> internal/mihomo（A1 保持不变）
```

后续 A4-A7 再把现有 TUI/mihomo 路径逐步接到应用服务。A1 不制造兼容 adapter，也不进行大文件搬迁。

## 2. 包边界

### `internal/domain`

只包含无框架依赖的值类型和枚举：

- `ProfileID`、`SubscriptionID`、`NodeID`、`GroupID` 等稳定标识。
- `CoreType`，A1 只认可 `mihomo`，保留类型本身供 3.0 扩展。
- `CoreState`，覆盖 `stopped`、`starting`、`running`、`stopping`、`degraded`、`failed`。
- 必要的 `Validate()` 或构造函数，拒绝空值和不受支持枚举。

领域包不得导入 `cobra`、`bubbletea`、`internal/mihomo` 或序列化框架专属 DTO。

### `internal/app`

采用小接口，避免单个总服务随里程碑膨胀：

```go
type CoreService interface { /* status/start/stop/restart/reload */ }
type ProfileService interface { /* list/show/use */ }
type NodeService interface { /* list/test/select */ }
type GroupService interface { /* list/show/select */ }
type SubscriptionService interface { /* list/show/update */ }
type RouteService interface { /* list/diagnose/whitelist */ }
type ConfigService interface { /* render/diff/validate/backup/restore */ }
type LogService interface { /* tail/follow */ }
```

接口只定义 Alpha 已知用例所需的调用边界，不包含 store CRUD、HTTP 路由或 core 原生方法。所有方法首参为 `context.Context`。列表和状态使用应用 DTO；DTO 只引用领域标识和值对象。

流式接口返回只读事件 channel，事件含数据或结构化错误并有终止语义；取消由 `context.Context` 控制。具体 NDJSON、mihomo WebSocket/HTTP 和断线策略由后续层处理。

应用错误放在 `internal/app`，避免 CLI 反向成为业务依赖：

```go
type Error struct {
    Category  ErrorCategory
    Code      ErrorCode
    Message   string
    Retryable bool
    Details   map[string]any
    Err       error
}
```

`Category` 只负责退出码分类，不进入 JSON；`Code` 是可细化的稳定机器码，例如 `PROFILE_CONFLICT`。`Unwrap()` 保留 Go 错误链，`Err` 只用于诊断，不直接序列化。未显式设置分类时，基础错误码保持向后兼容映射。

### `internal/cli`

负责：

- 根命令和已实现子命令注册。
- 参数校验、输出格式、成功/错误 envelope。
- 应用错误到进程退出码的唯一映射。
- table/json 输出和秘密展示策略。

不负责：业务状态、文件访问、mihomo 调用或 daemon 自动降级。

## 3. 入口与执行

命令执行拆成两层：

```go
func NewRoot(deps Dependencies) *cobra.Command
func Execute(
    ctx context.Context,
    deps Dependencies,
    args []string,
    stdin io.Reader,
    stdout, stderr io.Writer,
) int
```

`Dependencies` 至少包含可注入的 TUI runner。`Execute` 配置命令参数和 writer，执行后统一渲染错误并返回退出码。`cmd/mm/main.go` 只调用 `os.Exit(cli.Execute(...))`。

根命令使用与 `cobra.NoArgs` 等价、但返回结构化输入错误的 `noArgs`，并设置 `SilenceErrors: true`、`SilenceUsage: true`。无参数 `RunE` 和 `tui` 子命令调用同一个 runner。帮助或补全路径由 Cobra 处理，不进入 runner。

## 4. 输出协议

### 成功 JSON

```json
{
  "apiVersion": "mm/v1",
  "kind": "VersionInfo",
  "data": {},
  "warnings": []
}
```

字段使用 lower camel case。`warnings` 即使为空也输出空数组，避免 `null`。具体 `kind` 由命令定义，并在测试中固定。

### 错误 JSON

```json
{
  "apiVersion": "mm/v1",
  "kind": "Error",
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "不支持的输出格式",
    "retryable": false,
    "details": {}
  }
}
```

错误 JSON 只写 stderr，stdout 保持空。table 模式错误写一行可读消息到 stderr，不自动附带 usage；用户仍可显式请求 `--help`。

输出格式由严格枚举解析。A1 的产品命令树没有业务查询命令，因此输出 renderer 通过单元契约和未来命令可复用的 handler seam 验证，不添加假业务命令。
`--output` 和 `--show-secrets` 通过命令选项绑定 helper 注册到真正使用它们的命令，不作为 A1 根命令的无效果 persistent flags。契约测试使用夹具命令验证参数解析和渲染。

## 5. 退出码映射

| 应用错误类别 | 进程退出码 |
|---|---:|
| 未分类内部错误 | 1 |
| 参数/输入错误 | 2 |
| 资源不存在 | 3 |
| 状态/并发冲突 | 4 |
| daemon/协议不可用 | 5 |
| 配置/迁移/校验失败 | 6 |
| 权限/安全策略拒绝 | 7 |
| core/订阅/更新上游失败 | 8 |

Cobra 的未知命令、额外参数、必填参数和 flag 校验统一包装成 `INVALID_ARGUMENT`。未知 Go error 映射为内部错误。映射使用 `errors.As`，保证 wrapped `app.Error` 保持类别。

## 6. 秘密输出

不对任意字符串做猜测式全局替换。应用/命令 DTO 对秘密字段使用显式 `Secret` 值，输出上下文根据 `showSecrets` 决定显示原值或固定掩码；URL 帮助器负责隐藏 userinfo、敏感查询值和订阅路径令牌。

错误详情进入 renderer 前使用同一安全值机制。普通 `error.Error()` 不应携带秘密；测试用合成值验证默认 table/json/stderr 均不出现原文。`--show-secrets` 仅由显式 flag 设置，不读取环境变量。

## 7. 测试设计

- `internal/domain`：标识符和枚举有效/无效值表驱动测试。
- `internal/app`：错误链、错误码与 fake port 编译期断言。
- `internal/cli`：内存 buffer + fake TUI runner，覆盖根命令、`tui`、help、无效参数、writer 分离。
- `internal/cli`：成功/错误 JSON golden-like 结构断言，不依赖 map 输出顺序。
- `internal/cli`：退出码全表、wrapped/unknown error、秘密默认隐藏和显式显示。
- 全量回归：现有 `internal/mihomo` 测试保持通过。

测试不得读取真实 HOME、启动 Bubble Tea、执行 mihomo、监听端口或修改用户配置。

## 8. 兼容与回滚

- 现有 `app.RunInteractive()` 保留，作为真实 TUI runner 注入新 CLI 工厂。
- `internal/tui` 和 `internal/mihomo` 在 A1 中不改变依赖关系。
- 若新入口出现回归，可把 `cmd/mm` 恢复为旧根命令装配；新增的 domain/app/cli 基础类型没有持久状态，无数据回滚问题。
- A1 不承诺内部 Go API 跨主版本稳定，但 JSON、退出码和 CLI 行为一旦由产品命令发布，后续只能兼容扩展。
