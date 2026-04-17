#!/bin/bash
#
# mihomo-manager 服务管理库
# 负责启动、停止、重启、状态检查等
#

# 加载公共库
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

# ============================================
# 服务启动
# ============================================

# 启动服务
mm_start_service() {
    mm_clear_status_cache
    
    if mm_is_running; then
        local pid
        pid=$(mm_get_pid)
        mm_warn "服务已在运行中 (PID: $pid)"
        return 0
    fi
    
    mm_info "启动 Mihomo 服务..."
    
    cd "$CONFIG_DIR"
    nohup "$MIHOMO_BIN" -f "$CONFIG_FILE" >> "$LOG_FILE" 2>&1 &
    
    sleep 2
    
    mm_clear_status_cache
    if mm_is_running; then
        local pid
        pid=$(mm_get_pid)
        mm_success "服务启动成功 (PID: $pid)"
        return 0
    else
        mm_error "服务启动失败，请检查日志"
        return 1
    fi
}

# ============================================
# 服务停止
# ============================================

# 停止服务
mm_stop_service() {
    mm_clear_status_cache
    
    # 获取 mihomo 进程 PID（使用与 mm_check_status 相同的逻辑）
    local all_pids=""
    all_pids=$(pgrep -f "mihomo.*-f.*config.yaml" 2>/dev/null) || true
    if [[ -z "$all_pids" ]]; then
        # 尝试匹配简单的 mihomo 进程（并验证端口）
        local pid
        pid=$(pgrep -x "mihomo" 2>/dev/null | head -1)
        if [[ -n "$pid" ]]; then
            local port_pids
            port_pids=$(lsof -Pi :"$MIXED_PORT" -sTCP:LISTEN -t 2>/dev/null || true)
            if [[ "$port_pids" == *"$pid"* ]]; then
                all_pids="$pid"
            fi
        fi
    fi
    
    if [[ -z "$all_pids" ]]; then
        mm_warn "服务未运行"
        return 0
    fi
    
    mm_info "停止 Mihomo 服务..."
    
    # 尝试正常终止
    echo "$all_pids" | while read -r pid; do
        kill "$pid" 2>/dev/null || true
    done
    
    # 等待最多 5 秒
    for i in 1 2 3 4 5; do
        sleep 1
        local remaining
        remaining=$(pgrep -x "mihomo" 2>/dev/null)
        [[ -z "$remaining" ]] && break
    done
    
    # 强制终止剩余进程
    local remaining
    remaining=$(pgrep -x "mihomo" 2>/dev/null)
    if [[ -n "$remaining" ]]; then
        mm_warn "部分进程未响应，强制终止..."
        echo "$remaining" | while read -r pid; do
            kill -9 "$pid" 2>/dev/null || true
        done
        sleep 1
    fi
    
    mm_clear_status_cache
    mm_success "服务已停止"
}

# ============================================
# 服务重启
# ============================================

# 重启服务
mm_restart_service() {
    mm_info "重启 Mihomo 服务..."
    mm_stop_service
    sleep 2
    mm_start_service
}

# ============================================
# 热重载配置
# ============================================

# 热重载配置
mm_reload_config() {
    if ! mm_is_running; then
        mm_warn "服务未运行，直接启动..."
        mm_start_service
        return 0
    fi
    
    mm_info "测试配置文件..."
    if ! mm_test_config; then
        mm_error "配置文件测试失败"
        "$MIHOMO_BIN" -t -f "$CONFIG_FILE"
        return 1
    fi
    
    mm_success "配置文件测试通过"
    mm_info "发送热重载信号..."
    
    local pid
    pid=$(mm_get_pid)
    kill -HUP "$pid" 2>/dev/null || true
    
    mm_success "配置已热重载"
}

# ============================================
# 状态显示
# ============================================

