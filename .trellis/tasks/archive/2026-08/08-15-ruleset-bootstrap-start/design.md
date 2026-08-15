# 规则集引导启动设计

## 架构

在 daemon 的 ruleset capability 中编排引导事务；CoreManager 继续是唯一的 Core
进程所有者。为 external legacy 配置增加只读的临时 generation 准备能力：读取并
校验源配置、克隆 YAML、删除 manager-owned CN provider/rule/DNS policy、设置
`mode: global`，写入私有临时 generation 后使用既有 adapter 原生验证与启动。

引导过程不调用 `MarkActive`，因此不会把临时 RuntimeSpec 写为正式 current runtime
metadata，也不会更新 SQLite 活动 profile。临时进程停止后，正式 Core 使用现有
`Activate` 流程从原 external config 启动。

## 流程

```text
ruleset install
  -> Inspect: 已就绪 / 已运行 / 缺失
  -> 缺失且 stopped：Coordinator 获取锁，准备并启动 bootstrap Core
  -> 从 bootstrap listener 派生本地代理，下载并校验两个 .mrs
  -> PublishWithHook 原子发布
  -> 停止 bootstrap Core
  -> Activate 正式 external config
  -> 清理 bootstrap generation，发出成功事件
```

任一失败：停止 bootstrap Core，清理临时 generation；若正式 Core 尚未启动，最终
状态为 stopped。发布阶段沿用既有双文件恢复；正式启动失败不删除已成功发布的规则集，
但必须如实报告“规则集已安装、正式 Core 未启动”的状态。

## 边界与契约

- YAML 中 manager-owned 内容的识别和剥离放在 `internal/mihomo`，复用既有 `ParseRoutingPolicy` / `ApplyRoutingPolicy` 的 ownership 规则，避免 daemon 自己操作未类型化 YAML。
- `internal/core` 负责私有 generation 文件与 RuntimeSpec；`internal/daemon` 只编排启动、停止、发布和恢复。external 源文件不写入。
- 规则集安装事件添加稳定 phase，CLI/TUI 继续透传同一 typed stream，不添加第二套 IPC endpoint。
- 错误映射对下载失败保留经过脱敏的操作上下文（例如连接本地代理失败、GitHub 超时、HTTP 状态），不回显 URL userinfo、节点 URI 或响应体。

## 风险与处理

- 引导配置可能不含可用的 global 出口：引导 Core 可以启动但下载失败；报出“引导代理无法连接下载源”，并保持 stopped，不猜测或改选节点。
- 临时配置与正式配置使用同一监听端口：只允许原 Core stopped 时进入引导；全程由 Coordinator 排斥其他变更。
- 正式启动失败：引导已停止且源文件不变，报告分阶段失败；用户可修复配置后普通 `mm core start`，无需重新下载规则集。
