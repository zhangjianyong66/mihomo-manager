# 规则集引导启动修复

## Goal

当活动 legacy 配置引用 manager CN 规则集、规则集尚未安装且设备只能经
mihomo 代理访问下载源时，用户仍可通过 `mm ruleset install` 完成首次安装，
不再被“Core 无法启动、规则集无法下载”的循环阻塞。

## Confirmed Facts

- `CapabilityService.CoreAction` 在启动前调用 `ensureCurrentRuleSetDependencies`；规则集不是 `installed` 时拒绝启动。
- `InstallRuleSetsForProfile` 会先尝试由 CLI 环境变量指定的代理下载；没有外部代理且 Core stopped 时无法访问受限下载源。
- 活动 legacy 配置是 external config：源 `config.yaml` 必须保持只读，启动前和启动时都需要 SHA-256 核验。
- CoreManager 已负责受管进程、端口检查、就绪检测、Coordinator 串行化和失败恢复；新流程必须复用这些边界，不能使用 PID 扫描、直连 mihomo API 或配置直写。

## Requirements

- R1：规则集未就绪且 Core stopped 时，`mm ruleset install` 自动尝试启动一个仅用于下载的受管引导 Core；已安装规则集或已运行 Core 保持现有行为。
- R2：引导配置从当前 legacy 配置生成临时私有副本，保留代理节点、代理组、监听端口与 external-controller，但移除 manager CN providers、依赖它们的规则和 DNS policy，并以 global 模式运行，使下载流量经现有订阅代理出口。
- R3：引导配置不得覆盖或改写用户 `config.yaml`、profile revision、恢复点和正式运行 generation；临时文件及目录遵守 `0700/0600` 并在流程结束后清理。
- R4：引导 Core 就绪后，规则集下载继续使用 `127.0.0.1` 的匹配 mixed/http/socks listener；下载、SHA-256、格式校验和成对原子发布沿用现有规则集事务。
- R5：规则集发布成功后，停止引导 Core 并启动正式配置；最终 Core 为 running，且正式 rule 配置中的两个 manager providers 已加载。
- R6：引导启动、下载、发布或正式启动任一步失败时，停止引导进程并清理临时文件；原本 stopped 的 Core 必须仍为 stopped，正式配置与旧规则集对保持不变。
- R7：并发 Core/配置/路由操作必须由既有 Coordinator 排斥；Core 已 running 时不创建第二个引导进程。
- R8：CLI/TUI 安装过程展示引导、下载、恢复正式 Core 和失败阶段；网络或代理错误应显示可执行的具体原因，不应只显示泛化的“mihomo 或 legacy 操作失败”。

## Acceptance Criteria

- [ ] 在临时 legacy 配置中放入 manager providers 但不放规则集文件，且模拟受限下载源时，安装流程能启动引导 Core、经其本地 listener 获取两份规则集并启动正式 Core。
- [ ] 成功后源配置字节和 SHA-256 不变，规则集成对有效、权限为 `0600`，临时目录不存在，Core 运行的是正式配置。
- [ ] 引导就绪失败、下载失败、摘要/格式校验失败、发布失败和正式 Core 启动失败均具备回归测试；断言不残留受管进程或临时文件，原 stopped 状态和旧规则集保持。
- [ ] Core 已运行、规则集已就绪、没有 manager provider 的配置和外部环境代理可用的既有路径保持兼容。
- [ ] CLI JSON 流与 TUI 均能显示新增阶段；失败响应保留安全、可行动的底层错误原因。

## Out of Scope

- 不提供无法联网时的离线规则集镜像或绕过网络限制的第三方服务。
- 不修改用户的订阅、节点选择、系统代理、环境代理或正式路由策略。
- 不改变规则集固定来源、摘要、下载重试和成对发布的安全约束。
