# M2 技术设计：Rule Policy 合成器

## 1. 模块边界

在 `internal/mihomo` 新增专用 routing policy 文件，提供纯变换与 IO 包装两层：

```go
type RoutingPolicy struct {
    Mode domain.RoutingMode
    CustomRules []string
    CustomProviders map[string]any
    Whitelist []string
    DNS map[string]any
}

func ParseRoutingPolicy(config map[string]any, whitelist []string) (RoutingPolicy, error)
func ApplyRoutingPolicy(config map[string]any, policy RoutingPolicy, paths config.Paths) error
```

纯函数不读 HOME、不调用 runtime，便于 golden 和属性测试。`legacy.Compatibility` 继续拥有备份、expected SHA、原生验证和恢复；旧 `Client` 只作为过渡 IO adapter，不再拥有另一份规则算法。

## 2. manager 所有权识别

manager provider 名固定为 `mm-cn-domain`、`mm-cn-ip`。以下视为 manager 规则并在合成前清理：

- 本机/局域网完整签名集合。
- `RULE-SET,mm-cn-domain,*`、`RULE-SET,mm-cn-ip,*`。
- 旧 `GEOSITE,CN,DIRECT`、`GEOIP,CN,DIRECT*`。
- 所有 `MATCH,*`。
- 从 `whitelist.yaml` 得到的直连域名规则。

其余规则视为 custom，保持原始字符串和相对顺序。provider map 中只替换两个 manager key，其他 key/value 原样保留。未知或格式错误规则不静默丢弃：若 mihomo 可验证则作为 custom 保留，否则候选整体失败。

## 3. DNS 策略

M2 先用 v1.19.28 做最小实验，固定支持字段后形成 golden。预期 manager 所有字段包括：

- `enable: true`、`respect-rules: true`。
- 国内 IP bootstrap resolver。
- `proxy-server-nameserver` 直连解析代理服务器，避免代理 DNS 自举循环。
- 境外加密 resolver 作为普通 nameserver，其连接按 routing rules 进入代理。
- `nameserver-policy` 中 CN provider 对应国内 resolver。

保留用户的 `enhanced-mode`、`fake-ip-range`、`fake-ip-filter`、IPv6 和不与 manager key 冲突的 policy。旧 `fallback`/`fallback-filter` 若会绕过 manager 分流则迁移为明确字段或移除，并在 config diff/warning 中说明。

## 4. 订阅 overlay

更新前解析旧 routing policy、两个组的 runtime selection 和白名单。订阅解析只产生 proxies 与候选名称；随后：

1. 重建 `🌐 代理`/`🎯 直连`组候选，不写入内置 GLOBAL。
2. 应用旧 policy 与 manager provider/DNS。
3. 原生验证并写入。
4. core 运行时受控 reload。
5. 对仍存在的 selected node 调用 group select 恢复；消失则选择排序后首个真实节点并返回 warning。

warning 必须进入 typed result，不能只写日志。M3/M4 再将其穿透 IPC/CLI。

## 5. 诊断兼容

静态 `route diagnose` 增加 `RULE-SET` 类型识别，但不手写 `.mrs` 二进制解析器。无法证明集合成员时返回低置信度和“以实时连接为准”，不得错误落到 MATCH 并宣称高置信度。M5 的真实 `/connections` 是权威命中证据。

## 6. 失败恢复

所有变更经 legacy compatibility mutation：捕获相关源文件、备份、候选验证、权限收紧、expected 摘要刷新。订阅内容、DNS URL 或规则验证失败时恢复旧 config/whitelist/备份状态，并不 reload 无效候选。
