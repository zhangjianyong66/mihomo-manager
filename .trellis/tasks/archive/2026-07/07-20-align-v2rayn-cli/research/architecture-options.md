# 架构选型研究

## 1. 约束

- 保持 Go 单二进制和 `CGO_ENABLED=0` 安装能力。
- 首期只支持 Ubuntu/Debian amd64、arm64。
- daemon、CLI、TUI 均以普通用户运行。
- 只允许本机同用户访问，不提供远程 TCP API。
- CLI/JSON 是稳定契约，TUI 是共享应用服务的前端。
- 2.x 必须兼容现有配置、环境变量和无参数 TUI。

## 2. IPC

### 选择：HTTP/JSON over Unix socket

使用 Go `net/http`，在 Unix socket 上暴露版本化本地 API，例如 `/v1/status`。流式日志、测速和连接事件使用 NDJSON 流；普通命令使用 JSON 请求/响应。

理由：

- 标准库支持，避免 protobuf/codegen 和额外运行时。
- 便于 CLI、TUI、测试夹具复用，协议可通过 HTTP 状态和版本化路径演进。
- NDJSON 适合终端管道和逐条事件，不要求客户端缓冲完整响应。
- Unix socket 目录权限、socket 权限和 Linux peer credentials 可共同限制同用户访问。

未选择：

- gRPC：类型契约强，但引入 codegen、HTTP/2 和流式调试复杂度；当前规模没有必要。
- `net/rpc`：接口冻结、版本和错误契约不适合作为长期产品协议。
- 直接复用 Cobra 进程内调用：无法支持 daemon 单写者和并发客户端。

## 3. 持久化

### 选择：`database/sql` + 无 CGO SQLite 驱动

首选候选为 `modernc.org/sqlite`；在 2.0 Alpha 子任务中固定版本并重新核对许可证、Go 版本和 amd64/arm64 构建。不得使用要求系统 C 编译器的驱动。

SQLite 保存规范化档案、来源、订阅、节点、规则、设置、schema 版本和聚合统计。大日志、生成配置、外部完整配置、覆盖层和备份包仍使用受权限保护的文件。

理由：

- 事务适合订阅整批替换、档案切换和迁移。
- 唯一约束与外键能保证稳定 ID、来源归属和删除边界。
- 单文件备份和一致性检查适合单用户 CLI。
- daemon 单写者避免多进程写锁竞争。

未选择：

- 单个 YAML/JSON：可读，但跨来源事务、引用完整性和迁移成本高。
- BoltDB/Badger：键值模型会把关联和迁移复杂度推回应用层。

## 4. 模块边界

建议目标结构：

```text
cmd/mm                 Cobra 根入口
internal/app           依赖装配、命令应用服务
internal/cli           命令、输出和退出码
internal/tui           只保留交互状态与渲染
internal/daemon        本地 API、调度、运行状态
internal/ipc           Unix socket 客户端/服务端协议
internal/domain        档案、订阅、节点、规则等模型
internal/store         SQLite 仓储与迁移
internal/core          内核适配器接口和生命周期编排
internal/core/mihomo   mihomo 配置生成、API、进程适配器
internal/configgen     生成管线、覆盖层、diff、验证
internal/platform      systemd、权限、系统代理平台接口
internal/security      脱敏、文件权限、摘要验证
```

现有 `internal/mihomo/client.go` 采用绞杀式迁移：先由新适配器包装已测试行为，再逐块移动，不一次性重写 1550 行实现。现有 `internal/tui/model.go` 先依赖应用服务接口，再逐步拆页面。

## 5. Core 适配器

适配器至少表达：

```go
type Adapter interface {
    Type() CoreType
    Discover(context.Context) (Installation, error)
    Render(context.Context, ProfileSnapshot) ([]byte, error)
    Validate(context.Context, []byte) error
    Start(context.Context, RuntimeSpec) (Process, error)
    Runtime(context.Context) RuntimeClient
}
```

领域层不暴露 mihomo 原始 `map[string]any`。core 专属字段只存在于适配器、覆盖层和明确标注的原始响应 DTO 中。

## 6. 生成和切换事务

```text
读取活动候选快照
  -> 生成临时配置
  -> 静态冲突检查
  -> core 原生验证
  -> 原子发布生成配置
  -> 停止旧实例
  -> 启动并等待新实例就绪
  -> 提交活动档案状态
  -> 失败时尝试恢复旧实例，否则进入明确 stopped/failed 状态
```

数据库事务不能跨越外部进程生命周期；采用持久化操作记录和补偿步骤，不假装提供跨 SQLite/文件/进程的原子事务。

## 7. 运行与服务

- socket 优先位于 `$XDG_RUNTIME_DIR/mihomo-manager/mm.sock`；无该变量时使用受 `0700` 保护的用户运行目录。
- daemon 使用锁文件/独占 socket 保证单实例。
- systemd user unit 设置显式重启退避；应用层再限制 core 的重启次数和熔断。
- daemon 不自动请求 sudo，也不监听 TCP。
- TUN 设置由显式一次性命令完成，记录变更并提供撤销；具体 capability 集在 2.1 子任务中通过 mihomo 官方行为验证后最小化。

## 8. 备份与同步

备份使用逻辑导出，不直接上传运行中的 SQLite 文件。包内包含版本化 manifest 和规范化 JSON，再整体加密。初始加密候选为 `age` 格式，支持 passphrase 或 recipient；具体依赖在 2.2 子任务中固定。

传输接口只处理不透明加密字节和远端版本元数据。WebDAV 不解析备份内容，冲突默认停止。

## 9. 主要风险

- 迁移同时触及 CLI、TUI、进程、存储和安装，必须用纵向切片及双轨回退控制。
- SQLite 驱动会显著增加构建时间和二进制体积，需要在 Alpha 开始时做 amd64/arm64 构建验证。
- Unix peer UID 校验是 Linux 专属实现，必须通过平台接口和构建标签隔离。
- core API DTO 与领域模型混用会重新形成大包，代码评审必须检查边界。
- TUN capability、系统代理和更新属于高权限/高影响功能，不能与基础架构迁移合并交付。
