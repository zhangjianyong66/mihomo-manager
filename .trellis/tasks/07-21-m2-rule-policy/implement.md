# M2 实施计划：Rule Policy 与订阅保留

## Task 1：RoutingPolicy 纯变换

- [x] 定义 mode、manager provider/rule 签名和路径常量的单一来源。
- [x] 实现 parse/clean/apply，保持 custom rules/providers 顺序并生成唯一兜底。
- [x] 添加 golden、重复规则、未知规则、IPv4/IPv6 局域网和白名单测试。

验证：`go test ./internal/mihomo -run 'Routing|Whitelist'`。

## Task 2：DNS policy 与原生校验

- [x] 用固定 mihomo v1.19.28 验证 `.mrs` provider、Rule 和 DNS 字段组合。
- [x] 实现 manager DNS ownership 与兼容字段保留，覆盖递归依赖和 IPv6。
- [x] 增加 golden、无效 resolver/provider、验证失败恢复测试。

验证：隔离临时目录执行 `mihomo -t -d <dir> -f <file>`；CI 使用受控替身验证参数和失败矩阵。

## Task 3：订阅、白名单与兼容预设接入

- [x] `UpdateSubscription` 提取/重放 routing overlay，只替换节点。
- [x] 白名单 mutation 与 `ApplyRouteCN` 兼容入口改用同一 policy。
- [x] 保存/恢复组选择并返回 typed warnings；静态 diagnose 对 RULE-SET 降低置信度。
- [x] 覆盖完整 YAML、URI 列表、节点消失、reload/selection 失败和回滚。

验证：`go test ./internal/mihomo ./internal/legacy ./internal/daemon`。

## Task 4：一致性与文档

- [x] 删除或收口旧的重复规则辅助函数，搜索所有 `MATCH,GLOBAL`、`MATCH,🌐 代理` 写入点。
- [x] 更新 project-conventions spec、根 `AGENTS.md` 和相关命令文案。
- [x] 运行 Go 全量、race、vet 和双架构无 CGO 构建。

## 回滚点

- M2 独立提交；保留旧配置备份与 compatibility 恢复测试。
- v1.19.28 不接受计划 DNS/provider 字段时停止，不修改用户配置，先修订 design/golden。
