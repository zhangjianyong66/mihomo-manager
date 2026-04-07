#!/bin/bash
#
# mihomo-manager 共享函数库
# 被所有脚本共享的通用函数和常量
#

# 防止重复加载
[[ -n "$_MM_COMMON_LOADED" ]] && return
_MM_COMMON_LOADED=1

# ============================================
# 颜色定义
# ============================================
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly CYAN='\033[0;36m'
readonly NC='\033[0m' # No Color

# ============================================
# 配置路径
# ============================================
readonly MIHOMO_BIN="${MIHOMO_BIN:-$HOME/.local/bin/mihomo}"
readonly CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/mihomo}"
readonly CONFIG_FILE="$CONFIG_DIR/config.yaml"
readonly CONFIG_BACKUP="$CONFIG_DIR/config.yaml.bak"
readonly SUBSCRIPTION_URL_FILE="$CONFIG_DIR/subscription.url"
readonly LOG_FILE="$CONFIG_DIR/mihomo.log"
readonly MONITOR_LOG_FILE="$CONFIG_DIR/mihomo-monitor.log"
readonly NODE_SPEED_FILE="$CONFIG_DIR/node_speed.txt"
readonly FASTEST_NODE_FILE="$CONFIG_DIR/fastest_node.txt"

# 端口配置（可通过环境变量覆盖）
readonly MIXED_PORT="${MIHOMO_MIXED_PORT:-10808}"
readonly SOCKS_PORT="${MIHOMO_SOCKS_PORT:-7891}"
readonly API_PORT="${MIHOMO_API_PORT:-9090}"

# 白名单标记
readonly WHITELIST_MARKER="# ===== 白名单（不走代理）====="
readonly WHITELIST_END="# ===== 白名单结束 ====="

# ============================================
# 打印函数
# ============================================

# 打印信息
mm_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# 打印成功
mm_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# 打印警告
mm_warn() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# 打印错误
mm_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 打印调试信息（仅在 DEBUG=1 时显示）
mm_debug() {
    [[ "${DEBUG:-0}" == "1" ]] && echo -e "${CYAN}[DEBUG]${NC} $1" >&2
}

# ============================================
# 临时文件管理
# ============================================

# 临时文件列表（用于清理）
declare -a _MM_TEMP_FILES=()

# 创建临时文件
mm_temp_file() {
    local suffix="${1:-tmp}"
    local temp_file
    temp_file=$(mktemp "/tmp/mihomo_${suffix}.XXXXXX")
    _MM_TEMP_FILES+=("$temp_file")
    echo "$temp_file"
}

# 清理所有临时文件
mm_cleanup() {
    for f in "${_MM_TEMP_FILES[@]}"; do
        [[ -f "$f" ]] && rm -f "$f"
    done
    _MM_TEMP_FILES=()
}

# 注册退出时的清理
mm_register_cleanup() {
    trap mm_cleanup EXIT
}

# ============================================
# 依赖检查
# ============================================

# 检查命令是否存在
mm_has_command() {
    command -v "$1" >/dev/null 2>&1
}

