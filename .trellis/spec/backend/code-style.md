# 代码风格

## Go 基础风格

- 所有 Go 文件以 `gofmt` 结果为准；当前 `cmd/` 和 `internal/` 均可通过 `gofmt -l` 零输出检查。
- 标识符、包名和导出规则遵循 Go 惯例；用户界面文案以中文为主，代码标识符使用英文。
- 导入按标准库、第三方库、项目内部包分组。可参考 `internal/app/run.go` 和 `internal/tui/model.go`。
- 小型入口保持薄：`cmd/mm/main.go` 只注入进程依赖并调用 `cli.Execute`，Cobra 命令和进程级错误映射位于 `internal/cli`，`internal/app/run.go` 只负责 TUI 装配。

## 依赖与边界

- 通过构造函数注入依赖：daemon 装配 `mihomo.Adapter`，`tui.New`/`NewWithContext` 接收窄化的 daemon capability；禁止在 TUI 内重新加载 HOME/XDG 路径或构造 `mihomo.Client`。
- TUI 不直接执行 `os.WriteFile`、`exec.Command` 或原始 HTTP 请求；业务副作用经 `internal/app.DaemonCapabilities`，本地编辑器由 `app.InteractiveCapabilities` 桥接。
- 路径和端口默认值集中在 `internal/config/config.go`，不要在不同业务方法中重复读取环境变量。
- 与 mihomo external-controller 交互统一经 `Client.call`，新增 API 方法应复用该入口处理地址、超时和状态码。

## 错误处理

- 发生错误时立即返回，不吞掉会影响正确性的错误；需要补充上下文时使用 `%w` 包装，例如订阅解析和应用路由后的配置测试。
- 仅在“尽力恢复”或幂等清理场景忽略错误，例如 `Restart()` 先停止服务、写配置失败后尝试 `RestoreConfig()`。
- CLI 错误只由 `internal/cli.Presenter` 输出到 stderr，`cmd/mm` 只应用 `cli.Execute` 返回的退出码；内部包返回 `error`，TUI 通过消息和结果页展示。
- 错误信息应描述失败动作或非法输入，不包含订阅密码、完整节点 URI 等敏感内容。现有 URI 解析错误通过 `truncate` 限制输入片段长度。
- 新应用错误使用 `app.Error`：`Category` 决定退出码，`Code` 是可细化机器码，底层 `Err` 不直接序列化。秘密字段必须通过 `cli.Secret` 和显式输出投影处理。

## 配置文件写入

- 修改 `config.yaml` 前先备份到 `config.yaml.bak`。通用写入入口是 `Client.writeConfigMap`。
- 写入失败时尝试恢复备份；对路由规则等影响运行的变更，写入后执行 `Client.TestConfig()`，失败则恢复原配置。
- YAML 通过 `gopkg.in/yaml.v3` 解码为 `map[string]any` 并重新编码；不要承诺保留字段顺序和注释。
- 需要维护规则顺序时显式构造切片。白名单规则必须在最终 `MATCH` 规则之前，相关实现为 `injectWhitelistRules`。

## 异步与 TUI

- 长耗时测速和日志跟随使用只读事件 channel，将进度、结果、错误和完成状态封装为消息类型，如 `NodeTestEvent`、`LogEvent`。
- 可取消操作使用派生 `context.Context`，TUI 返回页面时调用对应 cancel；daemon stream 必须随 context 关闭 HTTP body/channel。
- Bubble Tea `Update` 只处理状态转换，`View` 只渲染当前状态；所有 capability 调用封装为 `tea.Cmd`，不得在 `Update`/`View` 直接执行网络、文件或进程副作用。
- 新页面或流程优先复用现有 `page`、`actionCtx` 和消息模式，避免另建与主状态机并行的全局状态。

## 测试风格

- 测试与实现使用同一包，便于覆盖包内辅助函数和内部行为。
- 文件系统测试使用 `t.TempDir()`；HTTP 行为使用 `httptest.NewServer()`；测试完成后由 `defer server.Close()` 清理。
- 断言同时覆盖输出和不变量，例如路由测试既检查规则顺序，也检查旧 `MATCH`/CN 规则已清理。
- 修改配置的功能应覆盖失败回滚，参考 `TestApplyRouteCN_RestoreConfigWhenPostWriteTestFails`。

## Shell/Python 遗留代码

- 只有维护现有脚本时才遵循其本地模式：Bash 使用 `#!/bin/bash`、变量引用加双引号、公共常量和函数放在 `scripts/lib/common.sh`。
- 不要把新的 Go 产品功能再实现一份 Shell/Python 版本；避免 Go 与遗留脚本继续产生行为分叉。
