#!/bin/bash
#
# mihomo-manager 白名单管理库
# 管理直连域名（不走代理）
#

# 加载公共库
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

# ============================================
# 白名单管理
# ============================================

# 添加域名到白名单
mm_whitelist_add() {
    local domain="$1"
    
    if [[ -z "$domain" ]]; then
        mm_error "请指定要添加的域名"
        return 1
    fi
    
    # 清理域名（移除可能的协议前缀）
    domain=$(echo "$domain" | sed -e 's|^https://||' -e 's|^http://||' -e 's|/.*$||')
    
    mm_info "添加域名到白名单: $domain"
    
    if [[ ! -f "$CONFIG_FILE" ]]; then
        mm_error "配置文件不存在: $CONFIG_FILE"
        return 1
    fi
    
    # 检查域名是否已存在
    if grep -q "DOMAIN-SUFFIX,$domain,🎯 Direct" "$CONFIG_FILE" || \
       grep -q "DOMAIN,$domain,🎯 Direct" "$CONFIG_FILE"; then
        mm_warn "域名 $domain 已在白名单中"
        return 0
    fi
    
    mm_backup_config
    
    # 使用 Python 修改 YAML 文件
    python3 << EOF
import re

with open('$CONFIG_FILE', 'r') as f:
    content = f.read()

marker = '$WHITELIST_MARKER'
end_marker = '$WHITELIST_END'
new_rule = f'  - DOMAIN-SUFFIX,$domain,🎯 Direct    # 白名单: 直连'

if marker in content:
    content = content.replace(marker, marker + '\n' + new_rule)
else:
    content = content.replace('rules:', f'''rules:
{marker}
{new_rule}
{end_marker}''', 1)

with open('$CONFIG_FILE', 'w') as f:
    f.write(content)
EOF
    
    mm_success "域名 $domain 已添加到白名单"
    mm_info "建议执行: mm reload 使配置生效"
}

# 从白名单移除域名
mm_whitelist_remove() {
    local domain="$1"
    
    if [[ -z "$domain" ]]; then
        mm_error "请指定要移除的域名"
        return 1
    fi
    
    # 清理域名
    domain=$(echo "$domain" | sed -e 's|^https://||' -e 's|^http://||' -e 's|/.*$||')
    
    mm_info "从白名单移除域名: $domain"
    
    if [[ ! -f "$CONFIG_FILE" ]]; then
        mm_error "配置文件不存在"
        return 1
    fi
    
    # 检查是否存在
    if ! grep -q "DOMAIN.*$domain.*🎯 Direct" "$CONFIG_FILE"; then
        mm_warn "域名 $domain 不在白名单中"
        return 0
    fi
    
    mm_backup_config
    
    # 删除该域名的白名单规则
    sed -i.tmp "/DOMAIN.*$domain.*🎯 Direct/d" "$CONFIG_FILE"
    rm -f "$CONFIG_FILE.tmp"
    
    mm_success "域名 $domain 已从白名单移除"
    mm_info "建议执行: mm reload 使配置生效"
}

# 列出白名单
mm_whitelist_list() {
    mm_info "当前白名单（直连域名）:"
    echo ""
    
    if [[ ! -f "$CONFIG_FILE" ]]; then
        mm_error "配置文件不存在"
        return 1
    fi
    
    local found=false
    local count=0
    
    while IFS= read -r line; do
        if echo "$line" | grep -q "🎯 Direct"; then
            local domain
            domain=$(echo "$line" | grep -o "DOMAIN[^,]*,[^,]*" | cut -d',' -f2)
            if [[ -n "$domain" ]]; then
                count=$((count + 1))
                found=true
                echo "  $count. $domain"
            fi
        fi
    done < "$CONFIG_FILE"
    
    if [[ "$found" == false ]]; then
        mm_warn "白名单为空"
        echo ""
        mm_info "使用以下命令添加:"
        mm_info "  mm whitelist add <域名>"
    else
        echo ""
        mm_info "共 $count 个域名"
    fi
}

# 白名单帮助
mm_whitelist_help() {
    echo "白名单管理 - 配置直连域名（不走代理）"
    echo ""
    echo "用法: mm whitelist <子命令> [参数]"
    echo "      mm wl <子命令> [参数]"
    echo ""
    echo "子命令:"
    echo "  add <域名>    添加域名到白名单"
    echo "  remove <域名> 从白名单移除域名"
    echo "  list          列出所有白名单域名"
    echo "  help          显示帮助"
    echo ""
    echo "示例:"
    echo "  mm whitelist add example.com     # 添加域名"
    echo "  mm wl add github.com             # 简写形式"
    echo "  mm whitelist remove example.com  # 移除域名"
    echo "  mm whitelist list                # 查看列表"
}

# 白名单命令处理
mm_whitelist_handler() {
    local subcmd="$1"
    local arg="$2"
    
    case "$subcmd" in
        add|a)
            mm_whitelist_add "$arg"
            ;;
        remove|rm|r)
            mm_whitelist_remove "$arg"
            ;;
        list|ls|l)
            mm_whitelist_list
            ;;
        help|h|--help)
            mm_whitelist_help
            ;;
        "")
            mm_whitelist_list
            ;;
        *)
            # 直接把子命令当域名，尝试添加
            mm_whitelist_add "$subcmd"
            ;;
    esac
}
