#!/bin/bash
#
# mihomo-manager 测试脚本
# 测试核心功能（使用 mock，不涉及真实服务）
#

set -e

# 忽略某些命令的错误以继续测试
set +e

# 获取项目目录
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_DIR="$PROJECT_DIR/scripts/tests"
LIB_DIR="$PROJECT_DIR/scripts/lib"

# 加载公共库
source "$LIB_DIR/common.sh"

# 测试计数
TESTS_PASSED=0
TESTS_FAILED=0

# 测试函数
test_start() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

test_pass() {
    echo -e "  ${GREEN}✓ PASS${NC}"
    ((TESTS_PASSED++))
}

test_fail() {
    echo -e "  ${RED}✗ FAIL${NC}: $1"
    ((TESTS_FAILED++))
}

# ============================================
# 测试套件
# ============================================

test_common_library() {
    echo ""
    echo "=== 测试公共库 ==="
    
    # 测试颜色定义
    test_start "颜色变量已定义"
    if [[ -n "$RED" && -n "$GREEN" && -n "$BLUE" ]]; then
        test_pass
    else
        test_fail "颜色变量未定义"
    fi
    
    # 测试配置路径
    test_start "配置路径正确"
    if [[ "$CONFIG_DIR" == "$HOME/.config/mihomo" ]]; then
        test_pass
    else
        test_fail "CONFIG_DIR = $CONFIG_DIR"
    fi
    
    # 测试打印函数存在
    test_start "打印函数存在"
    if type mm_info &>/dev/null && type mm_success &>/dev/null && \
       type mm_warn &>/dev/null && type mm_error &>/dev/null; then
        test_pass
    else
        test_fail "打印函数未定义"
    fi
    
    # 测试临时文件创建
    test_start "临时文件创建"
    local temp_file
    temp_file=$(mm_temp_file test)
    if [[ -f "$temp_file" ]]; then
        test_pass
        rm -f "$temp_file"
    else
        test_fail "临时文件未创建"
    fi
    
    # 测试依赖检查函数存在
    test_start "依赖检查函数存在"
    if type mm_check_deps &>/dev/null && type mm_has_command &>/dev/null; then
        test_pass
    else
        test_fail "依赖检查函数未定义"
    fi
}

test_proxy_parser() {
    echo ""
    echo "=== 测试 Python 解析模块 ==="
    
    # 测试 Python 模块存在
    test_start "proxy.py 模块存在"
    if [[ -f "$LIB_DIR/proxy.py" ]]; then
        test_pass
    else
        test_fail "proxy.py 不存在"
    fi
    
    # 测试 Python 模块可执行
    test_start "proxy.py 可执行"
    if python3 "$LIB_DIR/proxy.py" --help &>/dev/null; then
        test_pass
    else
        test_fail "proxy.py 执行失败"
    fi
    
    # 测试节点解析
    test_start "解析 vless 节点"
    local vless_url="vless://uuid-123@test.example.com:443?security=reality&pbk=key123&sni=test.example.com#测试节点"
    local temp_sub
    temp_sub=$(mktemp)
    echo "$vless_url" > "$temp_sub"
    if python3 "$LIB_DIR/proxy.py" "$temp_sub" --json 2>/dev/null | grep -q "测试节点"; then
        test_pass
    else
        test_fail "vless 解析失败"
    fi
    rm -f "$temp_sub"
    
    # 测试 vmess 解析
    test_start "解析 vmess 节点"
    local vmess_json='{"v":"2","ps":"测试VM","add":"test.com","port":"443","id":"uuid-456","aid":"0","net":"ws","type":"none","host":"test.com","path":"/path","tls":"tls"}'
    local vmess_b64
    vmess_b64=$(echo -n "$vmess_json" | base64)
    echo "vmess://$vmess_b64" > "$temp_sub"
    if python3 "$LIB_DIR/proxy.py" "$temp_sub" --json 2>/dev/null | grep -q "测试VM"; then
        test_pass
    else
        test_fail "vmess 解析失败"
    fi
    rm -f "$temp_sub"
    
    # 测试 trojan 解析
    test_start "解析 trojan 节点"
    echo "trojan://password123@trojan.example.com:443?sni=trojan.example.com#测试Trojan" > "$temp_sub"
    if python3 "$LIB_DIR/proxy.py" "$temp_sub" --json 2>/dev/null | grep -q "测试Trojan"; then
        test_pass
    else
        test_fail "trojan 解析失败"
    fi
    rm -f "$temp_sub"
    
    # 测试 ss 解析
    test_start "解析 ss 节点"
    local ss_b64
    ss_b64=$(echo -n "aes-256-gcm:password123" | base64)
    echo "ss://${ss_b64}@ss.example.com:8388#测试SS" > "$temp_sub"
    if python3 "$LIB_DIR/proxy.py" "$temp_sub" --json 2>/dev/null | grep -q "测试SS"; then
        test_pass
    else
        test_fail "ss 解析失败"
    fi
    rm -f "$temp_sub"
}

