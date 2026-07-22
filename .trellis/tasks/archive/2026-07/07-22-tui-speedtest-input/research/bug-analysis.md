# Bug Analysis: TUI 测速期间按键看似阻塞

## 1. Root Cause Category

- **Category**: D - Test Coverage Gap，同时包含 E - Implicit Assumption。
- **Specific Cause**: TUI 没有被通用 `busy` 锁住，但 `t` 和 `a` 共用“取消当前流并替换”的入口，且活动页脚隐藏了按键反馈。既有 fake stream 会立即完成，只验证第二条 command 非空，默认“channel 调用非阻塞”等同于“长期测速期间交互正确”，没有覆盖未完成流中的输入、context 是否被误取消、串行顺序和 generation 迟到消息。

## 2. Why Fixes Failed

1. 仅确认 `nodeTestActive` 独立于 `busy`：只能排除键盘分发总锁，不能证明第二次按键采用了用户期望的状态转换。
2. 仅验证重复按键返回 command：旧实现确实会返回 command，但它会取消当前流；即时完成 fake 无法暴露这一语义差异。
3. 所有活动状态显示相同“测速中”：即使批量替换已经发生，用户也没有可观察反馈，行为看起来仍像没有响应。

## 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
|----------|-----------|-----------------|--------|
| P0 | Architecture | 使用 `idle/single/batch` 作为唯一测速模式事实来源；单测队列、抢占和结束都经集中状态转换 | DONE |
| P0 | Test Coverage | 使用不会自动结束的独立 channel，断言重复 `t` 不取消当前 context、队列有序去重并串行推进 | DONE |
| P0 | Test Coverage | 覆盖 single/batch 双向抢占、批量重测、失败继续、系统错误停止、异常 close 和旧 generation 消息 | DONE |
| P1 | Documentation | 在稳定 TUI 节点测速契约中明确按键矩阵、可见反馈和 stream 终止语义 | DONE |

## 4. Systematic Expansion

- **Similar Issues**: 日志 follow、实时连接和组合重启也属于长期异步流程；新增交互按键时不能只用预填充后立即关闭的 fake channel 验证。
- **Design Improvement**: action-like 异步状态应由一个枚举和集中转换函数拥有，避免布尔活动标记与模式字段重复表达。
- **Process Improvement**: TUI 长期流回归测试应分别断言“command 延迟执行”“旧 context 是否取消”“活动页是否即时反馈”“迟到消息是否隔离”。

## 5. Knowledge Capture

- [x] 更新 `.trellis/spec/backend/cli-contract.md` 的节点历史与显式测速契约。
- [x] 更新根 `AGENTS.md` 的可复用 TUI 测速运行约定。
- [x] 更新 README 快捷键与节点管理说明。
- [x] 增加长期未完成 stream 的状态机回归测试。
