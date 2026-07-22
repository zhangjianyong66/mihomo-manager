# 父任务最终验收记录

验收日期：2026-07-22

## Milestone 与父 AC 映射

| 父 AC | 主要证据 |
|---|---|
| AC1-AC2、AC9-AC10 | M3 模式事务、M4 CLI/TUI、真实 running core 三模式与连接关闭验收 |
| AC3-AC5、AC8 | M2 Rule/DNS/订阅 overlay 测试、mihomo v1.19.28 原生验证、真实 CN domain/IP 与 MATCH 链路 |
| AC6-AC7、AC13 | M1 安装器 26 项隔离测试、规则集摘要/权限与当前 daemon 状态 |
| AC11-AC12 | M5 snapshot/follow/TUI/入口诊断测试与真实 GNOME/env mismatch 输出 |
| AC14 | 本记录中的全量质量门 |

M1-M5 的 child PRD 验收项均已完成并归档，未发现未解决开放问题。

## 自动化质量门

以下命令全部通过：

```bash
go test ./...
GOTOOLCHAIN=go1.22.12 go test -race ./...
go vet ./...
bash scripts/tests/test_install.sh
bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/mm-routing-amd64 ./cmd/mm
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/mm-routing-arm64 ./cmd/mm
MIHOMO_NATIVE_TEST=1 go test ./internal/mihomo -run '^TestRoutingPolicy_MihomoNativeValidation$' -count=1 -v
```

安装器测试使用临时 HOME、fixture 和命令替身，覆盖规则集事务、daemon fresh/existing/no-systemd 分支与默认卸载保留，共 26 项。Go 集成测试覆盖临时 Unix socket、迁移、模式事务、订阅 overlay、连接 snapshot/follow 和 TUI capability，不访问真实 HOME 或 systemd。

## 真实运行态链路

使用已安装 mihomo v1.19.28 和显式 `127.0.0.1:7890` 测试入口，切换时默认不关闭连接：

| 模式/目标 | mihomo 返回链路 | 结果 |
|---|---|---|
| Global / `speed.cloudflare.com` | `<当前 GLOBAL 节点> -> GLOBAL` | 配置与 runtime 均为 `global` |
| Direct / 本机临时 HTTP fixture | `DIRECT` | 配置与 runtime 均为 `direct` |
| Rule / `speed.cloudflare.com` | `<当前代理组节点> -> 🌐 代理`，rule=`Match` | 非 CN 进入代理组 |
| Rule / `www.baidu.com` | `DIRECT`，rule=`RuleSet:mm-cn-domain` | CN domain 直连 |
| Rule / `223.109.82.212` | `DIRECT`，rule=`RuleSet:mm-cn-ip` | CN IPv4 直连 |

显式 `mm mode set rule --close-connections` 前测试连接数为 1，返回 `connectionsClosed=true` 后为 0。自动化测试另覆盖 IPv6 局域网/CN provider、失败恢复、连接关闭 partial-success 和 stopped core 的“下次启动生效”。

## 恢复与非侵入检查

- 最终配置与 runtime 均恢复为 `rule`，core 为 `running`，活动连接为 0。
- 配置 SHA-256 恢复为 `b31003f69ee4111772d78acc945fcc4632c27b6195171c6a3cb37b6fcd8509b1`，权限为 `0600`。
- CN 规则集摘要分别为 `52c146262ef51dc23a84533a0d13f8addd031c61708a863d17cdb75cc3089ee4` 和 `206ad4cc22005976e8bfb50a869e5483cb81cc174a56c9a79c8a13e3e64e2eea`，目录/文件权限为 `0700/0600`。
- GNOME HTTP/HTTPS/SOCKS 与当前 CLI 代理环境仍指向 `127.0.0.1:10808`，10808 监听保持存在；测试未修改系统代理、未停止或重配置 xray。
