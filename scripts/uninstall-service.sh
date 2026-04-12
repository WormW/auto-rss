#!/bin/bash
# Auto-RSS 服务卸载脚本

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

OS="$(uname -s)"
SERVICE_NAME="auto-rss"

echo -e "${YELLOW}卸载 Auto-RSS 服务...${NC}"

case "$OS" in
    Darwin)
        PLIST_TARGET="$HOME/Library/LaunchAgents/com.wormw.auto-rss.plist"
        
        if [ -f "$PLIST_TARGET" ]; then
            echo "停止服务..."
            launchctl stop com.wormw.auto-rss 2>/dev/null || true
            launchctl unload "$PLIST_TARGET" 2>/dev/null || true
            rm "$PLIST_TARGET"
            echo -e "${GREEN}服务已卸载${NC}"
        else
            echo -e "${YELLOW}服务未安装${NC}"
        fi
        ;;
        
    Linux)
        if [ "$EUID" -ne 0 ]; then
            echo -e "${RED}错误: 卸载 systemd 服务需要 root 权限${NC}"
            exit 1
        fi
        
        SERVICE_TARGET="/etc/systemd/system/auto-rss.service"
        
        if [ -f "$SERVICE_TARGET" ]; then
            echo "停止并禁用服务..."
            systemctl stop auto-rss 2>/dev/null || true
            systemctl disable auto-rss 2>/dev/null || true
            rm "$SERVICE_TARGET"
            systemctl daemon-reload
            echo -e "${GREEN}服务已卸载${NC}"
        else
            echo -e "${YELLOW}服务未安装${NC}"
        fi
        ;;
        
    *)
        echo -e "${RED}不支持的操作系统: $OS${NC}"
        exit 1
        ;;
esac
