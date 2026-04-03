#!/bin/bash
#
# Mihomo 服务监控脚本
# 定期检查服务状态，如果不存在则自动重启
#

set -e

# 配置
MIHOMO_BIN="$HOME/.local/bin/mihomo"
CONFIG_DIR="$HOME/.config/mihomo"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
LOG_FILE="$CONFIG_DIR/mihomo-monitor.log"
MIXED_PORT="${MIHOMO_MIXED_PORT:-10808}"

# 最大日志行数
MAX_LOG_LINES=500

# 记录日志
log_message() {
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "[$timestamp] $1" >> "$LOG_FILE"
}

# 检查服务是否运行
check_service() {
    # 匹配两种启动方式：相对路径和绝对路径
    pgrep -f "mihomo.*-f.*config.yaml" > /dev/null 2>&1
}

# 检查端口是否监听
check_port() {
    lsof -Pi :$MIXED_PORT -sTCP:LISTEN -t > /dev/null 2>&1
}

# 启动服务
start_service() {
    log_message "INFO: 正在启动 Mihomo 服务..."

    cd "$CONFIG_DIR"
    nohup "$MIHOMO_BIN" -f "$CONFIG_FILE" >> "$CONFIG_DIR/mihomo.log" 2>&1 &

    # 等待启动
    sleep 3

    if check_service; then
        local pid
        pid=$(pgrep -f "mihomo.*-f.*config.yaml" | head -1)
        log_message "INFO: 服务启动成功 (PID: $pid)"
        return 0
    else
        log_message "ERROR: 服务启动失败"
        return 1
    fi
}

# 清理旧日志
cleanup_log() {
    if [ -f "$LOG_FILE" ]; then
        local line_count
        line_count=$(wc -l < "$LOG_FILE")
        if [ "$line_count" -gt "$MAX_LOG_LINES" ]; then
            tail -n "$MAX_LOG_LINES" "$LOG_FILE" > "$LOG_FILE.tmp"
            mv "$LOG_FILE.tmp" "$LOG_FILE"
        fi
    fi
}

# 主逻辑
main() {
    # 清理日志
    cleanup_log

    log_message "INFO: 开始检查 Mihomo 服务状态"

    # 检查进程是否存在
    if check_service; then
        # 进程存在，再检查端口
        if check_port; then
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