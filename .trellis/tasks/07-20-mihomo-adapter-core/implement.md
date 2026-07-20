# 实施计划：mihomo adapter 与 core 生命周期

## 1. 依赖与执行边界

- 依赖已归档 A1（应用/领域契约）、A2（SQLite/operation 仓储）和 A3（daemon/coordinator/XDG）。
- 使用 inline 模式由主会话实现；编码前加载 `trellis-before-dev` 和 backend 规范。
- 不操作真实 `~/.config/mihomo`、真实 manager database 或已安装 mihomo；测试全部显式注入临时路径和假进程。
- 不新增用户可见业务命令。A4 以包契约、daemon 内部装配和自动化状态机测试验收。

## 2. 实施清单

### 2.1 core 契约与路径

- [x] 新建 `internal/core`，定义最小 adapter、snapshot、rendered config、runtime spec/client、process 和 typed error。
- [x] 扩展 `config.ManagerPaths`：generation 根目录、core state/runtime/log 路径；补默认 XDG 和显式路径测试。
- [x] 更新目录结构规范，明确 `internal/core` 与 `internal/mihomo` 的依赖方向。

验证：`go test ./internal/core ./internal/config`。

### 2.2 generation 与配置

- [x] 实现 generation ID/metadata、目录权限、同文件系统临时写、fsync、rename 和 current 指针原子替换。
- [x] 实现 external/legacy 只读引用和两阶段 SHA-256 检查。
- [x] 实现 mihomo managed 最小确定性 renderer 和静态 validator。
- [x] 增加 golden fixture、重复渲染、权限、失败清理、源文件不变和摘要冲突测试。

验证：`go test ./internal/core ./internal/mihomo`，并检查 golden 不含真实秘密。

### 2.3 mihomo 原生验证、进程与 API

- [x] 实现受控 `-t -d -f` validator，覆盖成功、非零退出、超时和错误截断。
- [x] 实现 process factory、Setsid 平台适配、日志 `0600`、精确 PID SIGTERM/SIGKILL 和 Wait。
- [x] 实现 loopback-only RuntimeClient、response 上限、状态码错误和 ready 轮询。
- [x] 使用 fake core helper/httptest 覆盖参数、信号、提前退出、慢退出、API 错误与超时。

验证：`go test -race ./internal/mihomo`；Linux/Darwin 平台文件编译检查。

### 2.4 daemon supervisor 与切换恢复

- [x] 实现单实例 supervisor 状态快照和只持有精确 Process 句柄的 start/stop/close。
- [x] 实现 CoreManager prepare/validate/stop/start/ready/commit/rollback 状态机。
- [x] 复用 coordinator 串行化操作；通过 repository 接口写 operation、活动 profile 和 runtime metadata。
- [x] daemon 装配 adapter/CoreManager，但不增加 IPC 路由、不在 daemon 启动时自动启动 core。
- [x] daemon shutdown 关闭受管 core，再关闭 listener/store；聚合清理错误。
- [x] fake adapter/store 测试成功切换、验证失败、启动失败、就绪失败、恢复成功/失败、并发冲突和退出清理。

验证：`go test -race ./internal/daemon`。

### 2.5 收敛与文档

- [x] 删除无真实使用价值的预留接口；确认 mihomo DTO/map 未越过 adapter 边界。
- [x] 更新 `README.md`、`AGENTS.md`、backend spec 和父任务 A4 状态。
- [x] 运行全量质量门，复查测试未访问真实 HOME/core/socket/database。

## 3. 全量验证

```bash
test -z "$(gofmt -l cmd internal)"
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/mm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/mm
git diff --check
```

可选真实 core 契约验证必须使用临时目录和显式开关，只执行配置 `-t`，不得启动代理监听或读取真实配置。

## 4. 风险点与停止条件

- 风险文件：`internal/daemon/server.go` 的 shutdown 顺序、`internal/config/config.go` 的 XDG 路径、`internal/mihomo/client.go` 的兼容边界。
- 若 managed 最小配置不能确定性渲染并通过受控 core 验证，停止发布 generation，不使用未验证 YAML 兜底。
- 若无法证明停止操作只影响所持进程，停止接入 daemon，不复用 `pkill`。
- 若新实例失败后旧实例恢复测试不稳定，A4 不归档，也不进入 A5。
- 若需要修改既有 SQLite migration，停止并改为追加 migration 或文件 metadata。

## 5. 回滚点

1. core 契约/路径可独立回滚，不改变用户行为。
2. generation/adapter 默认未暴露 CLI，可移除 daemon 装配而保留单元代码。
3. supervisor 接入失败时恢复 A3 daemon（仅 health/status），旧 TUI 继续使用现有 `mihomo.Client`。
4. 本任务不写真实用户配置或迁移数据，因此代码回滚不需要数据回滚。

## 6. 启动前审阅门

- [x] PRD 的 requirements 与 acceptance criteria 一一对应。
- [x] 设计明确 external 只读、managed generation 和失败恢复顺序。
- [x] 用户确认 A4 直接接入 daemon 生命周期，且不新增业务 CLI。
- [x] 用户审阅并批准本实施计划后，才执行 `task.py start`。
