#!/bin/bash
#
# Mihomo Manager 安装脚本
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 项目目录
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_NAME="mihomo-manager"
SYMLINK_NAME="mm"
INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/mihomo"
LAUNCHD_DIR="$HOME/Library/LaunchAgents"
PLIST_NAME="com.mihomo.monitor.plist"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}   Mihomo Manager 安装脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查依赖
echo -e "${BLUE}[1/4] 检查依赖...${NC}"

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

# 安装命令
echo -e "${BLUE}[2/4] 安装命令...${NC}"

if [ ! -d "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR"
fi

if [ -L "$INSTALL_DIR/$SYMLINK_NAME" ] || [ -f "$INSTALL_DIR/$SYMLINK_NAME" ]; then
    rm -f "$INSTALL_DIR/$SYMLINK_NAME"
fi

ln -sf "$PROJECT_DIR/bin/$BIN_NAME" "$INSTALL_DIR/$SYMLINK_NAME"
chmod +x "$PROJECT_DIR/bin/$BIN_NAME"
chmod +x "$PROJECT_DIR/scripts/mihomo-monitor.sh"

echo -e "  ${GREEN}✓${NC} $SYMLINK_NAME -> $PROJECT_DIR/bin/$BIN_NAME"
echo ""

# 创建配置目录
echo -e "${BLUE}[3/4] 创建配置目录...${NC}"

if [ ! -d "$CONFIG_DIR" ]; then
    mkdir -p "$CONFIG_DIR"
    echo -e "  ${GREEN}✓${NC} 创建 $CONFIG_DIR"
else
    echo -e "  ${GREEN}✓${NC} 配置目录已存在: $CONFIG_DIR"
fi

# 创建日志目录
mkdir -p "$CONFIG_DIR/logs"
echo -e "  ${GREEN}✓${NC} 创建日志目录"
echo ""

# 安装监控服务
echo -e "${BLUE}[4/4] 安装监控服务...${NC}"

# 生成 plist 文件
PLIST_CONTENT=$(sed -e "s|{{PROJECT_DIR}}|$PROJECT_DIR|g" \
                     -e "s|{{CONFIG_DIR}}|$CONFIG_DIR|g" \
                     -e "s|{{HOME}}|$HOME|g" \
                     "$PROJECT_DIR/launchd/mihomo-monitor.plist")

# 停止旧服务
if [ -f "$LAUNCHD_DIR/$PLIST_NAME" ]; then
    launchctl unload "$LAUNCHD_DIR/$PLIST_NAME" 2>/dev/null || true
fi

# 删除旧的 plist（兼容旧版本）
if [ -f "$LAUNCHD_DIR/com.openclaw.mihomo-monitor.plist" ]; then
    launchctl unload "$LAUNCHD_DIR/com.openclaw.mihomo-monitor.plist" 2>/dev/null || true
    rm -f "$LAUNCHD_DIR/com.openclaw.mihomo-monitor.plist"
    echo -e "  ${YELLOW}✓${NC} 已移除旧版本监控服务"
fi

# 写入新 plist
echo "$PLIST_CONTENT" > "$LAUNCHD_DIR/$PLIST_NAME"
echo -e "  ${GREEN}✓${NC} 创建 launchd 配置"

# 加载服务
launchctl load "$LAUNCHD_DIR/$PLIST_NAME"
echo -e "  ${GREEN}✓${NC} 启动监控服务"
echo ""

# 检查 PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo -e "${YELLOW}注意: $INSTALL_DIR 不在 PATH 中${NC}"
    echo "请将以下内容添加到 ~/.zshrc 或 ~/.bashrc:"
    echo ""
    echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
    echo ""
fi

# 验证安装
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}   安装完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "目录结构:"
echo "  命令:     $INSTALL_DIR/$SYMLINK_NAME"
echo "  配置:     $CONFIG_DIR/"
echo "  日志:     $CONFIG_DIR/logs/"
echo "  监控:     $LAUNCHD_DIR/$PLIST_NAME"
echo ""
echo "常用命令:"
echo "  mm status      - 查看服务状态"
echo "  mm restart     - 重启服务"
echo "  mm fastest     - 选择最快节点"
echo "  mm --help      - 查看帮助"
echo ""
echo "监控服务每 5 分钟检查一次，自动重启异常退出的服务"