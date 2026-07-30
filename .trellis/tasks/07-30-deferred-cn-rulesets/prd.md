# 支持延迟安装 CN 规则集

## Goal

首次安装 Mihomo Manager 时不因 CN 规则集下载受阻而失败；用户应能先完成 `mm`、mihomo core 与 manager daemon 的安装，待代理网络可用后再安全安装 CN 规则集。

## Background

- 当前安装器在 `scripts/install.sh` 中先构建 `mm`、安装 mihomo core，再强制下载固定 commit 的 domain/IP 两份 `.mrs`，之后才配置 mihomo、安装 `mm` 并启用 daemon。
- 当前首次下载、摘要或 mihomo 格式校验失败且没有 `install-state` 记录的有效缓存时，整个安装立即失败；这正是 macOS 未建立代理环境时的实际故障。
- 当前新建的最小配置仅包含 `mode: rule` 与 `MATCH,DIRECT`，不引用 CN providers，因此完成基础安装本身不依赖规则集文件。
- 当前仓库没有独立的规则集安装/更新 CLI；稍后重跑完整安装器虽会再次尝试下载，但也会重复构建、core 与 daemon 升级编排。
- 既有规则集安全契约包括固定来源与摘要、mihomo 原生格式校验、`0700/0600` 权限、两份文件成对发布，以及升级失败时只复用与 `install-state` 一致的有效缓存。

## Requirements

- 基础安装不得因默认的 CN 规则集网络下载不可用而失败。
- 默认安装完全跳过 CN 规则集网络请求，不做可能阻塞的“尝试下载后降级”。
- 安装器保留显式 `--with-rulesets` 及等价 `MM_INSTALL_RULESETS=1`；显式选择时执行完整规则集事务，失败仍使整次安装失败。
- 延迟安装不得削弱现有来源校验、摘要校验、格式校验、文件权限和成对原子发布契约。
- 安装结果和提示必须明确区分“基础组件已安装”与“CN 规则集尚未安装”，并给出后续可执行入口。
- 提供持久化 CLI 命令 `mm ruleset install`，只安装/更新 CN 规则集，不重复构建 `mm`、安装 core 或编排 daemon。
- 提供只读命令 `mm ruleset status`，报告 `installed`、`missing`、`invalid` 或 `outdated`，并显示固定引用、目标路径及不含秘密值的校验结果。
- `mm ruleset install` 在本地正好是当前固定版本且来源记录、摘要、mihomo 格式和权限全部有效时直接复用，不发起网络请求；缺失、损坏或版本落后时才下载并修复。
- TUI 提供与 CLI 等价的 CN 规则集安装入口和结果反馈。
- TUI 入口位于“配置管理 > CN 规则集”，页面展示整体状态、固定版本和两份资产校验结果，并提供“安装/修复”和“返回”。
- TUI 执行安装/修复前展示来源、固定版本和可能的 Core 热重载影响并要求确认。
- 下载与校验阶段允许 TUI `Esc`/`Ctrl+C` 和 CLI `Ctrl+C` 取消且不得修改现有文件；成对发布开始后忽略调用方取消，使用有界事务上下文完成运行时核验或回滚，并持续报告阶段。
- 下载按当前 CLI/TUI 进程的 `HTTPS_PROXY`、`ALL_PROXY`、`HTTP_PROXY`，当前运行中 mihomo 的 loopback mixed/HTTP listener，最后直连的顺序选择网络路径。
- 本次下载使用的代理端点不得持久化；凭据不得出现在日志、DTO、错误详情或安装状态中，无法安全解析的代理设置必须明确报错。
- 本次不支持认证代理；仅接受无 userinfo 的 `http`、`https`、`socks5` 或 `socks5h` 代理 URL。发现用户名或密码时返回脱敏错误，不静默移除凭据或降级直连。
- CLI 与 TUI 启动进程继续通过 `MM_RULESET_BASE_URL`、`MM_RULESET_REF` 及成对的 `MM_RULESET_DOMAIN_SHA256`/`MM_RULESET_IP_SHA256` 覆盖可信来源；TUI 不提供来源编辑表单。
- 规则集缺失时，阻止切换到 `rule`、应用 CN 分流预设，以及启动已经引用 `mm-cn-domain`/`mm-cn-ip` 的配置；错误必须明确引导执行 `mm ruleset install`。
- 规则集缺失不得阻止不引用 CN providers 的 Core 配置，以及 `global`、`direct` 模式。
- 已存在且可验证的规则集缓存不得因延迟安装策略被破坏。
- 当运行中的 Rule Core 实际引用 manager CN providers 时，规则集发布后立即热重载并核验运行时；失败时恢复旧文件并重新加载旧状态，恢复失败进入明确 failed 状态。
- Core 已停止或当前配置未引用 manager CN providers 时，只发布已验证资产并报告下次相关切换/启动时生效；规则集安装不主动关闭现有连接。

## Acceptance Criteria

- [ ] 在没有 CN 规则集缓存且下载源不可达时，默认首次安装仍能完成 `mm`、mihomo core、配置与 daemon 的既有流程。
- [ ] `--with-rulesets` 和 `MM_INSTALL_RULESETS=1` 可在网络可用时保持一次性完整安装，并对失败返回非零退出状态。
- [ ] 默认安装不会留下零长度、单份或未经校验的规则集文件。
- [ ] 用户能在网络可用后通过明确入口安装两份规则集，并沿用既有完整安全校验与原子发布语义。
- [ ] 远程 bootstrap 删除临时源码后，用户仍可通过已安装的 `mm` 完成规则集安装，不依赖仓库或重跑完整安装器。
- [ ] CLI 与 TUI 对安装中、成功、失败和已有有效版本的状态表达一致。
- [ ] 重复执行 `mm ruleset install` 在本地资产完整有效时不产生网络请求或文件替换。
- [ ] `mm ruleset status --output table|json` 可稳定区分已安装、缺失、无效和版本待更新。
- [ ] CLI 与 TUI 从代理环境发起安装时使用调用进程看到的代理设置，而不依赖后台 daemon 的启动环境。
- [ ] 没有进程代理但活动 Core 提供 loopback mixed/HTTP listener 时，规则集下载可自动经该 listener 完成；两者均不可用时才直连。
- [ ] 所有会引入或依赖 manager CN providers 的入口在资产缺失时均返回稳定的结构化错误，不触发 mihomo 隐式网络下载。
- [ ] 不依赖 CN providers 的基础配置仍可通过校验并启动 Core。
- [ ] 运行中规则集更新只有在文件与 runtime 都核验成功后才报告成功；任一步失败不留下新旧混合文件或错误的成功状态。
- [ ] 发布前取消不改变规则集、install-state 或 runtime；发布后取消仍完成成功事务或完整恢复。
- [ ] 认证代理被拒绝时，输出和结构化错误不包含用户名、密码、URL path/query/fragment，也不继续发起直连请求。
- [ ] 安装摘要能准确显示规则集是已安装、复用缓存还是待安装，不输出空引用伪装成功。
- [ ] Linux/macOS、交互/`--yes` 和远程 bootstrap/local `make install` 的行为一致且有自动化测试覆盖。

## Out of Scope

- 不改变 CN 规则集内容、固定 commit、可信摘要或路由策略合成顺序。
- 不让安装器自动配置或启用系统代理。
- 不降低失败升级时复用旧缓存的验证条件。
