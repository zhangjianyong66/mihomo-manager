#!/bin/bash
#
# Mihomo Manager 卸载脚本
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/mihomo"
LAUNCHD_DIR="$HOME/Library/LaunchAgents"
SYMLINK_NAME="mm"
PLIST_NAME="com.mihomo.monitor.plist"

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}   Mihomo Manager 卸载脚本${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""

# 停止并卸载监控服务
echo -e "${BLUE}[1/3] 卸载监控服务...${NC}"

if [ -f "$LAUNCHD_DIR/$PLIST_NAME" ]; then
    launchctl unload "$LAUNCHD_DIR/$PLIST_NAME" 2>/dev/null || true
    rm -f "$LAUNCHD_DIR/$PLIST_NAME"
    echo -e "  ${GREEN}✓${NC} 已移除 $PLIST_NAME"
fi

# 兼容旧版本
if [ -f "$LAUNCHD_DIR/com.openclaw.mihomo-monitor.plist" ]; then
    launchctl unload "$LAUNCHD_DIR/com.openclaw.mihomo-monitor.plist" 2>/dev/null || true
    rm -f "$LAUNCHD_DIR/com.openclaw.mihomo-monitor.plist"
    echo -e "  ${GREEN}✓${NC} 已移除旧版本监控服务"
fi

echo ""

# 删除命令链接
echo -e "${BLUE}[2/3] 删除命令...${NC}"

if [ -L "$INSTALL_DIR/$SYMLINK_NAME" ]; then
    rm -f "$INSTALL_DIR/$SYMLINK_NAME"
    echo -e "  ${GREEN}✓${NC} 已删除 $SYMLINK_NAME"
else
    echo -e "  ${YELLOW}✓${NC} $SYMLINK_NAME 未安装或已删除"
fi

echo ""

# 询问是否删除配置
echo -e "${BLUE}[3/3] 配置文件${NC}"
echo ""
echo "配置文件保留在: $CONFIG_DIR/"
echo ""
echo "如需删除配置，请运行:"
echo "    rm -rf $CONFIG_DIR/"
echo ""

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}   卸载完成${NC}"
echo -e "${GREEN}========================================${NC}"