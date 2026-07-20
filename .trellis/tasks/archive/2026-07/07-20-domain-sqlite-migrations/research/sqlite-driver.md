# SQLite 驱动选型记录

调研日期：2026-07-20。

## 结论

选择 `modernc.org/sqlite v1.36.1` 作为 A2 的固定候选版本，并在实施第一步完成项目级构建与体积终验。

选择依据：

- 驱动基于 `database/sql`，上游明确描述为 CGo-free SQLite port。
- v1.36.1 的 `go.mod` 声明 `go 1.21`，可满足项目 Go 1.22 最低版本；v1.36.2 已提升到 Go 1.23，因此 v1.36.1 是已核对版本中最新的 Go 1.22 兼容版本。
- 上游 v1.36.1 列出 Linux amd64、arm64 支持，内含 SQLite 3.49.0 的对应生成实现。
- 驱动公开 SQLite online backup API，可经 `database/sql.Conn.Raw` 创建一致性迁移恢复点。
- 驱动主许可证为 BSD-3-Clause；实际探针构建闭包只包含 MIT/BSD-3-Clause 依赖，与项目 MIT 二进制分发兼容。A8 仍需生成正式第三方许可证清单并携带声明。

## 版本证据

| 版本 | 模块 Go 版本 | 结论 |
|---|---:|---|
| v1.35.0 | 1.21 | 兼容 |
| v1.36.0 | 1.21 | 兼容 |
| v1.36.1 | 1.21 | 选择 |
| v1.36.2 | 1.23.0 | 不满足项目 Go 1.22 |
| v1.39.1 | 1.24.0 | 不满足 |
| v1.46.2 及后续抽查 | 1.25.0 | 不满足 |

v1.36.1 发布时间为 2025-03-12，上游 v1.36.0 变更记录说明 SQLite 升级到 3.49.0。

## 无 CGO 与架构探针

使用上游 `examples/example1`，在项目最低 Go 版本上执行：

```bash
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -o /tmp/mm-sqlite-probe-amd64 .
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -o /tmp/mm-sqlite-probe-arm64 .
```

两种构建均通过，`file` 显示为静态链接 ELF：

| 探针 | 字节数 |
|---|---:|
| SQLite amd64 | 8,968,706 |
| SQLite arm64 | 8,626,435 |
| 当前 mm amd64 基线 | 10,394,095 |
| 当前 mm arm64 基线 | 10,059,819 |

探针大小不能与当前 mm 简单相加。A2 集成后必须用相同 Go 1.22.12、`CGO_ENABLED=0`、`-trimpath` 参数重建成品并记录准确增量。用户确认 amd64 未压缩成品上限为 25 MiB。

实施终验使用临时空导入把 `internal/store` 链入现有 `cmd/mm`，测量后移除空导入，避免提前接入运行路径：

| A2 最终链接探针 | 字节数 | 相对基线 |
|---|---:|---:|
| Linux amd64 | 15,954,302 | +5,560,207 |
| Linux arm64 | 15,235,376 | +5,175,557 |

两种产物均为静态链接 ELF；amd64 低于 25 MiB（26,214,400 字节）上限。项目最终 `mm` 在 A3 装配 store 前不会包含未引用包，这是 Go 链接器的正常裁剪行为。

## 实际构建闭包许可证

通过 Go 1.22.12 对探针运行 `go list -deps`，实际进入构建闭包的第三方模块如下：

| 模块 | 版本 | 许可证 |
|---|---|---|
| `modernc.org/sqlite` | v1.36.1 | BSD-3-Clause |
| `modernc.org/libc` | v1.61.13 | BSD-3-Clause |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause |
| `modernc.org/memory` | v1.8.2 | BSD-3-Clause，多份来源声明 |
| `github.com/dustin/go-humanize` | v1.0.1 | MIT |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/remyoudompheng/bigfft` | 指定伪版本 | BSD-3-Clause |
| `golang.org/x/exp` | 指定伪版本 | BSD-3-Clause |
| `golang.org/x/sys` | v0.30.0 | BSD-3-Clause |

模块 `go.mod` 中出现但未进入该探针构建闭包的依赖不能据此永久排除；项目集成后应再次按最终二进制依赖图审计。

## 采用限制

- 必须同时固定 `modernc.org/sqlite` 解析出的 `modernc.org/libc` 版本，不能单独升级 libc；上游明确警告两者版本需匹配。
- store 使用单个打开连接并在连接上设置 PRAGMA，避免连接池切换导致外键或超时配置漂移。
- A2 默认使用 rollback journal。若后续改用 WAL，必须重新审计 `-wal`、`-shm` 权限、备份一致性和崩溃恢复。
- 迁移备份使用上游 online backup API，但通过 store 内部小接口隔离驱动专属类型；领域与应用层不感知该 API。

## 停止条件

出现任一条件时停止业务 schema 实现并重新选型：

- 项目本身无法在 Go 1.22.12、Linux amd64/arm64、`CGO_ENABLED=0` 构建。
- 最终 amd64 未压缩 `mm` 超过 25 MiB。
- 最终构建闭包出现与 MIT 二进制分发冲突或无法确认的许可证。
- online backup、事务 DDL 或外键行为不能通过临时数据库验证。
