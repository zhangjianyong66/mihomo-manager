# 技术设计：mihomo adapter 与 core 生命周期

## 1. 边界与依赖

```text
daemon CoreManager
  ├── core.Adapter (抽象契约)
  │     └── mihomo.Adapter
  │           ├── Renderer / Validator
  │           ├── ProcessFactory
  │           └── RuntimeClient
  ├── GenerationStore (文件发布)
  ├── Profile/Operation repository (SQLite)
  └── Coordinator (A3 单操作锁)
```

- `internal/core` 定义跨 core 的最小类型，不知道 YAML、HTTP 路径和进程参数。
- `internal/mihomo` 实现 adapter。现有 `Client` 暂留，A6/A7 再逐项迁移业务方法。
- `internal/daemon` 组合 adapter、generation store 和仓储，实现单活动实例与恢复事务。
- `internal/config` 只计算路径，不执行文件写入。

## 2. Core 契约

核心对象采用类型化值：

- `ProfileSnapshot`：profile 标识、revision、模式、external 路径、端口、类型化代理/规则最小集合。
- `RenderedConfig`：内容、SHA-256、建议 controller endpoint；不暴露 YAML map。
- `RuntimeSpec`：generation/config/log 路径、controller endpoint 和预期源摘要。
- `Process`：PID、`Wait()`、`Signal()`；生命周期只操作该句柄。
- `RuntimeClient`：`Ready(ctx)` 和后续可扩展的 typed runtime 查询。
- `Adapter`：`Type()`、`Render()`、`Validate()`、`Start()`、`Runtime()`。

不在首个适配器阶段为 Xray/sing-box 预设能力矩阵、订阅解析器或任意扩展字段。第二个 adapter 出现时再演进接口。

## 3. Generation 布局与发布

```text
${XDG_DATA_HOME}/mihomo-manager/
└── generations/
    └── <profile-id>/
        ├── <generation-id>/
        │   ├── config.yaml
        │   └── metadata.json
        └── current.json

${XDG_STATE_HOME}/mihomo-manager/
└── core/
    ├── mihomo.log
    └── runtime.json
```

`generation-id` 由 profile ID、revision 和配置 SHA-256 派生，避免时间戳导致相同输入重复发布。写入流程：

1. 在 profile generation 目录内创建 `0700` 临时目录。
2. 写 `0600` 配置和 metadata，逐文件 `fsync`。
3. 运行静态验证与 core 原生验证。
4. `fsync` 目录后 rename 为不可变 generation 目录。
5. 原子替换 `current.json` 指针；切换事务真正成功后再将其标记为 active runtime。

`metadata.json` 仅含版本、profile ID/revision、core 类型、模式、内容摘要和创建时间，不复制订阅 URL、节点密码或完整代理内容。

external/legacy 不复制或发布源配置。运行前分别读取摘要并验证，构造只读 `RuntimeSpec`；若 validate 与 start 之间摘要变化，返回冲突。

## 4. mihomo 实现

### 4.1 渲染与验证

- managed 最小 YAML 使用 `yaml.Node` 或稳定结构体，固定字段和切片顺序；不依赖 map 遍历顺序。
- 管理字段包括 controller、端口、mode、proxies、proxy-groups 和 rules；A4 只渲染支持测试闭环的最小模型。
- 静态验证使用结构化解析检查：配置非空、controller 是 loopback host:port、端口互不冲突、代理组引用存在、最终规则目标存在。
- 原生验证执行 `<mihomo> -t -d <generation-dir> -f <config>`，受 context timeout 控制，收集有限长度 stderr 并返回分类错误。

### 4.2 进程

- `exec.CommandContext` 不用于长期进程，以免调用上下文完成时立即 kill；使用 `exec.Command` + 显式启动和 `Wait`。
- Linux 保持 `SysProcAttr.Setsid=true`；非 Linux 使用平台桩，保证包可编译但不承诺支持。
- stdout/stderr 写入权限 `0600` 的 manager 日志。
- 停止流程向所持 PID 发送 SIGTERM，等待超时后发送 SIGKILL，再等待 `Wait` 收割；已退出视为幂等成功。

### 4.3 Runtime API

- adapter 只接受 loopback HTTP endpoint，禁止 hostname 解析后可能指向外部地址的模糊形式。
- HTTP client 设连接/总超时、response body 上限和非 2xx 错误；就绪轮询同时监听 process exit、context 取消和 deadline。
- 首版 Ready 使用 `/version` 与 `/configs`，只解码确认身份/可用性的最小字段。

## 5. 切换状态机

```text
prepare -> validate -> stop-old -> start-new -> ready -> commit -> succeeded
                                |          |
                                +-- error -+
                                      |
                                rolling_back
                                  |       |
                             old ready   restore failed
                                  |       |
                            rolled_back  failed
```

- `CoreManager.Activate` 在 coordinator 锁内创建 operation，recovery JSON 记录旧 profile、generation/config 摘要和是否原本运行。
- 旧实例在新候选完成全部验证后才停止。
- 新实例 ready 前不调用 `SetActiveProfile`，不写 active runtime 指针。
- 失败后只使用记录的旧 `RuntimeSpec` 和 adapter 重新启动旧实例；不通过进程名扫描。
- 恢复成功仍向调用方返回原始切换错误，并把 operation 标记为 `rolled_back`；恢复失败返回组合错误并设置 supervisor `failed`。
- daemon shutdown 调用 supervisor `Close`，只停止当前句柄。daemon 启动时 supervisor 默认 stopped，不读取 profile 后自动启动。

## 6. 兼容与演进

- 现有 `mihomo.Client.Start/Stop/...` 暂不删除，避免本任务改变 TUI。新代码不得调用其 `pgrep`/`pkill` 路径。
- A5 使用 external/legacy config reference 接入迁移；A6 增加 core/config CLI 和 IPC；A7 替换 TUI 直接依赖。
- 本任务若需要 SQLite 变更，只允许追加 migration；优先复用 `operations`/`settings` 和 generation metadata，避免为短期 runtime 状态扩 schema。

## 7. 失败与回滚

- 配置写入或验证失败：删除临时 generation，旧实例不变。
- 新进程启动/就绪失败：停止新句柄并尝试恢复旧 RuntimeSpec。
- SQLite commit 失败：视为切换失败，停止新实例并恢复旧实例，不能留下“运行新、数据库指向旧”的静默分叉。
- daemon 退出：停止持有进程；若停止超时，返回 cleanup 错误并保持 runtime 状态可诊断。
- 代码级回滚可移除新 adapter/CoreManager 装配而不触碰旧 `Client` 和用户配置。

## 8. 测试策略

- core/mihomo 单元：golden YAML、静态验证、HTTP 边界、命令参数、进程信号。
- generation：临时 XDG、权限、摘要、幂等、原子失败注入、external 源摘要变化。
- daemon：fake adapter/process/repositories 验证完整状态机和 operation 记录。
- 契约：仓库内假 mihomo helper 支持 `-t`、启动 HTTP controller、提前退出和忽略 SIGTERM 场景。
- 全量：race、vet、Linux amd64/arm64 `CGO_ENABLED=0` build；所有默认测试不依赖宿主 mihomo。
