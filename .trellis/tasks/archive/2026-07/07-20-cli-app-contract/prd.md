# CLI 与应用服务契约

## Goal

为 2.0 Alpha 建立可扩展、可脚本化的 CLI 基线和最小应用服务边界，使后续 daemon、存储、mihomo 适配器及 TUI 迁移能够共享同一套类型和错误契约，同时保持当前无参数 `mm` 打开 TUI 的行为。

本任务只建立契约和兼容入口，不迁移现有 mihomo 业务能力。未实现的业务命令不得出现在帮助页中。

## Background

- 当前 `cmd/mm/main.go:11` 直接组装 Cobra 根命令，并在根命令 `RunE` 中调用 `app.RunInteractive()`；错误统一以退出码 1 输出。
- 当前 `internal/app/run.go:13` 直接装配配置、`mihomo.Client`、Bubble Tea 模型，尚无可替换的应用服务接口。
- 当前 TUI 在 `internal/tui/model.go` 中直接调用 `mihomo.Client` 的 core 生命周期、代理组、订阅、路由、配置和日志方法。
- 父任务 `.trellis/tasks/07-20-align-v2rayn-cli` 已确认：非交互 CLI 是稳定契约，TUI 复用应用服务；无参数 `mm` 保持打开 TUI；查询输出为 table/json；错误、退出码和秘密脱敏从 2.0 起受兼容约束。
- 项目已依赖 Cobra `v1.9.1`，本任务不引入新的 CLI 框架。

## Requirements

### R1：薄入口与可测试命令工厂

- `cmd/mm` 只负责进程级依赖装配、执行命令并返回退出码。
- `internal/cli` 提供可注入 stdin/stdout/stderr 和应用依赖的根命令工厂，测试不得启动真实 TUI、mihomo 或访问用户配置。
- Cobra 自身不得直接调用 `os.Exit`；退出码只在 `main` 边界生效。

### R2：兼容入口

- 无参数 `mm` 继续打开现有 TUI。
- 增加显式 `mm tui`，与无参数入口调用同一应用能力。
- `mm --help`、`mm tui --help` 和参数错误不得启动 TUI。
- 帮助页只列出可实际执行的命令；不得预注册 daemon、profile、subscription 等空壳命令。

### R3：最小领域契约

- `internal/domain` 定义后续应用边界必需的稳定标识符、core 类型和通用运行状态，不包含 mihomo YAML、HTTP DTO 或 Cobra 类型。
- 标识符不得以展示名称作为身份；空标识符必须可被输入校验拒绝。
- 只加入 A1/A2-A7 已明确需要的类型，不为 3.0 未验证的多内核差异预建复杂抽象。

### R4：应用服务 ports

- `internal/app` 按能力拆分小接口，覆盖 Alpha 已规划的 core 生命周期、档案、节点/代理组、订阅、路由、配置和日志边界。
- 接口方法使用 `context.Context`，使用领域类型或应用 DTO，不泄漏 Cobra、Bubble Tea、mihomo API/YAML DTO。
- 日志等流式能力通过可取消的只读事件流表达；不得要求调用方管理实现内部进程或 channel 生命周期。
- 本任务不要求现有 `mihomo.Client` 或 TUI 立即实现/改用全部 ports；迁移分别由 A4、A6、A7 完成。

### R5：输出契约

- 建立 `table`、`json` 输出格式的统一解析和渲染入口；不支持的格式属于输入错误。
- JSON 成功响应固定包含 `apiVersion`、`kind`、`data`、`warnings`，其中 `apiVersion` 为 `mm/v1`，且 stdout 只包含一份有效 JSON 文档。
- 人类输出只写 stdout；错误只写 stderr；帮助文本遵循 Cobra 的 stdout/stderr 语义。
- A1 使用测试夹具验证输出基础设施，不为尚未实现的查询命令添加产品入口，也不在根命令暴露无效果的 `--output`。

### R6：错误和退出码契约

