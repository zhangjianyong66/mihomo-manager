# 实施计划：领域模型、SQLite 与迁移框架

## 1. 前置与停止门

- [x] 加载 `trellis-before-dev`，读取 backend 索引、目录、代码风格、项目约定、CLI 边界及跨层/复用指南。
- [x] 记录实施前 `go test ./...`、race、vet 和 Go 1.22.12 双架构无 CGO 构建基线。
- [x] 固定 `modernc.org/sqlite v1.36.1`，运行 `go mod tidy`，确认 `modernc.org/libc v1.61.13` 未被其他依赖抬升为不匹配版本。
- [x] 在实际项目中完成 amd64/arm64 无 CGO 构建、最终依赖许可证和成品体积探针。

停止门：任一目标架构构建失败、许可证不兼容/无法确认、libc 版本不匹配或 amd64 未压缩成品超过 25 MiB 时，停止 schema 实现并回到驱动选型。

## 2. 实施顺序

### 2.1 领域类型

- [x] 扩展 `internal/domain`：`OperationID`、`ProfileMode`、`OperationState`、Profile/Subscription/Node/Operation/Setting 聚合。
- [x] 集中实现枚举、必填字段、模式/路径、JSON、UTC 时间和归属校验；不复制 A1 的 ID 校验。
- [x] 添加表驱动测试，覆盖空值、未知枚举、无效 JSON、非 UTC 时间和合法 round-trip 数据。

质量门：`go test ./internal/domain`，人工确认不导入 store/mihomo/CLI/TUI。

### 2.2 Store 打开、安全与迁移器

- [x] 新建 `internal/store` 与 `migrations/0001_*.sql`、`0002_*.sql`。
- [x] 实现显式路径验证、目录/文件权限收紧、符号链接拒绝、单连接池和 PRAGMA 设置/读回。
- [x] 实现 quick check、迁移发现/连续性/checksum 校验、schema-too-new 与 migration-drift 错误。
- [x] 实现全部待执行迁移的单事务执行，以及 online backup 临时文件、校验、fsync 和原子命名。
- [x] 实现幂等 Close 和关闭状态检查。

质量门：临时数据库测试覆盖新建、重复打开、v1 升 v2、失败批次回滚、未知新版本、checksum 漂移、损坏文件和权限。

### 2.3 Profile 与 subscription 仓储

- [x] 实现 profile create/get/list/update/set-active/delete，保持 revision 与时间语义。
- [x] 实现 subscription create/get/list/update/delete，确保秘密不进入错误文本。
- [x] 用真实约束测试单 active profile、模式/路径、外键和档案级联边界。

质量门：`go test ./internal/store -run 'Profile|Subscription'`。

### 2.4 Node 来源事务

- [x] 实现按 subscription 排序读取节点。
- [x] 实现 `ReplaceSubscriptionNodes`：事务内再次确认来源、删除目标集合、插入新集合、更新成功元数据。
- [x] 覆盖空集合、重复 remote key、node ID 跨来源冲突、无效节点和 context 取消。
- [x] 明确断言失败回滚目标旧集合，且其他来源数据完全不变。

质量门：`go test ./internal/store -run 'Node|Replace'`。

### 2.5 Operation 与 settings 仓储

- [x] 实现 operation create/get/update/list-unfinished，不在 store 中复制应用状态机。
- [x] 实现 global/profile setting set/get/delete/list，验证 partial unique index 和 profile 删除级联。
- [x] 覆盖 JSON round trip、可选 profile、operation profile 删除后保留并置空。

质量门：`go test ./internal/store -run 'Operation|Setting'`。

### 2.6 并发、恢复与最终验证

- [x] 增加受控并发写、context timeout、未提交事务后重开和 foreign key check 测试。
- [x] 运行格式化、全量测试、race、vet、diff 检查和 Go 1.22.12 双架构构建。
- [x] 记录最终二进制字节数与基线差值，确认 amd64 小于等于 25 MiB。
- [x] 根据实际结构更新 backend spec、README/AGENTS 中已实现事实和父任务 A2 清单；不把默认路径接入、daemon 或真实迁移写成已完成。
- [x] 检查 diff/测试夹具不含真实数据库、订阅 URL、节点凭据或用户路径内容。

## 3. 验证命令

```bash
test -z "$(gofmt -l cmd internal)"
GOTOOLCHAIN=go1.22.12 go test ./...
GOTOOLCHAIN=go1.22.12 go test -race ./...
GOTOOLCHAIN=go1.22.12 go vet ./...
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -o /tmp/mm-a2-amd64 ./cmd/mm
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -o /tmp/mm-a2-arm64 ./cmd/mm
test "$(stat -c %s /tmp/mm-a2-amd64)" -le 26214400
git diff --check
```

必要的包级快速迭代命令：

```bash
go test ./internal/domain
go test ./internal/store
go test ./internal/store -run 'Migration|Backup|Permission|Corrupt'
go test ./internal/store -run 'Replace|Concurrent|Rollback'
```

## 4. 风险与回滚点

- 驱动依赖与体积是第一个停止门；业务代码开始前先验证，避免完成 schema 后才发现发布不兼容。
- 迁移器和仓储分阶段实现。迁移/权限测试未通过时不开始业务仓储，防止错误 schema 被更多代码依赖。
- migrations 一旦提交不得原地修改；修复只能新增版本。checksum 测试负责阻止误改。
- online backup API 是唯一驱动专属边界，封装在 `internal/store` 小接口中；若 API 有问题，可在不改领域/仓储 API 的情况下替换备份实现。
- A2 不接入默认用户路径，自动化不会产生真实状态；代码回滚可移除 store 装配和依赖，测试数据库都位于临时目录。
- 已升级测试数据库用 `.pre-vN-*.bak` 恢复；不实现 schema downgrade SQL。

## 5. 完成与交付门

- [x] `trellis-check` 全量检查通过，所有高优先级发现已修复或由用户明确接受。
- [x] `trellis-update-spec` 记录 `internal/store` 边界、迁移不可改写、权限和验证命令。
- [x] 父任务能力矩阵/实施清单只更新 A2 已交付项。
- [x] 使用中文 Conventional Commit，例如 `feat(store): 建立 SQLite 领域存储与迁移框架`。
- [ ] 执行 `trellis-finish-work`，归档 A2 并记录 journal。

## 6. 启动前审阅清单

- [x] PRD 已完成收敛，无重复事实或未决范围问题。
- [x] 设计明确了领域/SQL 映射、事务、迁移、权限、恢复点和 driver 隔离。
- [x] 实施顺序先经过驱动停止门，再写业务 schema。
- [x] 用户审阅并明确批准本任务工件。
- [x] 批准后运行 `task.py start 07-20-domain-sqlite-migrations`，再进入编码。