test_main_script() {
    echo ""
    echo "=== 测试主脚本 ==="
    
    local main_script="$PROJECT_DIR/bin/mihomo-manager"
    
    # 测试主脚本存在
    test_start "主脚本存在"
    if [[ -f "$main_script" ]]; then
        test_pass
    else
        test_fail "主脚本不存在"
    fi
    
    # 测试主脚本可执行
    test_start "主脚本可执行"
    if [[ -x "$main_script" ]]; then
        test_pass
    else
        test_fail "主脚本无执行权限"
    fi
    
    # 测试帮助输出
    test_start "帮助命令正常"
    if "$main_script" --help &>/dev/null; then
        test_pass
    else
        test_fail "帮助命令失败"
    fi
    
    # 测试版本输出
    test_start "版本命令正常"
    if "$main_script" --version &>/dev/null; then
        test_pass
    else
        test_fail "版本命令失败"
    fi
    
    # 测试未知命令处理
    test_start "未知命令返回错误"
    if ! "$main_script" unknown_cmd &>/dev/null; then
        test_pass
    else
        test_fail "未知命令未返回错误"
    fi
}

test_code_quality() {
    echo ""
    echo "=== 测试代码质量 ==="
    
    # 统计代码行数
    test_start "主脚本行数精简"
    local main_lines
    main_lines=$(wc -l < "$PROJECT_DIR/bin/mihomo-manager")
    if [[ "$main_lines" -lt 300 ]]; then
        test_pass
        echo "    主脚本行数: $main_lines (原始: 1474)"
    else
        test_fail "主脚本行数过多: $main_lines"
    fi
    
    # 检查是否有重复代码
    test_start "库文件存在"
    local lib_count
    lib_count=$(find "$LIB_DIR" -name "*.sh" -o -name "*.py" | wc -l)
    if [[ "$lib_count" -ge 4 ]]; then
        test_pass
        echo "    库文件数量: $lib_count"
    else
        test_fail "库文件数量不足: $lib_count"
    fi
    
    # 检查 Python 代码已从主脚本分离
    test_start "Python 代码已分离"
    if ! grep -q "python3 << 'PYTHON_SCRIPT'" "$PROJECT_DIR/bin/mihomo-manager"; then
        test_pass
    else
        test_fail "主脚本中仍有嵌入式 Python 代码"
    fi
}

# ============================================
# 主入口
# ============================================

echo ""
echo "========================================"
echo "   Mihomo Manager 测试套件"
echo "========================================"

# 运行所有测试
test_common_library
test_proxy_parser
test_main_script
test_code_quality

# 清理
mm_cleanup

# 输出结果
echo ""
echo "========================================"
echo "   测试结果"
echo "========================================"
echo -e "通过: ${GREEN}$TESTS_PASSED${NC}"
echo -e "失败: ${RED}$TESTS_FAILED${NC}"
echo ""

if [[ "$TESTS_FAILED" -eq 0 ]]; then
    echo -e "${GREEN}所有测试通过！${NC}"
    exit 0
else
    echo -e "${RED}有测试失败，请检查${NC}"
    exit 1
fi
