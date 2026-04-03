#!/bin/bash
#
# Mihomo Manager 测试脚本
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 测试计数
TESTS_PASSED=0
TESTS_FAILED=0

# 测试函数
test_case() {
    local name="$1"
    local result="$2"

    if [ "$result" = "pass" ]; then
        echo -e "${GREEN}✓${NC} $name"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}✗${NC} $name"
        ((TESTS_FAILED++))
    fi
}

echo "=========================================="
echo "  Mihomo Manager 测试套件"
echo "=========================================="
echo ""

# 1. 命令存在性测试
echo -e "${YELLOW}[1] 命令测试${NC}"

if command -v mm >/dev/null 2>&1; then
    test_case "mm 命令存在" "pass"
else
    test_case "mm 命令存在" "fail"
fi

if mm --help >/dev/null 2>&1; then
    test_case "mm --help 可执行" "pass"
else
    test_case "mm --help 可执行" "fail"
fi

echo ""

# 2. 依赖测试
echo -e "${YELLOW}[2] 依赖测试${NC}"

for cmd in curl python3 lsof; do
    if command -v $cmd >/dev/null 2>&1; then
        test_case "$cmd 已安装" "pass"
    else
        test_case "$cmd 已安装" "fail"
    fi
done

echo ""

# 3. 配置文件测试
echo -e "${YELLOW}[3] 配置文件测试${NC}"

CONFIG_FILE="$HOME/.config/mihomo/config.yaml"

if [ -f "$CONFIG_FILE" ]; then
    test_case "配置文件存在" "pass"

    # 检查端口配置
    if grep -q "mixed-port: 10808" "$CONFIG_FILE"; then
        test_case "mixed-port: 10808 配置正确" "pass"
    else
        test_case "mixed-port: 10808 配置正确" "fail"
    fi

    if grep -q "external-controller: 127.0.0.1:9090" "$CONFIG_FILE"; then
        test_case "external-controller 配置正确" "pass"
    else
        test_case "external-controller 配置正确" "fail"
    fi
else
    test_case "配置文件存在" "fail"
fi

echo ""

# 4. 服务状态测试
echo -e "${YELLOW}[4] 服务状态测试${NC}"

if mm status 2>&1 | grep -q "运行中"; then
    test_case "服务运行中" "pass"
else
    test_case "服务运行中" "fail"
fi

if lsof -i:10808 >/dev/null 2>&1; then
    test_case "端口 10808 监听中" "pass"
else
    test_case "端口 10808 监听中" "fail"
fi

if lsof -i:9090 >/dev/null 2>&1; then
    test_case "端口 9090 监听中" "pass"
else
    test_case "端口 9090 监听中" "fail"
fi

echo ""

# 5. 监控服务测试
echo -e "${YELLOW}[5] 监控服务测试${NC}"

if [ -f "$HOME/Library/LaunchAgents/com.mihomo.monitor.plist" ]; then
    test_case "监控 plist 文件存在" "pass"
else
    test_case "监控 plist 文件存在" "fail"
fi

if launchctl list | grep -q "com.mihomo.monitor"; then
    test_case "监控服务已加载" "pass"
else
    test_case "监控服务已加载" "fail"
fi

echo ""

# 6. API 测试
echo -e "${YELLOW}[6] API 测试${NC}"

if curl -s --max-time 5 "http://127.0.0.1:9090/proxies" | grep -q "proxies"; then
    test_case "API 可访问" "pass"
else
    test_case "API 可访问" "fail"
fi

echo ""

# 测试结果
echo "=========================================="
echo -e "  ${GREEN}通过: $TESTS_PASSED${NC}"
echo -e "  ${RED}失败: $TESTS_FAILED${NC}"
echo "=========================================="

if [ $TESTS_FAILED -eq 0 ]; then
    exit 0
else
    exit 1
fi