# M5 技术设计：活动连接与入口诊断

## 1. runtime 解码

`internal/mihomo` 拥有 external-controller 原始 DTO 和规范化：

```go
type RuntimeConnection struct {
    ID string
    Host string
    DestinationIP string
    DestinationPort int
    Network string
    Rule string
    RulePayload string
    Chains []string
    Upload int64
    Download int64
    Start time.Time
}
```

host 为空时显示 destination IP；chains 原顺序保留并单独计算 final node，不能假设数组永远含组名。缺失/额外字段兼容，负数计数、非法时间和超大响应拒绝或安全归零并附 warning。

## 2. snapshot 与 follow

IPC：`GET /v1/connections` 返回当前 typed slice；`GET /v1/connections/follow` 使用 NDJSON。

daemon follow 每秒轮询一次，以 connection ID 保存有界上一快照：

- 新 ID -> `open`。
- counters/rule/chains 变化 -> `update`。
- 旧 ID 消失 -> `closed`，携带最后快照。
- 客户端取消/daemon 结束 -> best-effort `done`；上游错误 -> `error`。

事件 payload 由 app 定义并在 IPC 边界解码一次。活动 map 设置合理上限，超过时终止为结构化 upstream/limit error，不无限增长。无连接时保持等待，不发送空噪声。

## 3. CLI/TUI

非 follow：table/json。follow：默认 text，可选 NDJSON；text 每行包含时间、action、target、rule、chain 和累计流量。NDJSON 使用既有 `mm/v1` stream event，不混入进度文本。

TUI 页面维持 ID -> row map，Update 消费 open/update/closed；View 只排序/渲染。支持滚动与 ESC 取消，最多保留当前活动连接和短暂关闭提示，不保存完整历史。

## 4. 入口检测

`internal/platform` 定义只读 `ProxyInspector`，Linux GNOME 实现调用受控 `gsettings get`；命令缺失、无 DBus 或非 GNOME 返回 unknown，不视为模式错误。CLI/TUI 进程本地读取标准 proxy env，只解析并投影 scheme/host/port，丢弃 userinfo/path/query。

daemon/runtime 返回 mihomo listeners：HTTP port、SOCKS port、mixed port 和 loopback host。app 组合比较：

- HTTP/HTTPS proxy 只能匹配 http 或 mixed listener。
- SOCKS/ALL_PROXY 只能匹配 socks 或 mixed listener。
- host 必须是等价 loopback，端口必须一致。

状态输出分 `matched|mismatched|disabled|unknown`，并指出来源和端点。检测不会查询或终止端口占用进程；xray 名称只在已知外部事实/用户输出中出现，不由 PID 猜测后控制。

## 5. 隐私与错误

- 活动 host/IP 属于敏感运行信息，只在本地显式命令/TUI 显示，不进入 daemon journal 或 SQLite。
- runtime/API 错误不附完整响应 body。
- proxy URL 默认只保留 scheme/host/port；任何凭据始终脱敏，即使 `--show-secrets` 也不需要支持。

## 6. 测试

httptest runtime 覆盖真实 JSON 形状、null、字段缺失、大小上限和取消。fake inspector/env 覆盖 GNOME manual/none、协议错配、loopback IPv4/IPv6、userinfo 脱敏和 xray 端口错配。TUI/CLI 使用事件 fixture，不连接真实 controller/gsettings。
