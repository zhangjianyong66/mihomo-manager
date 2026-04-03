#!/bin/bash
#
# Mihomo Manager 安装脚本
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 项目目录
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_NAME="mihomo-manager"
SYMLINK_NAME="mm"
INSTALL_DIR="$HOME/.local/bin"

echo -e "${GREEN}Mihomo Manager 安装脚本${NC}"
echo ""

# 检查依赖
echo -e "${YELLOW}检查依赖...${NC}"

check_command() {
    if command -v "$1" >/dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} $1"
        return 0
    else
        echo -e "  ${RED}✗${NC} $1 (未安装)"
        return 1
    fi
}

missing_deps=0
check_command curl || missing_deps=1
check_command python3 || missing_deps=1
check_command lsof || missing_deps=1

if [ $missing_deps -eq 1 ]; then
    echo ""
    echo -e "${RED}缺少必要依赖，请先安装${NC}"
    exit 1
fi

echo ""

# 创建安装目录
if [ ! -d "$INSTALL_DIR" ]; then
    echo -e "${YELLOW}创建安装目录: $INSTALL_DIR${NC}"
    mkdir -p "$INSTALL_DIR"
fi

# 检查是否已安装
if [ -L "$INSTALL_DIR/$SYMLINK_NAME" ] || [ -f "$INSTALL_DIR/$SYMLINK_NAME" ]; then
    echo -e "${YELLOW}已存在 $SYMLINK_NAME，正在更新...${NC}"
    rm -f "$INSTALL_DIR/$SYMLINK_NAME"
fi

# 创建符号链接
echo -e "${YELLOW}安装 $SYMLINK_NAME -> $PROJECT_DIR/bin/$BIN_NAME${NC}"
ln -sf "$PROJECT_DIR/bin/$BIN_NAME" "$INSTALL_DIR/$SYMLINK_NAME"

# 检查 PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo -e "${YELLOW}注意: $INSTALL_DIR 不在 PATH 中${NC}"
    echo "请将以下内容添加到 ~/.zshrc 或 ~/.bashrc:"
    echo ""
    echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
    echo ""
fi

# 验证安装
if command -v mm >/dev/null 2>&1; then
    echo -e "${GREEN}安装成功！${NC}"
    echo ""
    echo "运行 'mm --help' 查看使用说明"
else
    echo -e "${GREEN}安装完成！${NC}"
    echo ""
    echo "请运行以下命令使 PATH 生效:"
    echo "    source ~/.zshrc"
    echo ""
    echo "或重新打开终端"
fi