# 修复 legacy 迁移重复备份文件

## Goal

修复 legacy 迁移发现阶段重复收集 `config.yaml.bak`，导致 `mm migrate apply` 在创建迁移记录前失败的问题，使已有标准备份文件的用户可以正常完成迁移。

## Background

- `discoverFiles` 的固定允许列表已经包含 `config.yaml.bak`。
- 动态发现 `config.yaml.*.bak` 时，`config.yaml.bak` 本身再次匹配并被追加，因此 `mm migrate plan` 输出两个同名条目。
- 重复条目进入 `LegacyMigration.Files` 后被领域校验拒绝，实际错误为 `duplicate legacy file "config.yaml.bak"`。
- 失败发生在迁移记录写入前；当前数据库没有 legacy 迁移记录，恢复点目录也没有残留。
- daemon 将该未分类错误统一返回为“迁移操作失败”，终端无法看到具体原因。

## Requirements

- 文件发现结果中的相对路径必须唯一且顺序稳定。
- 去重应作为整个发现结果的通用不变量实现，不采用仅排除 `config.yaml.bak` 的文件名特判。
- `config.yaml.bak` 只能出现一次。
- `config.yaml.<标识>.bak` 等额外历史备份仍需被发现并纳入恢复点。
- 修复不得依赖用户移动、删除或改名已有配置文件。
- 为包含 `config.yaml.bak` 和时间戳备份的真实目录形态补充回归测试。
- 仓库验证通过后，重新安装本机 `~/.local/bin/mm` 并重启 `mm.service`，使修复进入当前 daemon 进程。
- 对真实 legacy 配置重新执行迁移，且不得启动当前已停止的 mihomo core。
- 如果真实迁移暴露另一个独立错误，保留失败记录和恢复点作为诊断证据并停止；未经再次确认不得自动执行 rollback。

## Acceptance Criteria

- [x] `mm migrate plan` 对每个相对路径只输出一次，同时仍展示标准备份和时间戳备份。
- [x] `mm migrate apply` 在存在 `config.yaml.bak` 时成功创建恢复点并注册活动 legacy 档案。
- [x] 重复路径不会进入领域模型或触发数据库唯一键冲突。
- [x] 相关单元测试通过，且 `go test ./...` 通过。
- [x] 本机升级后，`mm migrate plan` 不再输出重复路径。
- [x] 本机 `mm migrate apply` 成功，`mm migrate status` 可查询到成功恢复点与活动 legacy 档案，core 仍为 `stopped`。
- [x] 真实迁移未出现其他错误，未执行 rollback。

## Out of Scope

- 修改或删除用户现有 legacy 配置文件。
- 改变迁移恢复点、摘要校验及回滚语义。
- 改造 daemon 对未分类迁移错误的通用日志、IPC 错误详情或用户提示。