# 显示服务状态
mm_show_status() {
    mm_info "检查 Mihomo 服务状态..."
    
    if mm_is_running; then
        local pid
        pid=$(mm_get_pid)
        mm_success "服务运行中 (PID: $pid)"
        
        # 检查端口
        if mm_check_port "$MIXED_PORT"; then
            mm_success "混合代理端口 $MIXED_PORT: 监听中"
        else
            mm_warn "混合代理端口 $MIXED_PORT: 未监听"
        fi
        
        if mm_check_port "$SOCKS_PORT"; then
            mm_success "SOCKS 端口 $SOCKS_PORT: 监听中"
        else
            mm_warn "SOCKS 端口 $SOCKS_PORT: 未监听"
        fi
        
        if mm_check_port "$API_PORT"; then
            mm_success "管理面板 $API_PORT: 监听中 (http://127.0.0.1:$API_PORT)"
        else
            mm_warn "管理面板 $API_PORT: 未监听"
        fi
    else
        mm_warn "服务未运行"
    fi
}

# ============================================
# 日志查看
# ============================================

# 查看日志
mm_show_logs() {
    if [[ ! -f "$LOG_FILE" ]]; then
        mm_error "未找到日志文件: $LOG_FILE"
        return 1
    fi
    
    mm_info "查看实时日志 (按 Ctrl+C 退出)..."
    echo ""
    echo "=== 最近 20 行日志 ==="
    tail -n 20 "$LOG_FILE"
    echo ""
    echo "=== 实时刷新中... ==="
    echo ""
    tail -n 0 -f "$LOG_FILE"
}

# ============================================
# 配置编辑
# ============================================

# 编辑配置
mm_edit_config() {
    local editor="${EDITOR:-vi}"
    
    mm_info "使用编辑器: $editor"
    mm_info "编辑配置文件: $CONFIG_FILE"
    
    $editor "$CONFIG_FILE"
    
    mm_info "编辑完成，建议执行: mm test"
}

# 恢复配置
mm_restore_config() {
    if [[ ! -f "$CONFIG_BACKUP" ]]; then
        mm_error "未找到备份文件: $CONFIG_BACKUP"
        return 1
    fi
    
    echo -n "这将覆盖当前配置，是否继续? (y/N) "
    read -r confirm
    
    if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
        mm_info "已取消"
        return 0
    fi
    
    mm_backup_config
    
    mm_info "恢复备份配置..."
    cp "$CONFIG_BACKUP" "$CONFIG_FILE"
    
    mm_success "配置已恢复"
    mm_info "建议执行: mm reload"
}

# ============================================
# 节点管理
# ============================================

# 列出节点
mm_list_nodes() {
    if ! mm_is_running; then
        mm_error "服务未运行"
        return 1
    fi
    
    local response
    response=$(curl -s "http://127.0.0.1:$API_PORT/proxies" 2>/dev/null)
    
    if [[ -z "$response" ]]; then
        mm_error "无法获取节点列表"
        return 1
    fi
    
    echo "$response" | python3 -c "
import json
import sys

try:
    data = json.load(sys.stdin)
    proxies = data.get('proxies', {})
    proxy = proxies.get('GLOBAL')
    
    if proxy and proxy.get('type') == 'Selector':
        name = 'GLOBAL'
        print(f'\n📁 代理组: {name}')
        print(f'   当前节点: {proxy.get(\"now\", \"N/A\")}')
        print(f'   可用节点: {len(proxy.get(\"all\", []))} 个')
        for i, node in enumerate(proxy.get('all', []), 1):
            marker = '✓' if node == proxy.get('now') else ' '
            print(f'     [{marker}] {i:2}. {node}')
    else:
        print('未找到 GLOBAL 代理组')
except Exception as e:
    print(f'解析错误: {e}', file=sys.stderr)
    sys.exit(1)
"
}

# 显示当前节点
mm_show_current_node() {
    if ! mm_is_running; then
        mm_error "服务未运行"
        return 1
    fi
    
    local response
    response=$(curl -s "http://127.0.0.1:$API_PORT/proxies/GLOBAL" 2>/dev/null)
    
    if [[ -z "$response" ]]; then
        return 1
    fi
    
    local current
    current=$(echo "$response" | python3 -c "import json,sys; print(json.load(sys.stdin).get('now','N/A'))" 2>/dev/null)
    echo "🌐 当前节点: $current"
}