# 检查所有必要依赖
mm_check_deps() {
    local missing=()
    
    mm_has_command curl || missing+=("curl")
    mm_has_command python3 || missing+=("python3")
    mm_has_command lsof || missing+=("lsof")
    
    if [[ ${#missing[@]} -gt 0 ]]; then
        mm_error "缺少必要依赖: ${missing[*]}"
        return 1
    fi
    return 0
}

# 检查 mihomo 二进制
mm_check_mihomo() {
    if [[ ! -x "$MIHOMO_BIN" ]]; then
        mm_error "Mihomo 二进制不存在或不可执行: $MIHOMO_BIN"
        return 1
    fi
    return 0
}

# 检查配置目录
mm_check_config_dir() {
    if [[ ! -d "$CONFIG_DIR" ]]; then
        mm_error "配置目录不存在: $CONFIG_DIR"
        return 1
    fi
    return 0
}

# 检查配置文件
mm_check_config() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        mm_warn "配置文件不存在: $CONFIG_FILE"
        return 1
    fi
    return 0
}

# 完整依赖检查
mm_check_all() {
    mm_check_deps || return 1
    mm_check_mihomo || return 1
    mm_check_config_dir || return 1
}

# ============================================
# 服务状态
# ============================================

# 缓存的服务状态
_CACHED_STATUS=""

# 检查服务状态（带缓存）
mm_check_status() {
    # 如果有缓存，直接返回
    if [[ -n "$_CACHED_STATUS" ]]; then
        echo "$_CACHED_STATUS"
        return
    fi
    
    local pid
    # 先尝试匹配带参数的进程
    pid=$(pgrep -f "mihomo.*-f.*config.yaml" 2>/dev/null | head -1)
    
    # 如果没找到，尝试匹配简单的 mihomo 进程（并验证端口）
    if [[ -z "$pid" ]]; then
        pid=$(pgrep -x "mihomo" 2>/dev/null | head -1)
        # 验证该进程确实监听了代理端口
        if [[ -n "$pid" ]]; then
            if ! lsof -Pi :"$MIXED_PORT" -sTCP:LISTEN -t 2>/dev/null | grep -q "^${pid}$"; then
                pid=""  # 端口不匹配，不是我们的服务
            fi
        fi
    fi
    
    if [[ -n "$pid" ]]; then
        _CACHED_STATUS="running:$pid"
    else
        _CACHED_STATUS="stopped"
    fi
    echo "$_CACHED_STATUS"
}

# 清除状态缓存
mm_clear_status_cache() {
    _CACHED_STATUS=""
}

# 检查服务是否运行
mm_is_running() {
    [[ "$(mm_check_status)" != "stopped" ]]
}

# 获取服务 PID
mm_get_pid() {
    local status
    status=$(mm_check_status)
    if [[ "$status" != "stopped" ]]; then
        echo "$status" | cut -d: -f2
    fi
}

# 检查端口是否监听
mm_check_port() {
    local port="${1:-$MIXED_PORT}"
    lsof -Pi :"$port" -sTCP:LISTEN -t >/dev/null 2>&1
}

# ============================================
# 配置管理
# ============================================

# 备份配置
mm_backup_config() {
    local backup_name="config.yaml.$(date +%Y%m%d_%H%M%S).bak"
    
    if [[ -f "$CONFIG_FILE" ]]; then
        cp "$CONFIG_FILE" "$CONFIG_DIR/$backup_name"
        cp "$CONFIG_FILE" "$CONFIG_BACKUP"
        mm_success "配置已备份到: $backup_name"
        return 0
    fi
    return 1
}

# 测试配置
mm_test_config() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        mm_error "配置文件不存在"
        return 1
    fi
    
    if "$MIHOMO_BIN" -t -f "$CONFIG_FILE" >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# 获取保存的订阅 URL
mm_get_subscription_url() {
    if [[ -f "$SUBSCRIPTION_URL_FILE" ]]; then
        cat "$SUBSCRIPTION_URL_FILE"
    fi
}

# 保存订阅 URL
mm_save_subscription_url() {
    local url="$1"
    if [[ -z "$url" ]]; then
        mm_error "URL 不能为空"
        return 1
    fi
    
    echo "$url" > "$SUBSCRIPTION_URL_FILE"
    chmod 600 "$SUBSCRIPTION_URL_FILE"
    mm_success "订阅 URL 已保存"
}

# ============================================
# 下载工具
# ============================================

# 下载文件（自动选择 curl/wget）
mm_download() {
    local url="$1"
    local output="$2"
    local use_proxy="${3:-false}"
    
    local proxy_opt=""
    if [[ "$use_proxy" == "true" ]]; then
        proxy_opt="--proxy http://127.0.0.1:$MIXED_PORT"
    else
        proxy_opt="--proxy \"\""
    fi
    
    if mm_has_command curl; then
        eval curl -fsSL $proxy_opt "$url" -o "$output" 2>/dev/null
    elif mm_has_command wget; then
        if [[ "$use_proxy" == "true" ]]; then
            wget -q -e "use_proxy=yes" -e "http_proxy=http://127.0.0.1:$MIXED_PORT" "$url" -O "$output" 2>/dev/null
        else
            wget -q --no-proxy "$url" -O "$output" 2>/dev/null
        fi
    else
        mm_error "未找到 curl 或 wget"
        return 1
    fi
}

# ============================================
# 版本信息
# ============================================

# 获取脚本版本
mm_version() {
    local version_file="${0%/*}/../../VERSION"
    if [[ -f "$version_file" ]]; then
        cat "$version_file"
    else
        echo "unknown"
    fi
}
