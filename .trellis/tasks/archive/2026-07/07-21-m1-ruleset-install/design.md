# M1 技术设计：规则资产与 daemon 安装

## 1. 文件与常量

- `scripts/install.sh`：规则集下载、状态写入、daemon enable/start 和新建配置 migrate 编排。
- `scripts/tests/test_install.sh`：本地 fixture、curl/systemctl/mm 替身和失败矩阵。
- `scripts/uninstall.sh`：识别新增状态字段，但默认随 CONFIG_DIR 保留资产。
- `internal/config/config.go`：Go 侧暴露与 `CONFIG_DIR/rulesets` 一致的路径，供 M2 使用。
- `.trellis/spec/backend/deployment.md`、根 `AGENTS.md`：落地后记录新环境变量、路径和验证命令。

固定资产采用父设计中的 commit/SHA。`MM_RULESET_BASE_URL` 默认 `https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat`，`MM_RULESET_REF` 默认固定 commit；若任一引用被覆盖，要求同时设置 `MM_RULESET_DOMAIN_SHA256` 与 `MM_RULESET_IP_SHA256`，格式必须为 64 位十六进制。

## 2. 安装顺序

```text
preflight/dependencies
  -> build temporary mm
  -> install/verify mihomo core
  -> download+verify rulesets to same-dir temp files
  -> configure existing/new config (record CONFIG_CREATED)
  -> atomically publish rulesets and mm
  -> configure PATH
  -> mm daemon enable
  -> daemon enabled: mm daemon start
  -> CONFIG_CREATED && daemon running: mm migrate apply
  -> write install-state last
```

两份规则集先全部下载、校验，再逐一发布；发布第二份失败时恢复第一份的旧内容，避免混合版本。升级下载失败且旧文件存在时，重新校验旧文件：只有摘要/格式仍满足已记录状态才可降级继续。

## 3. daemon 与迁移

安装器通过正式 `mm daemon enable --output json` 的结果判断是否 enabled，不自行复制 unit 或解析本地化文本。enabled 后调用 `daemon start`，再用 `daemon status` 确认。

升级先查询 daemon/core 状态：

- daemon 不存在：正常 enable/start。
- daemon 存在且 core stopped：允许重启/启动以加载新 mm。
- daemon 存在且 core running：不停止或重启，输出新 binary 将在后续 daemon 重启生效。

只有本次 `configure_mihomo` 新建配置时自动 `migrate apply`；已有配置无论是否有效均不 apply。无 systemd 用户会话时 enable 的 `enabled=false` 是受支持降级，不执行 start/migrate。

## 4. 安全与恢复

- 所有路径必须位于解析后的 `CONFIG_DIR`，拒绝符号链接、非普通目标和危险目录。
- 临时文件与目标同目录，权限先设 `0600`，写入/校验完成后 `mv`。
- 日志和 install-state 不记录代理凭据；规则 URL、commit 和摘要不是秘密。
- 安装中断不删除旧规则缓存、不回滚 apt；mm/core/config 原有失败保护保持不变。

## 5. 测试

扩展安装测试 fixture，覆盖固定成功、摘要错误、第一/第二文件失败、旧缓存保留、自定义引用缺摘要拒绝、systemd enabled/不可用、fresh migrate、existing no-migrate、running core no-restart、重复安装幂等和状态 round-trip。
