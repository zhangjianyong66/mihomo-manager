# M5 提供实时链路与入口诊断

## Goal

让用户基于 mihomo 真实运行态确认每条活动连接命中的规则、策略组和最终节点，并识别桌面/终端代理是否实际进入 mihomo。

## Dependencies

- 依赖 M3 的 typed runtime/IPC 基础；TUI 页面接入依赖 M4。

## Requirements

- runtime 类型化解析 mihomo `/connections`，将 `connections: null` 视为空列表。
- CLI 提供 `mm route connections` 快照与 `--follow` 流；TUI 提供实时连接页面。
- 每条连接展示目标、网络、命中规则/payload、完整 chains、最终节点、上传和下载量。
- follow 通过 daemon 轮询产生 typed open/update/closed 事件，以 NDJSON terminal event 结束并支持取消。
- 连接详情和历史不写数据库、不写持久日志；默认内存有界。
- 模式状态和 TUI 展示 mihomo listener、GNOME system proxy 与当前 CLI 环境代理的规范化端点。
- 入口不匹配时警告普通应用不会使用 mihomo；检测失败标记 unknown，不影响模式本身。
- 诊断只读，不调用 gsettings set、不停止 xray、不占用端口、不展示代理 URL 凭据。

## Acceptance Criteria

- [x] M5-AC1：空/null、多连接、缺失 host、IPv4/IPv6、未知 chains 和超限响应解析测试通过。
- [x] M5-AC2：快照 table/json 展示真实 rule/chains/counters；不根据静态配置伪造节点。
- [x] M5-AC3：follow 输出 open/update/closed 和 done/error，seq 单调、每行完整 JSON、取消无 goroutine 泄漏。
- [x] M5-AC4：TUI 实时页能刷新、滚动、退出并停止 producer，布局不会因计数变化跳动。
- [x] M5-AC5：GNOME HTTP/HTTPS/SOCKS 与环境代理按协议和监听端口正确匹配 mixed/http/socks listener。
- [x] M5-AC6：指向 xray/其他端口时产生明确 warning，但不修改系统设置或进程。
- [x] M5-AC7：userinfo/path/query 默认不进入 DTO、输出、日志或错误；连接历史不落盘。

## Out of Scope

- 历史流量数据库、统计报表和长期审计。
- 一键启用/恢复 GNOME 系统代理。
- 关闭单条连接、按进程归属连接或抓包分析。