# 切换节点
mm_switch_node() {
    local node_name="$1"
    
    if [[ -z "$node_name" ]]; then
        mm_error "请指定节点名称或序号"
        return 1
    fi
    
    if ! mm_is_running; then
        mm_error "服务未运行"
        return 1
    fi
    
    # 如果是纯数字，当作 GLOBAL 组的序号处理
    if [[ "$node_name" =~ ^[0-9]+$ ]]; then
        local index="$node_name"
        local response
        response=$(curl -s "http://127.0.0.1:$API_PORT/proxies/GLOBAL" 2>/dev/null)
        
        if [[ -z "$response" ]]; then
            mm_error "无法获取 GLOBAL 代理组信息"
            return 1
        fi
        
        local total
        total=$(echo "$response" | python3 -c "import json,sys; print(len(json.load(sys.stdin).get('all', [])))" 2>/dev/null)
        
        if [[ -z "$total" || "$total" -eq 0 ]]; then
            mm_error "无法获取节点列表"
            return 1
        fi
        
        if [[ "$index" -lt 1 || "$index" -gt "$total" ]]; then
            mm_error "序号 $index 超出范围 (1-$total)"
            return 1
        fi
        
        node_name=$(echo "$response" | python3 -c "import json,sys; data=json.load(sys.stdin); print(data.get('all', [])[$index-1])" 2>/dev/null)
        
        if [[ -z "$node_name" ]]; then
            mm_error "无法找到序号 $index 对应的节点"
            return 1
        fi
        
        mm_info "根据序号 $index 找到节点: $node_name"
    fi
    
    mm_info "切换节点到: $node_name"
    
    local response
    response=$(curl -s -X PUT "http://127.0.0.1:$API_PORT/proxies/GLOBAL" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"$node_name\"}" 2>/dev/null)
    
    if [[ $? -eq 0 ]]; then
        mm_success "节点切换成功"
        mm_show_current_node
    else
        mm_error "节点切换失败"
        return 1
    fi
}

