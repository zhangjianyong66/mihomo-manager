# 开发与验证命令

## 环境基线

- 模块名：`github.com/zhangjianyong66/mihomo-manager`。
- `go.mod` 声明 Go `1.22`。
- 运行管理器需要单独安装 mihomo core；默认位置为 `~/.local/bin/mihomo`。

## 常用命令

| 目的 | 命令 | 说明 |
|------|------|------|
| 运行已安装程序 | `mm` | 打开交互式 TUI，需要终端环境 |
| 非交互冒烟 | `mm --help` | 不进入 TUI，适合安装后检查 |
| 从源码查看帮助 | `go run ./cmd/mm --help` | 验证当前源码入口 |
| 构建 | `go build -o /tmp/mm ./cmd/mm` | 使用临时输出，避免无意覆盖仓库中的 `bin/mm` |
| 格式检查 | `test -z "$(gofmt -l cmd internal)"` | 有输出表示存在未格式化的 Go 文件 |
| 单元测试 | `go test ./...` | 当前主要且必须执行的自动化测试 |
| 安装 | `make install` | 安装依赖、校验后的 mihomo core 和独立 `~/.local/bin/mm` |
| 卸载 | `make uninstall` | 删除 mm/隔离 Go/PATH 块，保留 core 和用户配置 |
| 安装测试 | `bash scripts/tests/test_install.sh` | 临时 HOME 驱动，不触碰真实配置、apt 或运行态 |
| mihomo 配置检查 | `"${MIHOMO_BIN:-$HOME/.local/bin/mihomo}" -t -f "${CONFIG_DIR:-$HOME/.config/mihomo}/config.yaml"` | 修改生成配置或路由规则后执行 |

## 按修改范围验证

- 修改 `internal/config`、`internal/mihomo`、`internal/tui` 或 `cmd/mm`：运行 `gofmt` 检查、`go test ./...`，并用 `go run ./cmd/mm --help` 做入口冒烟。
- 修改进程启动逻辑：除全量测试外，确保 `internal/mihomo/client_start_test.go` 仍验证 `Setsid: true`。
- 修改订阅、白名单或路由：优先在 `internal/mihomo/client_route_test.go` 添加临时目录或 `httptest.Server` 驱动的回归测试；涉及最终 YAML 时再执行 mihomo 配置检查。
- 修改安装 Shell：执行 `bash scripts/tests/test_install.sh` 和 `bash -n scripts/*.sh scripts/lib/*.sh scripts/tests/*.sh tests/*.sh`，且不能以 Shell 测试替代 Go 测试。

## 测试边界

- `go test ./...` 是当前 Go 产品代码的权威测试入口。
- `tests/test.sh` 依赖本机端口、launchd 和已运行服务，还调用已不再支持的 `mm status`，不能作为通用 CI 或当前 CLI 的必过测试。
- `scripts/tests/test_manager.sh` 主要验证遗留 `bin/mihomo-manager` 和 `scripts/lib`，仅在维护旧 Shell 实现时运行。
- 单元测试不得依赖真实的 `~/.config/mihomo`、真实订阅或正在运行的 mihomo；使用 `t.TempDir()`、`httptest.NewServer()` 和 `/usr/bin/true`/`false` 等可控替身。
