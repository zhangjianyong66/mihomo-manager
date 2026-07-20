# CLI 与应用边界契约

## Scenario：新增或修改 `mm` 命令

### 1. Scope / Trigger

以下变化必须遵守本规范：新增 Cobra 命令、修改应用服务接口、增加 table/json 输出、增加机器错误码或输出可能含秘密的字段。

目标是让 `cmd/mm` 只负责进程装配，让 CLI/TUI 最终复用 `internal/app` 用例，并保证脚本可依赖 JSON 和退出码。当前 A1 只发布 `mm` 与 `mm tui` 两个 TUI 入口，业务子命令尚未接入。

### 2. Signatures

入口签名：

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

应用服务接口放在 `internal/app`，按 core、profile、node、group、subscription、route、config、log 拆分。所有方法首参为 `context.Context`，参数和返回值只能使用标准库、`internal/domain` 或 `internal/app` 类型。

领域 ID 和枚举放在 `internal/domain`：`ProfileID`、`SubscriptionID`、`NodeID`、`GroupID`、`CoreType`、`CoreState`。

### 3. Contracts

命令行为：

- 无参数 `mm` 和 `mm tui` 调用同一个 `TUIRunner`。
- help、补全和参数错误不得启动 TUI。
- 未实现的业务命令不得注册空壳；`--output`、`--show-secrets` 只绑定到实际使用它们的命令。
- stdin/stdout/stderr 从 `Execute` 注入，不允许命令测试读取真实终端或 HOME。

JSON 成功 envelope：

```json
{"apiVersion":"mm/v1","kind":"CoreStatus","data":{},"warnings":[]}
```

JSON 错误 envelope：

```json
{"apiVersion":"mm/v1","kind":"Error","error":{"code":"PROFILE_CONFLICT","message":"档案正在切换","retryable":false,"details":{}}}
```

应用错误的 `Category` 只决定进程退出码，不进入 JSON；`Code` 是稳定且可细化的机器码。底层 `Err` 只保留错误链，不直接输出。

秘密输出：

- DTO/presenter 对秘密字段显式使用 `cli.Secret`，不得靠关键词猜测任意字符串。
- `Secret.String()` 和 JSON marshal 永远脱敏；需要完整值时由输出投影显式调用 `Display(showSecrets)`。
- `NewURLSecret` 默认只保留 scheme/host，隐藏 userinfo、非根路径、query 和 fragment。
- 只有命令行显式 `--show-secrets` 可以使投影显示完整值，环境变量不得隐式开启。

### 4. Validation & Error Matrix

| 条件 | `ErrorCategory` | 退出码 |
|---|---|---:|
| 未分类错误、缺少内部依赖 | `internal` | 1 |
| 未知命令、额外参数、非法 flag/输出格式 | `invalid_argument` | 2 |
| 资源不存在 | `not_found` | 3 |
| 状态或并发冲突 | `conflict` | 4 |
| daemon 不可用或协议不兼容 | `daemon_unavailable` | 5 |
| 配置、迁移或校验失败 | `validation_failed` | 6 |
| 权限不足或安全策略拒绝 | `permission_denied` | 7 |
| core、订阅、更新源等上游失败 | `upstream_failure` | 8 |

`errors.As` 必须能穿透包装后的 `*app.Error`。未知底层错误只输出“内部错误”，不得直接把 `err.Error()` 写到 stdout/stderr 或 JSON。

### 5. Good / Base / Bad Cases

- Good：`mm future-command --output json` 的 stdout 只有一份有效 JSON，stderr 为空，`warnings` 即使为空也是 `[]`。
- Base：`mm` 和 `mm tui` 在同一 fake runner 上各调用一次；`mm --help` 不调用 runner。
- Bad：未知命令返回 2，stdout 为空，stderr 不附带 usage，也不启动 TUI。
- Bad：秘密出现在 URL path/query、错误详情或表格字段时，默认输出不得包含原值。

### 6. Tests Required

- 根命令：无参数、`tui`、两级 help、未知命令、额外参数、未知 flag、IO 注入和 runner 错误映射。
- 输出：table/json writer 分离、稳定 envelope、空 warnings、非法格式。
- 错误：0-8 全部映射、wrapped `app.Error`、细化机器码与分类解耦、未知错误隐藏。
- 秘密：table、JSON、错误详情、URL 默认脱敏及显式展示。
- 领域/应用：空 ID、非法枚举以及每个 port 的 fake 编译期断言。
- 提交前执行：`go test ./...`、`go vet ./...`、`CGO_ENABLED=0 go build -trimpath -o /tmp/mm ./cmd/mm`、两个 help 冒烟。

### 7. Wrong vs Correct

#### Wrong

```go
cmd := &cobra.Command{RunE: func(*cobra.Command, []string) error {
    fmt.Println(subscription.URL) // 绕过 writer 注入并泄漏秘密
    return nil
}}
```

#### Correct

```go
secret := cli.NewURLSecret(subscription.URL)
result := cli.Result{
    Kind: "Subscription",
    Data: func(show bool) any {
        return map[string]any{"url": secret.Display(show)}
    },
    Table: func(w io.Writer, show bool) error {
        _, err := fmt.Fprintln(w, secret.Display(show))
        return err
    },
}
```

命令只做输入和展示适配；业务调用通过 `internal/app` port 完成，不能直接访问 `internal/mihomo`、配置文件或未来 SQLite。
