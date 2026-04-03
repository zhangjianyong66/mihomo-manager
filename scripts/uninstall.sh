#!/bin/bash
#
# Mihomo Manager 卸载脚本
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

INSTALL_DIR="$HOME/.local/bin"
SYMLINK_NAME="mm"

echo -e "${YELLOW}Mihomo Manager 卸载脚本${NC}"
echo ""

# 删除符号链接
if [ -L "$INSTALL_DIR/$SYMLINK_NAME" ]; then
    rm -f "$INSTALL_DIR/$SYMLINK_NAME"
    echo -e "${GREEN}已删除 $SYMLINK_NAME${NC}"
else
    echo -e "${YELLOW}$SYMLINK_NAME 未安装或已删除${NC}"
fi

echo ""
echo -e "${GREEN}卸载完成${NC}"
echo ""
echo "配置文件保留在: ~/.config/mihomo/"
echo "如需删除配置，运行: rm -rf ~/.config/mihomo/"