# M3 实现模式事务与 daemon API

## Goal

让 daemon 以单写者事务可靠设置和查询 global/rule/direct，并在配置、运行态或恢复失败时给出真实、可机器判断的结果。

## Dependencies

- 依赖 M2 的 RoutingPolicy 候选生成、原生验证和订阅 overlay。

## Requirements

- 领域层区分 RoutingMode 与 ProfileMode，只接受 `global|rule|direct`。
- 提供配置模式、runtime 模式、core 状态、有效组/节点、规则集健康、活动连接数和 warnings 的 typed status。
- 设置模式时执行摘要冲突检查、备份、候选验证、原子写入、runtime 更新和核验。
- core stopped 时只保存配置并报告下次启动生效；不启动 core。
- core running 时核验 `/configs`；Rule 模式还核验 provider/rules 已加载。
- 默认不关闭连接；显式选项仅关闭 mihomo 活动连接。
- 失败恢复原配置、expected 摘要和 runtime mode；恢复失败返回稳定机器错误并标记 failed。
- 仅支持活动 legacy profile；external 明确 unsupported，managed 留待后续实现。
- 暴露版本化 Unix IPC，修改请求保持 request ID 幂等。

## Acceptance Criteria

- [x] M3-AC1：三种枚举输入 round-trip，非法值映射输入错误且无状态变化。
- [x] M3-AC2：core stopped 设置成功只改变已验证配置，status 显示 runtime unavailable/下次生效。
- [x] M3-AC3：core running 设置后 `/configs` 与配置 mode 一致；Global/Direct/Rule 有效目标正确。
- [x] M3-AC4：默认不调用 close connections；显式选项只调用 mihomo close API 并报告结果。
- [x] M3-AC5：文件写入、原生验证、runtime 更新、runtime 核验、expected 刷新各失败点均恢复旧状态。
- [x] M3-AC6：恢复失败返回 `RESTORE_FAILED`，不得返回成功或旧 runtime 的伪状态。
- [x] M3-AC7：并发 mode/core/subscription 写操作被同一操作协调器串行化；request ID 重放不重复切换。
- [x] M3-AC8：IPC 方法、body 限制、协议、错误分类和同 UID 契约测试通过。

## Out of Scope

- CLI/TUI 展示；由 M4 完成。
- 完整连接 DTO/follow；由 M5 完成。
- managed/external profile 写能力。
