# TUI 连续测速队列设计

## Scope

改动限定在 `internal/tui` 的节点列表状态机、测试及稳定交互文档。继续通过现有 `Capabilities.TestNodes` 调用 daemon；不修改 app DTO、HTTP 路由、NDJSON 事件或 mihomo 测速实现。

## Confirmed Cause and Gap

当前实现没有用 `busy` 锁住节点测速键盘，但测速中再次按 `t`/`a` 采用“取消并替换”，且每轮都只显示相同的“测速中”。现有测试只证明第二个 command 非空，并使用立即返回的 fake；没有覆盖长期未完成流、串行下一项、可见队列状态和模式切换。因此真实操作看起来像按键没有生效，也无法连续安排多个单测。

## State Model

在现有字段上增加最小的 TUI 本地状态：

- 测速模式：`idle`、`single`、`batch`；只用于状态转换和渲染。
- 当前单测节点：单测模式下标识正在执行的节点。
- 单测队列：`[]domain.NodeID`，保持按键顺序；规模天然受当前组节点数约束。
- 队列集合：可通过小型查询函数扫描当前节点和切片，无需额外 map；节点数有限，避免维护两个事实来源。
- generation、cancel、done/total 和 `nodeResults` 继续沿用现有字段。

队列只存在于节点页内存。进入或离开代理组时重置；任何结果仍写入现有 `nodeResults`，不写 SQLite。

## Input Transitions

### `t`

- idle：验证光标项后，启动该节点单测。
- single：当前节点或队列中已存在时不重复加入，并显示提示；否则追加到队尾，当前流继续。
- batch：取消批量并递增 generation，清空旧队列，立即启动光标节点单测。
- “返回”或不可测速项：不改变当前流或队列，只显示既有输入提示。

`t` 不移动光标。用户用方向键选中下一个节点后按 `t` 入队，避免隐式改变导航位置。

### `a`

验证当前组存在可测速节点后，无论 idle/single/batch 都清空单测队列，取消当前流并以新 generation 启动完整批量测速。该动作立即更新模式和页脚，即使 daemon 尚未返回首个事件也能看到按键生效。

### `Esc`

single/batch：清空队列、取消当前流、递增 generation、保留结果并停留在列表。idle：沿用现有返回分组列表行为。

## Stream Transitions

- started：只有 generation 与当前轮次一致且模式非 idle 时才等待事件。
- result：只有当前 generation 才更新 `nodeResults` 和进度。
- single finished/channel closed：先把当前流置为完成；队列非空时弹出队首，通过新的 generation 和派生 context 返回下一条 `tea.Cmd`；队列为空时进入 idle 并显示完成摘要。
- batch finished/channel closed：进入 idle 并显示完整进度摘要。
- 单节点失败/超时：它是 `Result.Status=failed` 的正常事件，随后 finished，继续队列。
- 系统性 event error：取消当前 context、清空队列、进入 idle 并显示错误；不继续发送后续请求。
- 旧 generation 的任意消息：直接丢弃，不修改 cancel、模式、队列、结果或文案。

正常 finished 后不再读取该 channel，因此不依赖随后出现的 close 消息。异常 close 按当前轮次完成处理，避免队列永久停留在 active。

## Rendering

页脚继续保持两行：导航行和操作状态行。

- idle：`t 单测 | a 批量 | Esc 返回`，可追加最近摘要。
- single：显示当前节点的截断名称、单项进度、`待测 N` 和 `Esc 取消`。
- batch：显示 `批量测速`、`done/total` 和 `Esc 取消`。
- 重复入队等提示在 active 状态下也必须可见，不能沿用当前“仅 idle 显示 message”的限制。

所有动态节点名和提示经现有 `fitFooter`/截断逻辑约束，不按视口缩放字体，不引入新页面或卡片。

## Compatibility and Safety

- 不改变 `app.NodeTestRequest` 或 daemon 协议，新旧 daemon 行为不受影响。
- 同时最多保留一个活动 HTTP stream；单测队列不会扩大 daemon 并发。
- 替换先取消旧 context，再启动新 context；generation 负责屏蔽取消竞态中的迟到消息。
- 不触碰真实用户配置、controller 状态或已安装二进制，测试全部使用 fake capability。

## Documentation

更新 `.trellis/spec/backend/cli-contract.md` 的稳定 TUI 节点测速契约，并在根 `AGENTS.md` 记录可复用的运行约定。README 如现有描述不足以表达串行队列，则同步补充一句。

## Rollback

回滚只需恢复 TUI 队列字段、状态转换、测试和文档。没有 schema、配置、IPC 或持久数据迁移，也不会留下运行态资产。

