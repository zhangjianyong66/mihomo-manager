# 延迟安装 CN 规则集：代码证据

## 当前安装路径

- `scripts/install.sh:17-20` 分别硬编码默认规则集来源、固定 commit 和两份 SHA-256。
- `scripts/install.sh:379-417` 校验自定义来源与成对摘要；`scripts/install.sh:474-505` 使用 `type: file` 临时配置执行 mihomo 原生格式校验。
- `scripts/install.sh:508-539` 只信任与 `install-state` 路径、摘要和格式一致的旧缓存。
- `scripts/install.sh:556-627` 下载后执行摘要/格式校验并成对发布；首次无缓存失败时终止安装。
- `scripts/install.sh:1158-1167` 在安装 `mm` 和配置 daemon 之前无条件执行规则集下载，因此网络失败阻断基础安装。
- `scripts/bootstrap.sh` 在临时目录运行安装器并退出后删除源码，远程用户不能依赖仓库内的后续 Make target。

## 当前路由与 Core 行为

- `internal/mihomo/routing_policy.go:16-20` 另行定义 provider 名称和可变 `meta` URL，和安装器的固定版本形成两个事实来源。
- `internal/mihomo/routing_policy.go:117-143` 对三种模式都写入两个 manager providers/rules；`internal/mihomo/routing_policy.go:164-170` 将其渲染为 `type: http`，文件缺失时 mihomo 可以隐式联网。
- `internal/legacy/mode.go:86-160` 的模式候选在发布前执行原生校验；`internal/legacy/mode.go:352-399` 只在运行中 Rule 模式核验两个 provider 是否已加载。
- `internal/daemon/capability.go:328-360` 的 Core start/restart/reload 当前没有 manager 规则集就绪门禁。
- `internal/daemon/capability.go:214-244`、`491-510` 表明模式与其他写操作共享 Coordinator，规则集发布也必须复用该单写者边界。

## 当前跨层入口

- `internal/app/capabilities.go:151-157` 的 daemon capability 已注入调用进程 `os.LookupEnv`，可在客户端解析代理环境后只把脱敏 endpoint 发送给 daemon。
- `internal/platform/proxy.go:194-225` 当前代理投影会丢弃 userinfo/path/query；规则集下载必须在投影前显式拒绝认证代理，不能静默丢失凭据。
- `internal/ipc/protocol.go:25` 将单个 JSON/NDJSON body 限为 1 MiB，规则集文件不能经 IPC 上传；应由 daemon 按受校验来源下载。
- `internal/daemon/server.go:149-187` 集中装配 capability 和 `/v1/` Unix IPC；`internal/daemon/capability_http.go:637-708` 集中映射结构化错误。
- `internal/cli/root.go:30-69` 只注入应用 capability；新命令应保持 Cobra 表层不直接读写文件或请求规则集源。
- `internal/tui/model.go:18-51` 使用窄 capability；`internal/tui/model.go:1345-1399` 的“配置管理”菜单是 CN 规则集页面的既有归属点。

## 可复用约束

- `internal/config/config.go:5-24` 已集中定义规则集目录和两份目标文件，适合追加规则集 metadata 路径。
- `internal/daemon/capability_http.go` 已有 request ID、NDJSON flush、typed error 模式可复用。
- `internal/legacy/mode.go` 已有独立恢复 context、runtime reload/verify 和 core failed 语义；规则集运行中发布应保持同样的补偿标准。
- `scripts/tests/test_install.sh` 已覆盖自定义摘要、符号链接拒绝、成对发布、旧缓存复用和 install-state round-trip，应扩展而不是另起测试框架。
