# A5：legacy 迁移、兼容层与回滚

## 目标与用户价值

让现有 1.x `mihomo-manager` 用户可以安全地被 2.x 终端 CLI 识别和管理：迁移过程保留原配置与运行行为，失败可恢复，2.x 不会把无法确认归属的文件误转换为 managed 配置。

## 已确认事实与约束

- 父任务要求 Alpha 只实现 `migrate plan|apply|status|rollback`，`convert` 不在本子任务范围。
- 领域层已经定义 `managed`、`external`、`legacy` 三种档案；`legacy` 只能由升级迁移创建，`ConfigPath` 指向旧配置文件，不能被新建为普通档案。
- 2.x 的 `CONFIG_DIR` 继续指向旧 mihomo 配置目录，不与 manager 的 XDG 配置目录混用。
- 旧 1.x 默认文件位于 `CONFIG_DIR`：`config.yaml`、`config.yaml.bak`、`subscription.url`、`whitelist.yaml`、`mihomo.log`、`node_speed.txt`、`fastest_node.txt`，并可能有时间戳 `.bak` 文件。
- 旧写操作先备份 `config.yaml`，写入失败或配置验证失败时恢复；订阅 URL 和白名单文件分别保存，白名单还会同步注入配置规则。
- daemon 是状态、文件写入和 core 控制的唯一写入者；daemon 不可用时 CLI/TUI 不得直接读取 SQLite、修改配置或控制进程。
- managed 配置写入 generation 目录；external/legacy 源文件只读或由兼容层按旧语义备份后写回，不能被 A5 自动转换或重写为 managed YAML。
- 迁移不得把订阅 URL、节点 URI、密码或完整配置内容写入日志；路径、大小、权限和 SHA-256 摘要可输出。
- 迁移不应主动停止、启动或替换旧 core；只能在可安全判断时记录运行状态，保持升级前 stopped/running 状态。

## 需求

### R1 发现与计划

daemon 提供只读发现/计划服务，识别 `CONFIG_DIR`、旧配置、订阅、白名单、备份、日志/测速文件、mihomo 二进制、API 端口和可观察的运行状态。`migrate plan --output table|json` 必须列出将读取、创建、备份、保持不变的路径及风险，不泄漏敏感值；不存在的文件标记为 absent，不因空环境失败。

### R2 恢复点与 legacy 元数据

`migrate apply` 在任何新状态写入前创建带 UTC 时间戳和唯一 ID 的恢复点及不可变 manifest，保存旧文件的受保护内容，以及相对路径、存在性、权限、大小和 SHA-256；恢复点目录和文件权限遵循 `0700/0600`。应用成功后创建稳定 ID 的 `legacy` profile metadata，并记录迁移 operation、源目录和环境变量作用域。不得自动修改或重排旧 YAML。

### R3 校验、幂等与失败保护

apply 在保留源文件后执行 mihomo 配置静态/原生验证；有效配置标记 ready，并仅在当前没有活动档案时激活 legacy profile。空/无效配置创建恢复点和 failed operation 后返回 `VALIDATION_FAILED`（退出码 6），legacy profile 不激活，也不得删除或覆盖旧文件。重复 apply 必须返回已有迁移状态或冲突，不得重复覆盖恢复点；中断或进程崩溃后可由 status 识别 incomplete，并能安全继续或回滚。迁移失败时旧 mm/core/config 的存在性、权限和摘要保持不变。

### R4 legacy 兼容 service

在 daemon 内包装现有 1.x 行为：配置 validate/backup/restore/edit、订阅 URL show/set/update、白名单 list/add/remove，以及旧配置的 reload/start/stop/status 所需的只读观察。所有写入遵循“时间戳备份 -> 写临时文件/原子替换 -> 验证 -> 失败恢复”规则；不得调用 `pgrep/pkill` 扫描或误杀非本次管理的 core。

### R5 回滚

回滚必须显式传入 `--restore-point <id>`，不自动选择最近恢复点。它只恢复迁移创建的 manager 状态和用户明确选择的恢复点内容，不删除用户在迁移前已有的文件；恢复前核对 manifest 摘要，发现源文件在迁移后被外部修改时停止并报告冲突。回滚后 legacy profile/迁移标记状态明确，1.x 可以继续读取原 `CONFIG_DIR`；2.x 新增数据可保留但不得被 1.x 依赖。

### R6 环境变量契约

在 CLI 帮助、JSON 结果和文档中明确：`CONFIG_DIR` 选择 legacy 源目录；`MIHOMO_BIN` 只选择验证/受控 core 二进制；`MIHOMO_API_PORT` 只影响 loopback external-controller 探测；`EDITOR` 只用于显式 edit，迁移不会自动启动编辑器。环境变量值不写入秘密日志。

## 验收标准

- [x] Good：隔离 HOME 的空环境可 plan；apply 创建可查询恢复点和 failed operation，返回退出码 6，legacy profile 不激活且不创建伪造配置内容。
- [x] Good：有效旧配置、订阅 URL、白名单和现有备份可发现；apply 后 2.x status 可管理 legacy，旧文件 SHA-256/权限未改变。
- [x] Base：无效旧配置能给出稳定的 validation error，迁移 operation 可查询，旧文件摘要不变。
- [x] Base：重复 apply、daemon 重启和 apply 中断不会重复写入或产生不可追踪状态；status 明确 pending/running/failed/succeeded/rolled_back。
- [x] Base：回滚恢复点后，旧 `mm` 读取的配置、订阅和白名单与迁移前一致，恢复点 manifest 通过校验。
- [x] Bad：恢复点缺失、manifest 摘要冲突、路径越界、权限不安全、非 loopback API 或未知文件归属均拒绝操作，不猜测为 managed。
- [x] 所有迁移测试使用临时 HOME/XDG、fake mihomo 和临时 Unix socket，不读取真实用户配置、不启动真实 core；`go test ./...`、`go test -race ./...`、`go vet ./...`、`CGO_ENABLED=0` 构建通过。

## 不在范围内

- `migrate convert` 及任何自动 YAML/节点/规则规范化。
- TUN、系统代理、定时更新、远程同步、多 core 和安装器完整回退（由后续 A6-A8/B 阶段负责）。
- 删除旧 1.x 二进制、旧配置目录或用户手工备份。
