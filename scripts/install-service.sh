#!/bin/bash
# Auto-RSS 服务安装脚本
# 支持 macOS (launchd) 和 Linux (systemd)

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 项目目录
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY_NAME="auto-rss"
SERVICE_NAME="auto-rss"

# 检测操作系统
OS="$(uname -s)"

echo -e "${GREEN}Auto-RSS 服务安装脚本${NC}"
echo "项目目录: $PROJECT_DIR"
echo "操作系统: $OS"
echo ""

# 检查二进制文件是否存在
if [ ! -f "$PROJECT_DIR/$BINARY_NAME" ]; then
    echo -e "${YELLOW}警告: 未找到 $BINARY_NAME 二进制文件${NC}"
    echo "正在构建..."
    cd "$PROJECT_DIR"
    make build
fi

# 创建日志目录
mkdir -p "$PROJECT_DIR/logs"

# 创建数据目录
mkdir -p "$PROJECT_DIR/data"

case "$OS" in
    Darwin)
        # macOS - 使用 launchd
        echo -e "${GREEN}安装 macOS launchd 服务...${NC}"
        
        PLIST_SOURCE="$PROJECT_DIR/scripts/auto-rss.plist"
        PLIST_TARGET="$HOME/Library/LaunchAgents/com.wormw.auto-rss.plist"
        
        # 复制 plist 文件
        cp "$PLIST_SOURCE" "$PLIST_TARGET"
        
        # 加载服务
        launchctl unload "$PLIST_TARGET" 2>/dev/null || true
        launchctl load "$PLIST_TARGET"
        
        echo -e "${GREEN}服务已安装!${NC}"
        echo "启动命令: launchctl start com.wormw.auto-rss"
        echo "停止命令: launchctl stop com.wormw.auto-rss"
        echo "查看状态: launchctl list | grep auto-rss"
        echo "日志文件: $PROJECT_DIR/logs/auto-rss.{out,err}.log"
        
        # 询问是否立即启动
        read -p "是否立即启动服务? (y/n): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            launchctl start com.wormw.auto-rss
            echo -e "${GREEN}服务已启动${NC}"
        fi
        ;;
        
    Linux)
        # Linux - 使用 systemd
        echo -e "${GREEN}安装 Linux systemd 服务...${NC}"
        
        if [ "$EUID" -ne 0 ]; then
            echo -e "${RED}错误: 安装 systemd 服务需要 root 权限${NC}"
            echo "请使用: sudo $0"
            exit 1
        fi
        
        SERVICE_SOURCE="$PROJECT_DIR/scripts/auto-rss.service"
        SERVICE_TARGET="/etc/systemd/system/auto-rss.service"
        
        # 获取当前用户
        CURRENT_USER="${SUDO_USER:-$USER}"
        
        # 替换占位符
        sed -e "s|%USER%|$CURRENT_USER|g" \
            -e "s|%WORK_DIR%|$PROJECT_DIR|g" \
            "$SERVICE_SOURCE" > "$SERVICE_TARGET"
        
        # 重新加载 systemd
        systemctl daemon-reload
        
        # 启用服务
        systemctl enable auto-rss
        
        echo -e "${GREEN}服务已安装!${NC}"
        echo "启动命令: sudo systemctl start auto-rss"
        echo "停止命令: sudo systemctl stop auto-rss"
        echo "查看状态: sudo systemctl status auto-rss"
        echo "查看日志: sudo journalctl -u auto-rss -f"
        
        # 询问是否立即启动
        read -p "是否立即启动服务? (y/n): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            systemctl start auto-rss
            echo -e "${GREEN}服务已启动${NC}"
        fi
        ;;
        
    *)
        echo -e "${RED}不支持的操作系统: $OS${NC}"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}安装完成!${NC}"
