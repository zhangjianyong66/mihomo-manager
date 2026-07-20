# 实施计划：CLI 与应用服务契约

## 1. 前置检查

- [x] 加载 `trellis-before-dev`，读取 backend 规范和跨层/复用指南。
- [x] 记录实施前 `go test ./...` 基线，确认已有失败与本任务无关。
- [x] 核对 `cmd/mm/main.go`、`internal/app/run.go` 和 TUI 对 `mihomo.Client` 的调用清单，防止 ports 遗漏 Alpha 已有能力。

## 2. 实施顺序

### 2.1 领域基础类型

- [x] 新建 `internal/domain`，实现稳定 ID、`CoreType`、`CoreState` 及严格校验。
- [x] 添加表驱动测试，覆盖空 ID、支持/不支持枚举和字符串表示。

质量门：`go test ./internal/domain`。

### 2.2 应用错误与 ports

- [x] 在 `internal/app` 定义错误类别、错误链和详情契约。
- [x] 按 core、profile、node/group、subscription、route、config、log 拆分小接口及最小 DTO。
- [x] 使用 fake 和编译期断言验证 ports，不实现 store、daemon 或 mihomo adapter。

质量门：`go test ./internal/app`，并人工检查公开签名不含 CLI/TUI/mihomo DTO。

### 2.3 CLI 输出与安全值

- [x] 在 `internal/cli` 实现 output format 严格解析、成功 envelope 和 table/json renderer。
- [x] 实现结构化错误 renderer 与 1-8 退出码映射。
- [x] 实现显式安全值/URL 脱敏和 `--show-secrets` 控制，保证默认 stdout/stderr/JSON 不出现原值。
- [x] 添加格式、writer、错误、wrapped error、未知错误及脱敏测试。

质量门：`go test ./internal/cli`。

### 2.4 根命令与兼容入口

- [x] 实现可注入依赖和 writer 的 Cobra 根命令工厂与 `Execute`。
- [x] 让根命令无参数和 `mm tui` 调用同一 TUI runner。
- [x] 将 `cmd/mm` 收敛为依赖装配和进程退出边界。
- [x] 验证 help、无效参数和未知命令不会启动 TUI，也不输出 usage 噪声。
- [x] 检查帮助页，不注册未来空壳命令。

质量门：`go test ./internal/cli ./cmd/mm`，构建 `/tmp/mm-a1` 后执行 help 冒烟。

### 2.5 全量验证与文档同步

- [x] 运行格式化、全量测试、vet 和无 CGO 构建。
- [x] 根据实际新增的目录边界和命令约定更新 README、`.trellis/spec/` 与根 `AGENTS.md`；不记录尚未实现的未来能力为当前事实。
- [x] 对照父任务能力矩阵，仅把 A1 已交付的契约项更新为完成。
- [x] 检查 diff 中没有真实订阅 URL、节点、令牌或用户配置内容。

## 3. 验证命令

```bash
test -z "$(gofmt -l cmd internal)"
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o /tmp/mm-a1 ./cmd/mm
/tmp/mm-a1 --help
/tmp/mm-a1 tui --help
git diff --check
```

## 4. 风险与停止条件

- 如果应用 ports 必须引用 `internal/mihomo` 类型才能表达现有能力，停止接口定稿，先把 DTO 收敛为领域/应用类型；不得让 core 实现泄漏进稳定契约。
- 如果无参数执行与 Cobra help/参数错误无法可靠区分，先用命令工厂测试锁定 Cobra 行为；不得以检测终端或 `os.Args` 特判绕过。
- 如果通用脱敏需要猜测任意字符串含义，停止扩大规则，改为显式安全值和命令 DTO；不得声称启发式替换能保证保密。
- 如果 A1 需要修改 `internal/mihomo` 业务逻辑或 TUI 页面才能通过，视为范围越界，拆到 A4/A7。

## 5. 回滚点

- 根命令接入是唯一运行路径变更，保持为独立提交阶段；失败时可恢复旧 `cmd/mm` 装配而不删除新基础包。
- 新 domain/app/cli 类型不写持久状态，无 schema、配置或用户数据回滚。
- 不修改安装脚本、真实配置、core 进程或系统服务。

## 6. 启动前审阅清单

- [x] PRD 中不存在未决产品问题，验收标准可自动验证。
- [x] 设计没有提前实现 daemon/SQLite/mihomo adapter/TUI 迁移。
- [x] ports 覆盖 Alpha 已有能力，但每个接口保持按用例拆分。
- [x] 用户审阅并明确批准本子任务工件。
- [x] 批准后运行 `task.py start 07-20-cli-app-contract`，再进入编码。
