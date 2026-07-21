# M2 构建 Rule 分流与订阅保留

## Goal

建立唯一、可测试的 Rule 路由与 DNS 策略合成器，使订阅、白名单和兼容 CN 预设不再各自重写规则，并保证更新节点时保留用户路由意图。

## Dependencies

- 依赖 M1 提供规则集路径、固定资产和安装契约。

## Requirements

- 生成本机/局域网、自定义规则、白名单、CN domain/IP provider 和唯一 `MATCH,🌐 代理` 的固定顺序。
- 识别并清理旧 manager CN 规则、旧 `MATCH`、重复 provider/rule；保留其他 custom rule/provider 顺序。
- 生成经 mihomo v1.19.28 验证的 DNS 分流与防循环配置，保留兼容用户 DNS 项。
- 保留 `mode` 字段及 `GLOBAL`、`🌐 代理`、`🎯 直连`组结构；Global/Rule 使用独立节点选择。
- 订阅更新只替换节点，保留路由、DNS、白名单、custom rules/providers 和模式。
- 节点仍存在时恢复组选择；消失时确定性回退并返回 warning。
- 白名单增删、兼容 `route preset cn` 和订阅更新复用同一合成器，写入后执行原生校验和失败恢复。

## Acceptance Criteria

- [ ] M2-AC1：Rule golden 配置的规则顺序、provider 行为/路径/URL/interval 与父契约一致。
- [ ] M2-AC2：已有自定义规则/provider 保持相对顺序，重复 manager 规则和所有旧 `MATCH` 被清理，最终只有一个末尾兜底。
- [ ] M2-AC3：局域网 IPv4/IPv6、白名单、CN domain/IP 和非 CN 目标分别命中预期目标。
- [ ] M2-AC4：DNS golden 通过当前 mihomo 原生验证，国内/境外/代理服务器解析链路无递归依赖。
- [ ] M2-AC5：订阅更新前后 mode、规则、DNS、白名单和仍存在的组选择相同；节点消失产生 warning 与确定性回退。
- [ ] M2-AC6：任一写入后验证失败恢复原配置、权限和 migration expected 摘要。
- [ ] M2-AC7：旧 `ApplyRouteCN()` 不再保留独立 `MATCH,GLOBAL` 行为，兼容入口调用同一 Rule policy。

## Out of Scope

- daemon mode API 与运行态事务；由 M3 完成。
- CLI/TUI 页面和实时连接。
- 任意规则可视化 CRUD。
