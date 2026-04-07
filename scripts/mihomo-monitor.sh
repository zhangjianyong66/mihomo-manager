#!/bin/bash
#
# Mihomo 服务监控脚本
# 定期检查服务状态，如果不存在则自动重启
#

set -e

# 加载公共库
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

# 最大日志行数
readonly MAX_LOG_LINES=500

# 记录日志
log_message() {
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "[$timestamp] $1" >> "$MONITOR_LOG_FILE"
}

# 启动服务
start_service() {
    log_message "INFO: 正在启动 Mihomo 服务..."
    
    cd "$CONFIG_DIR"
    nohup "$MIHOMO_BIN" -f "$CONFIG_FILE" >> "$LOG_FILE" 2>&1 &
    
    sleep 3
    
    if mm_is_running; then
        local pid
        pid=$(mm_get_pid)
        log_message "INFO: 服务启动成功 (PID: $pid)"
        return 0
    else
        log_message "ERROR: 服务启动失败"
        return 1
    fi
}

# 清理旧日志
cleanup_log() {
    if [[ -f "$MONITOR_LOG_FILE" ]]; then
        local line_count
        line_count=$(wc -l < "$MONITOR_LOG_FILE")
        if [[ "$line_count" -gt "$MAX_LOG_LINES" ]]; then
            tail -n "$MAX_LOG_LINES" "$MONITOR_LOG_FILE" > "$MONITOR_LOG_FILE.tmp"
            mv "$MONITOR_LOG_FILE.tmp" "$MONITOR_LOG_FILE"
        fi
    fi
}

# 主逻辑
main() {
    # 清理日志
    cleanup_log
    
    log_message "INFO: 开始检查 Mihomo 服务状态"
    
    # 检查进程是否存在
    if mm_is_running; then
        # 进程存在，再检查端口
        if mm_check_port "$MIXED_PORT"; then
            log_message "INFO: 服务运行正常，端口 $MIXED_PORT 监听中"
            exit 0
        else
            log_message "WARNING: 进程存在但端口未监听，尝试重启..."
            pkill -f "mihomo" 2>/dev/null || true
            sleep 1
            start_service
        fi
    else
        # 进程不存在，启动服务
        log_message "WARNING: 服务未运行，正在启动..."
        start_service
    fi
}

# 运行主函数
main
