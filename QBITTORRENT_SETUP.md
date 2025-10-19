# qBittorrent 测试环境设置指南

## 方法一：使用 Docker（推荐）

### 1. 确保 Docker Desktop 正在运行

### 2. 配置 Docker 镜像源（如果网络访问慢）

在 Docker Desktop 设置中添加镜像源：
```json
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn",
    "https://hub-mirror.c.163.com"
  ]
}
```

### 3. 使用 docker-compose 启动

```bash
cd /Users/WormW/work/bgm/auto-rss
docker-compose -f docker-compose.qbittorrent.yml up -d
```

### 4. 手动 Docker 启动（备用方案）

```bash
docker run -d \
  --name auto-rss-qbittorrent \
  -e PUID=1000 \
  -e PGID=1000 \
  -e TZ=Asia/Shanghai \
  -e WEBUI_PORT=8080 \
  -p 8080:8080 \
  -p 6881:6881 \
  -p 6881:6881/udp \
  -v $(pwd)/data/qbittorrent/config:/config \
  -v $(pwd)/data/downloads:/downloads \
  --restart unless-stopped \
  linuxserver/qbittorrent:latest
```

## 方法二：使用 Homebrew 安装（macOS）

### 1. 安装 qBittorrent

```bash
brew install --cask qbittorrent
```

### 2. 启动应用

```bash
open -a qBittorrent
```

### 3. 配置 Web UI

1. 打开 qBittorrent 应用
2. 偏好设置 → Web UI
3. 启用 Web 用户界面
4. 端口：8080
5. 用户名：admin
6. 密码：adminadmin
7. 取消勾选 "对本地主机上的客户端跳过身份验证"

## 方法三：下载官方二进制文件

从官网下载：https://www.qbittorrent.org/download.php

## 访问 Web UI

启动后访问：http://localhost:8080

默认凭据（Docker）：
- 首次启动时，密码会在容器日志中显示
- 查看密码：`docker logs auto-rss-qbittorrent`
- 默认用户名：admin

## 项目配置

在 auto-rss 中配置 qBittorrent：
- Host: http://localhost:8080
- Username: admin
- Password: [查看容器日志获取]

## 目录结构

```
/Users/WormW/work/bgm/auto-rss/
├── data/
│   ├── downloads/          # 下载文件存储位置
│   └── qbittorrent/
│       └── config/         # qBittorrent 配置文件
└── docker-compose.qbittorrent.yml
```

## 常用命令

```bash
# 启动
docker-compose -f docker-compose.qbittorrent.yml up -d

# 停止
docker-compose -f docker-compose.qbittorrent.yml down

# 查看日志
docker logs -f auto-rss-qbittorrent

# 重启
docker-compose -f docker-compose.qbittorrent.yml restart

# 查看初始密码
docker logs auto-rss-qbittorrent 2>&1 | grep -i password
```

## 故障排除

### Docker 拉取镜像超时

1. 配置 Docker 镜像源（见上方）
2. 或使用 HTTP 代理：
   ```bash
   export HTTP_PROXY=http://127.0.0.1:7890
   export HTTPS_PROXY=http://127.0.0.1:7890
   docker-compose -f docker-compose.qbittorrent.yml up -d
   ```

### 无法访问 Web UI

1. 检查容器状态：`docker ps`
2. 检查端口占用：`lsof -i :8080`
3. 查看容器日志：`docker logs auto-rss-qbittorrent`
