#!/bin/bash
#
# mihomo-manager 分流管理库
# 配置中国大陆直连、其他流量走代理
#

# 加载公共库
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

readonly ROUTING_MARKER="# ===== 分流规则（中国大陆直连）====="
readonly ROUTING_END="# ===== 分流规则结束 ====="
readonly GEOIP_URL="https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat"
readonly GEOSITE_URL="https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat"
readonly MMDB_URL="https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country.mmdb"

# 应用中国大陆直连规则
mm_route_cn_apply() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        mm_error "配置文件不存在: $CONFIG_FILE"
        return 1
    fi

    mm_info "应用分流规则：大陆直连，其他走 GLOBAL..."
    mm_backup_config || return 1

    if ! python3 << EOF
import re
import sys

config_file = r"$CONFIG_FILE"
marker = r"$ROUTING_MARKER"
end_marker = r"$ROUTING_END"
geoip_url = r"$GEOIP_URL"
geosite_url = r"$GEOSITE_URL"
mmdb_url = r"$MMDB_URL"

with open(config_file, "r", encoding="utf-8") as f:
    text = f.read()

def ensure_scalar(src: str, key: str, value: str) -> str:
    pattern = rf'(?m)^{re.escape(key)}:\\s*.*$'
    repl = f"{key}: {value}"
    if re.search(pattern, src):
        return re.sub(pattern, repl, src, count=1)
    return repl + "\\n" + src

def upsert_geox(src: str) -> str:
    block = (
        "geox-url:\\n"
        f"  geoip: \\"{geoip_url}\\"\\n"
        f"  geosite: \\"{geosite_url}\\"\\n"
        f"  mmdb: \\"{mmdb_url}\\""
    )
    pattern = r'(?ms)^geox-url:\\n(?:^[ \\t].*\\n?)*'
    if re.search(pattern, src):
        return re.sub(pattern, block + "\\n", src, count=1)
    return block + "\\n" + src

def split_rules_section(src: str):
    lines = src.splitlines(keepends=True)
    rules_idx = None
    for i, line in enumerate(lines):
        if re.match(r'^rules:\\s*$', line.strip()):
            rules_idx = i
            break
    if rules_idx is None:
        return lines, None, None

    end_idx = len(lines)
    for i in range(rules_idx + 1, len(lines)):
        raw = lines[i]
        if raw.startswith(" ") or raw.startswith("\t") or raw.strip() == "":
            continue
        end_idx = i
        break
    return lines, rules_idx, end_idx

text = ensure_scalar(text, "geodata-mode", "true")
text = ensure_scalar(text, "geo-auto-update", "true")
text = ensure_scalar(text, "geo-update-interval", "24")
text = upsert_geox(text)

lines, rules_idx, rules_end = split_rules_section(text)
managed_match = f"  - MATCH,GLOBAL\\n"

if rules_idx is None:
    rules_block = [
        "rules:\\n",
        f"  {marker}\\n",
        "  - GEOSITE,CN,DIRECT\\n",
        "  - GEOIP,CN,DIRECT,no-resolve\\n",
        managed_match,
        f"  {end_marker}\\n",
    ]
    if len(lines) > 0 and not lines[-1].endswith("\\n"):
        lines[-1] = lines[-1] + "\\n"
    lines.extend(["\\n"] if len(lines) > 0 and lines[-1].strip() != "" else [])
    lines.extend(rules_block)
else:
    section = lines[rules_idx + 1:rules_end]
    # 清理旧的受管块和重复的 CN 规则，再重新插入
    cleaned = []
    in_managed = False
    for line in section:
        if marker in line:
            in_managed = True
            continue
        if end_marker in line:
            in_managed = False
            continue
        if in_managed:
            continue
        if re.match(r'^\\s*-\\s*GEOSITE,CN,DIRECT\\s*$', line):
            continue
        if re.match(r'^\\s*-\\s*GEOIP,CN,DIRECT,no-resolve\\s*$', line):
            continue
        cleaned.append(line)
    existing_match = any(re.match(r'^\\s*-\\s*MATCH,', line) for line in cleaned)

    managed = [
        f"  {marker}\\n",
        "  - GEOSITE,CN,DIRECT\\n",
        "  - GEOIP,CN,DIRECT,no-resolve\\n",
    ]
    if not existing_match:
        managed.append(managed_match)
    managed.append(f"  {end_marker}\\n")

    lines = lines[:rules_idx + 1] + managed + cleaned + lines[rules_end:]

with open(config_file, "w", encoding="utf-8") as f:
    f.write("".join(lines))
EOF
    then
        mm_error "更新分流规则失败"
        cp "$CONFIG_BACKUP" "$CONFIG_FILE" 2>/dev/null || true
        return 1
    fi

    if ! mm_test_config; then
        mm_error "配置测试失败，正在回滚..."
        cp "$CONFIG_BACKUP" "$CONFIG_FILE" 2>/dev/null || true
        return 1
    fi

    mm_success "分流规则已写入配置"
    mm_reload_config
}
