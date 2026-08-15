# 实施计划

## 1. 路由数据与合成器

- [x] 在 `internal/mihomo` 定义受管分流规则、类别、规范化与冲突检测；解析 IPv4/IPv6/IP/CIDR，保留现有域名规范化。
- [x] 演进 `whitelist.yaml` 的读取/写入，兼容仅含旧 `domains` 字段的文件，确保 `0600` 原子写入与失败恢复。
- [x] 扩展 `RoutingPolicy`、owned-rule 识别和规则剥离/渲染；在自定义规则之后按具体性插入直连/代理规则。
- [x] 将白名单 client 方法保留为 direct 兼容包装，并增加通用规则 CRUD。
- [x] 为以上路径先添加临时目录测试，包括 optional `MIHOMO_NATIVE_TEST=1` 原生校验。

## 2. Daemon 与应用契约

- [x] 扩展 app route service、capability API、daemon capability 与 legacy compatibility 的 typed route-rule DTO 和方法。
- [x] 新增 `/v1/routes/rules` 的 GET/POST/PUT/DELETE handler，接入 request ID、Coordinator、profile 解析和标准 error envelope。
- [x] 保留 `/v1/routes/whitelist` 行为，内部映射至 direct 规则。
- [x] 覆盖 IPC、旧 API 兼容、冲突和写入恢复测试。

## 3. CLI

- [x] 增加 `mm route direct|proxy list|add|remove|edit`，保留 `route whitelist`。
- [x] 为写命令增加 `--restart`；保存成功后才执行受管 Core restart，输出区分保存/重启结果。
- [x] 补足 table/JSON、profile、非法输入、daemon 错误和 `--restart` 测试。

## 4. TUI

- [x] 用“域名分流”替换菜单文案，重构白名单局部状态为类别化规则列表，复用现有 Bubble Tea message/command 模式。
- [x] 增加 running Core 的重启确认、取消后的保存提示和 stopped Core 的无确认提示；不在 `Update`/`View` 直接调用 capability。
- [x] 在 mode 状态增加非 rule 模式的分流无效 warning。
- [x] 覆盖导航、CRUD、确认/取消/重启、错误和窄终端测试。

## 5. 质量与收尾

- [x] `gofmt -w` 改动的 Go 文件，并确认 `gofmt -l` 无输出。
- [x] 执行 `go test ./...`、`go vet ./...`、`CGO_ENABLED=0 go build -trimpath -o /tmp/mm ./cmd/mm`。
- [x] 执行 `MIHOMO_NATIVE_TEST=1 go test ./internal/mihomo`（本机 core 和规则集可用时）。
- [ ] 按实际落地更新相关 Trellis backend spec；执行 `trellis-check`；检查 git diff 后以中文 Conventional Commit 提交。

## 风险与回滚点

- 高风险点是 owned-rule 识别：错误识别会删除用户手写规则。因此先完成纯函数测试，再接入写路径。
- 规则文件结构变更必须保持旧 `domains` 可读；出现兼容问题时可回滚到仅 direct 写入，同时保留配置已验证的备份恢复机制。
- Core restart 失败不回滚规则，用户可通过 `mm core restart` 重试；此行为在 CLI/TUI 输出中明确显示。