- 应用错误包含稳定机器码、面向用户的信息、可重试标志和可选详情；底层错误可用于诊断和 `errors.Is/As`，但不得直接泄漏到机器字段。
- JSON 错误固定使用 `apiVersion: mm/v1`、`kind: Error` 和结构化 `error` 对象；不得混入进度或 Cobra usage。
- 退出码映射固定为：`0` 成功、`1` 未分类内部错误、`2` 参数/输入错误、`3` 资源不存在、`4` 状态冲突、`5` daemon/协议不可用、`6` 配置/迁移/校验失败、`7` 权限/安全策略拒绝、`8` core/订阅/更新等上游失败。
- Cobra 参数错误映射为退出码 2；应用错误按稳定类别映射；未知错误映射为 1。

### R7：默认脱敏

- 输出层提供统一的秘密值类型或等价显式机制，使 table/json/error 在默认模式下不会输出订阅令牌、UUID、密码等原值。
- `--show-secrets` 是唯一允许输出完整秘密的 CLI 开关；不能通过环境变量隐式开启。
- A1 提供供真实命令注册该开关的复用机制，但根命令在没有秘密输出时不暴露无效果的 `--show-secrets`。
- 测试必须使用合成秘密，且同时验证 stdout、stderr 和 JSON 序列化结果。

### R8：兼容与范围约束

- 不改变 `internal/mihomo.Client` 的现有运行、配置写回或进程控制语义。
- 不引入 daemon、IPC、SQLite、systemd、managed 配置生成或新的 mihomo 用户能力。
- 不复制或改写 v2rayN GPL-3.0 源码；本任务是基于已确认产品契约的独立实现。
- 继续满足 Go 1.22 和 `CGO_ENABLED=0` 构建要求。

## Acceptance Criteria

- AC1（R1/R2）：命令工厂测试证明无参数和 `tui` 都只调用一次注入的 TUI runner；`--help`、子命令帮助及无效参数不调用 runner。
- AC2（R2）：`mm --help` 只展示实际可执行入口，不包含 daemon、profile、subscription 等未来空壳命令；`mm tui --help` 可用。
- AC3（R3/R4）：编译期断言和单元测试证明各应用 port 可由 fake 实现；公开方法不依赖 Cobra、Bubble Tea 或 `internal/mihomo` 类型。
- AC4（R5）：table/json 成功输出测试验证 stdout/stderr 分离，JSON envelope 字段和值稳定，不支持的 `--output` 返回输入错误。
- AC5（R6）：表驱动测试覆盖全部退出码类别、Cobra 参数错误、未知错误、wrapped error，以及 JSON 错误对象；错误路径不向 stdout 写普通文本或 usage。
- AC6（R7）：table/json/error 的合成 URL、令牌、UUID 和密码默认不可见；显式 `--show-secrets` 才可见原值。
- AC7（R8）：现有 TUI 入口仍可构建，`internal/mihomo` 行为测试不需修改；本任务不创建 daemon/store/core adapter 的实际实现。
- AC8：以下验证全部通过：

```bash
test -z "$(gofmt -l cmd internal)"
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o /tmp/mm-a1 ./cmd/mm
/tmp/mm-a1 --help
/tmp/mm-a1 tui --help
```

## Out Of Scope

- daemon、Unix socket、systemd user service 和任何 IPC 请求。
- SQLite、schema、档案持久化和迁移。
- mihomo 适配器、配置生成、legacy 迁移和现有 `mihomo.Client` 拆分。
- core、订阅、节点、路由、配置和日志的可操作 CLI 子命令；这些由 A6 在真实应用服务可用后接入。
- TUI 改用应用服务；该工作属于 A7。
- MIT `LICENSE` 和第三方许可证清单落地；该发布工件属于 A8，A1 仅遵守已选许可证边界。

## Dependencies

- 父任务：`07-20-align-v2rayn-cli`。
- 前置实施依赖：无。
- 后续依赖：A2 复用领域标识和应用边界；A3 复用错误/输出协议语义；A4-A7 分别实现或消费本任务定义的 ports。
