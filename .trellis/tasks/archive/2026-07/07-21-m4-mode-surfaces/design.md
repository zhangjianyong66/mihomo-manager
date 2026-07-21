# M4 技术设计：模式 CLI 与 TUI

## 1. CLI 命令

`internal/cli` 新增 `newModeCommand`，只做参数与 presenter 适配：

```text
mm mode status [--profile ID] [--output table|json]
mm mode set <global|rule|direct> [--profile ID]
    [--close-connections] [--output table|json]
```

成功 kind 分别为 `RoutingModeStatus`、`RoutingModeChange`。table 输出固定字段，不根据终端宽度改变含义。warnings 使用 envelope 的 warnings 和 table 末尾“警告”行；不存在秘密字段，不注册 `--show-secrets`。

## 2. TUI 依赖迁移

`app.RunInteractiveContext` 不再加载 `config.Paths` 或构造 `mihomo.Client`，改为接收注入的窄接口。`cmd/mm` 只构造一次 `DaemonCapabilities` 并同时注入 CLI 与 TUI。

TUI interface 按实际页面组合 CapabilityAPI 的子集，测试用 fake 实现。配置编辑保留“客户端本地启动 EDITOR”的安全模式：app helper 通过 daemon 读取内容/SHA，创建 `0600` 临时文件、运行 editor，再回传 expected SHA；daemon 不占终端。

迁移顺序按页面纵向完成，发布前删除 direct client 字段和所有 `mihomo.New`、原始配置路径、进程/API 副作用。长流仍使用 context/channel 消息。

## 3. 运行模式页面

主菜单增加“运行模式”。进入时异步读取 status，页面稳定显示：

```text
配置模式：Rule
运行模式：Rule / Core 已停止
实际路径：非大陆 -> 🌐 代理 -> <节点>
规则集：CN Domain 正常，CN IP 正常
活动连接：N

○ 全局代理
● 规则分流
○ 全局直连
```

选择后先展示简短确认和“关闭现有连接”复选状态，再提交 SetMode。busy 状态固定布局，防止文本变化导致跳动。失败结果保留原选中项并显示机器错误对应的中文说明。

## 4. 错误与引导

- daemon unavailable：提示 `mm daemon status/start`，不 fallback。
- no active legacy profile：提示 `mm migrate plan`、`mm migrate apply`。
- stopped：显示配置已保存、下次启动生效。
- connection close warning：模式成功与关闭失败分开显示。
- restore failed：醒目标记状态不确定，提示查看 daemon/core status，不建议重复盲切。

## 5. 测试

CLI 使用 fake CapabilityAPI 覆盖 IO、envelope、warnings 和错误。TUI 将 mode 页面状态转换、三模式选择、busy、取消、失败恢复和现有页面迁移拆为 model 测试，不启动真实 Bubble Tea 终端或 editor/core。