# 测试节点延迟
mm_test_nodes() {
    local limit="${1:-0}"
    
    mm_info "获取节点列表..."
    
    if ! mm_is_running; then
        mm_error "服务未运行"
        return 1
    fi
    
    local proxies_response
    proxies_response=$(curl -s "http://127.0.0.1:$API_PORT/proxies" 2>/dev/null)
    
    if [[ -z "$proxies_response" ]]; then
        mm_error "无法获取代理信息"
        return 1
    fi
    
    echo "正在分析节点信息..."
    
    # 获取节点列表
    local nodes_list
    nodes_list=$(echo "$proxies_response" | python3 -c "
import json
import sys

try:
    data = json.load(sys.stdin)
    proxies = data.get('proxies', {})
    global_proxy = proxies.get('GLOBAL', {})
    all_nodes = global_proxy.get('all', [])
    
    group_types = ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Direct', 'Reject', 'RejectDrop', 'Pass', 'Compatible']
    
    for i, node_name in enumerate(all_nodes, 1):
        if node_name in ['DIRECT', 'REJECT']:
            continue
        if node_name.startswith('官网') or node_name.startswith('有效期'):
            continue
        
        node_info = proxies.get(node_name, {})
        node_type = node_info.get('type', '')
        
        if node_type in group_types:
            continue
        
        print(f'{i}:{node_name}')
except Exception as e:
    print(f'Error: {e}', file=sys.stderr)
")
    
    local nodes_file
    nodes_file=$(mm_temp_file nodes)
    echo "$nodes_list" > "$nodes_file"
    
    local first_node
    first_node=$(echo "$nodes_list" | cut -d: -f2 | head -1)
    
    local total_nodes
    total_nodes=$(echo "$nodes_list" | wc -l | tr -d ' ')
    
    local total_all_nodes
    total_all_nodes=$(echo "$proxies_response" | python3 -c "
import json, sys
data = json.load(sys.stdin)
proxies = data.get('proxies', {})
global_proxy = proxies.get('GLOBAL', {})
print(len(global_proxy.get('all', [])))
" 2>/dev/null)
    [[ -z "$total_all_nodes" ]] && total_all_nodes="$total_nodes"
    
    echo ""
    if [[ "$limit" -gt 0 ]]; then
        echo "正在测试节点延迟 (最多 $limit 个，每个最多 5 秒)..."
    else
        echo "正在测试节点延迟 (共 $total_nodes 个可测节点，代理组共 $total_all_nodes 个，每个最多 5 秒)..."
    fi
    echo ""
    
    local results_file
    results_file=$(mm_temp_file results)
    
    echo "=== 节点速度测试结果 $(date '+%Y-%m-%d %H:%M:%S') ===" > "$NODE_SPEED_FILE"
    echo "" >> "$NODE_SPEED_FILE"
    
    local test_count=0
    local success_count=0
    
    while IFS=: read -r node_index node_name; do
        [[ -z "$node_name" ]] && continue
        
        if [[ "$limit" -gt 0 && "$success_count" -ge "$limit" ]]; then
            break
        fi
        
        test_count=$((test_count + 1))
        
        # URL 编码节点名称
        local encoded_name
        encoded_name=$(python3 -c "import urllib.parse; print(urllib.parse.quote('''$node_name''', safe=''))" 2>/dev/null)
        
        # 测试延迟
        local delay_response
        delay_response=$(curl -s --max-time 5 "http://127.0.0.1:$API_PORT/proxies/${encoded_name}/delay?timeout=3000&url=http://www.gstatic.com/generate_204" 2>/dev/null)
        
        local delay=9999
        if [[ -n "$delay_response" ]]; then
            delay=$(echo "$delay_response" | python3 -c "import json,sys; print(json.load(sys.stdin).get('delay', 9999))" 2>/dev/null || echo 9999)
        fi
        
        local status="✗"
        local delay_str="超时"
        if [[ "$delay" -lt 9999 ]] 2>/dev/null; then
            status="✓"
            delay_str="${delay}ms"
            echo "$node_name:$delay" >> "$results_file"
            success_count=$((success_count + 1))
            printf "%-30s %6s\n" "$node_name" "$delay_str" >> "$NODE_SPEED_FILE"
        else
            printf "%-30s %6s\n" "$node_name" "超时" >> "$NODE_SPEED_FILE"
        fi
        
        echo "  [$node_index/$total_all_nodes] $status $node_name: $delay_str"
        sleep 0.2
    done < "$nodes_file"
    
    echo ""
    echo "=== 测试结果 ==="
    
    if [[ -s "$results_file" ]]; then
        echo ""
        echo "📊 最快的 10 个节点:"
        sort -t: -k2 -n "$results_file" | head -10 | while IFS=: read -r name delay; do
            printf "  %-30s %6sms\n" "$name" "$delay"
        done
        
        local fastest
        fastest=$(sort -t: -k2 -n "$results_file" | head -1 | cut -d: -f1)
        echo "$fastest" > "$FASTEST_NODE_FILE"
        
        echo "" >> "$NODE_SPEED_FILE"
        echo "=== 按速度排序 ===" >> "$NODE_SPEED_FILE"
        sort -t: -k2 -n "$results_file" | while IFS=: read -r name delay; do
            printf "%-30s %6sms\n" "$name" "$delay" >> "$NODE_SPEED_FILE"
        done
        
        echo ""
        echo "💡 最快节点: $fastest"
        echo ""
        echo "📁 完整结果已保存到: $NODE_SPEED_FILE"
    else
        echo "  没有成功测试的节点"
        if [[ -n "$first_node" ]]; then
            echo "$first_node" > "$FASTEST_NODE_FILE"
            echo ""
            echo "⚠️ 使用第一个节点作为备选: $first_node"
        fi
    fi
    
    echo ""
    echo "📈 统计: 测试 $test_count 个节点，成功 $success_count 个"
}

# 切换到最快节点
mm_switch_to_fastest() {
    local limit="${1:-0}"
    
    mm_info "自动选择最快节点..."
    
    if ! mm_is_running; then
        mm_error "服务未运行"
        return 1
    fi
    
    if [[ -n "$limit" && "$limit" =~ ^[0-9]+$ && "$limit" -gt 0 ]]; then
        mm_info "测到 $limit 个可用节点后停止..."
    fi
    
    mm_test_nodes "$limit"
    
    if [[ -f "$FASTEST_NODE_FILE" ]]; then
        local fastest
        fastest=$(cat "$FASTEST_NODE_FILE")
        echo ""
        mm_info "最快节点: $fastest"
        mm_switch_node "$fastest"
    else
        mm_error "未找到最快节点记录"
        return 1
    fi
}
