#!/bin/bash
#
# mihomo-manager 订阅管理库
# 负责订阅下载和更新
#

# 加载公共库
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

# ============================================
# 订阅下载
# ============================================

# 下载订阅配置
mm_download_subscription() {
    local url="$1"
    local use_proxy="${2:-false}"
    
    if [[ -z "$url" ]]; then
        mm_error "请提供订阅链接"
        return 1
    fi
    
    mm_info "下载订阅配置..."
    
    local temp_file
    temp_file=$(mm_temp_file sub)
    local download_success=false
    
    # 根据是否使用代理选择下载方式
    if [[ "$use_proxy" == "true" ]]; then
        mm_info "使用代理下载 (http://127.0.0.1:$MIXED_PORT)..."
        
        if mm_has_command curl; then
            if curl -fsSL -x "http://127.0.0.1:$MIXED_PORT" "$url" -o "$temp_file" 2>/dev/null; then
                download_success=true
            fi
        elif mm_has_command wget; then
            if wget -q -e "use_proxy=yes" -e "http_proxy=http://127.0.0.1:$MIXED_PORT" "$url" -O "$temp_file" 2>/dev/null; then
                download_success=true
            fi
        fi
        
        if [[ "$download_success" != "true" ]]; then
            mm_error "使用代理下载失败"
            return 1
        fi
    else
        mm_info "直接下载（不走代理）..."
        
        # 临时取消代理环境变量
        local old_http_proxy="$http_proxy"
        local old_https_proxy="$https_proxy"
        local old_HTTP_PROXY="$HTTP_PROXY"
        local old_HTTPS_PROXY="$HTTPS_PROXY"
        unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY
        
        if mm_has_command curl; then
            if curl -fsSL --proxy "" "$url" -o "$temp_file" 2>/dev/null; then
                download_success=true
            fi
        elif mm_has_command wget; then
            if wget -q --no-proxy "$url" -O "$temp_file" 2>/dev/null; then
                download_success=true
            fi
        else
            mm_error "未找到 curl 或 wget"
            export http_proxy="$old_http_proxy"
            export https_proxy="$old_https_proxy"
            export HTTP_PROXY="$old_HTTP_PROXY"
            export HTTPS_PROXY="$old_HTTPS_PROXY"
            return 1
        fi
        
        # 恢复代理环境变量
        export http_proxy="$old_http_proxy"
        export https_proxy="$old_https_proxy"
        export HTTP_PROXY="$old_HTTP_PROXY"
        export HTTPS_PROXY="$old_HTTPS_PROXY"
        
        if [[ "$download_success" != "true" ]]; then
            mm_error "下载失败"
            mm_info "如果订阅需要代理访问，请使用 --proxy 选项"
            return 1
        fi
    fi
    
    # 验证下载的文件
    if [[ ! -s "$temp_file" ]]; then
        mm_error "下载的文件为空"
        return 1
    fi
    
    # 检测订阅格式
    local first_line
    first_line=$(head -1 "$temp_file")
    
    if echo "$first_line" | grep -q "^vless://\|^vmess://\|^ss://\|^trojan://\|^hysteria://\|^hysteria2://"; then
        mm_info "检测到节点订阅格式，正在转换为完整配置..."
        
        # 使用 Python 模块解析节点
        if ! python3 "$SCRIPT_DIR/proxy.py" "$temp_file" -c "$CONFIG_FILE" -o "$CONFIG_FILE" 2>/dev/null; then
            mm_error "节点解析失败"
            return 1
        fi
        
        mm_success "订阅节点已合并到配置文件"
        mm_info "建议执行: mm reload 或 mm restart"
    else
        mm_info "检测到完整 YAML 配置格式..."
        
        # 测试配置文件
        mm_info "验证下载的配置..."
        if ! "$MIHOMO_BIN" -t -f "$temp_file" >/dev/null 2>&1; then
            mm_error "下载的配置文件无效"
            mm_warn "可能是订阅链接已过期或格式不正确"
            return 1
        fi
        
        mm_backup_config
        
        # 复制新配置
        cp "$temp_file" "$CONFIG_FILE"
        
        # 合并配置：保留本地端口设置
        mm_info "合并端口配置..."
        
        # 将 port: 7890 替换为 mixed-port: 10808
        if grep -q "^port:" "$CONFIG_FILE"; then
            sed -i.bak '/^port:/d' "$CONFIG_FILE"
        fi
        
        # 确保 mixed-port 存在
        if ! grep -q "^mixed-port:" "$CONFIG_FILE"; then
            sed -i.bak "s/^allow-lan: false/allow-lan: false\nmixed-port: 10808/" "$CONFIG_FILE"
        fi
        
        # 修复 external-controller
        if grep -q "^external-controller: 0.0.0.0" "$CONFIG_FILE"; then
            sed -i.bak 's/^external-controller: 0.0.0.0:.*/external-controller: 127.0.0.1:9090/' "$CONFIG_FILE"
        fi
        
        rm -f "$CONFIG_FILE.bak"
        chmod 600 "$CONFIG_FILE"
        
        mm_success "订阅配置已更新"
        mm_info "建议执行: mm reload 或 mm restart"
    fi
}

# 显示订阅 URL
mm_show_subscription_url() {
    if [[ ! -f "$SUBSCRIPTION_URL_FILE" ]]; then
        mm_warn "未找到保存的订阅 URL"
        mm_info "使用以下命令保存:"
        mm_info "  mm save-url 'https://xxx.com/subscribe?token=xxx'"
        return 1
    fi
    
    local url
    url=$(cat "$SUBSCRIPTION_URL_FILE")
    
    if [[ -z "$url" ]]; then
        mm_warn "保存的订阅 URL 为空"
        return 1
    fi
    
    mm_info "已保存的订阅 URL:"
    echo "  $url"
}

# 从保存的订阅 URL 更新
mm_update_from_saved_url() {
    mm_info "从保存的订阅 URL 更新配置..."
    
    if [[ ! -f "$SUBSCRIPTION_URL_FILE" ]]; then
        mm_error "未找到保存的订阅 URL"
        mm_info "请先使用以下命令保存订阅 URL:"
        mm_info "  mm save-url 'https://xxx.com/subscribe?token=xxx'"
        return 1
    fi
    
    local url
    url=$(cat "$SUBSCRIPTION_URL_FILE")
    
    if [[ -z "$url" ]]; then
        mm_error "保存的订阅 URL 为空"
        return 1
    fi
    
    mm_info "使用订阅 URL:"
    echo "  $url"
    echo ""
    
    mm_download_subscription "$url" "false"
}
