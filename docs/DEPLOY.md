# Auto-RSS 部署文档

> **版本**: v0.1.0
> **更新日期**: 2025-10-19

本文档提供 Auto-RSS 的完整部署指南，包括 Docker 部署、二进制部署和源码部署。

---

## 📋 目录

1. [系统要求](#系统要求)
2. [快速开始](#快速开始)
3. [Docker 部署](#docker-部署)
4. [二进制部署](#二进制部署)
5. [源码部署](#源码部署)
6. [配置说明](#配置说明)
7. [常见问题](#常见问题)
8. [升级指南](#升级指南)

---

## 系统要求

### 硬件要求

| 配置 | 最低要求 | 推荐配置 |
|------|----------|----------|
| CPU | 1 核 | 2 核+ |
| 内存 | 256 MB | 512 MB+ |
| 磁盘 | 100 MB (程序) + 下载空间 | 1 GB+ |

### 软件要求

**Docker 部署**:
- Docker 20.10+
- Docker Compose 2.0+ (可选)

**二进制部署**:
- Linux / macOS / Windows
- 无其他依赖

**源码部署**:
- Go 1.25+
- Node.js 18+
- SQLite 3.40+

### 外部依赖

- **qBittorrent**: 3.0+ (必须)
  - 需要开启 Web UI
  - 建议版本: 4.5+

---

## 快速开始

### 使用 Docker (推荐)

```bash
# 1. 拉取镜像
docker pull auto-rss:latest

# 2. 运行容器
docker run -d \
  --name auto-rss \
  -p 7892:7892 \
  -e QB_HOST=http://192.168.1.100:8080 \
  -e QB_USERNAME=admin \
  -e QB_PASSWORD=yourpassword \
  -v ./data:/data \
  -v ./downloads:/downloads \
  auto-rss:latest

# 3. 访问 Web UI
# 浏览器打开: http://localhost:7892
```

### 使用二进制

```bash
# 1. 下载二进制文件
wget https://github.com/WormW/auto-rss/releases/download/v0.1.0/auto-rss-linux-amd64

# 2. 添加执行权限
chmod +x auto-rss-linux-amd64

# 3. 创建配置文件
cat > .env <<EOF
QB_HOST=http://localhost:8080
QB_USERNAME=admin
QB_PASSWORD=yourpassword
EOF

# 4. 运行程序
./auto-rss-linux-amd64

# 5. 访问 Web UI
# 浏览器打开: http://localhost:7892
```

---

## Docker 部署

### 方式 1: Docker CLI

#### 基础部署

```bash
docker run -d \
  --name auto-rss \
  --restart unless-stopped \
  -p 7892:7892 \
  -e QB_HOST=http://192.168.1.100:8080 \
  -e QB_USERNAME=admin \
  -e QB_PASSWORD=yourpassword \
  -v $(pwd)/data:/data \
  -v $(pwd)/downloads:/downloads \
  auto-rss:latest
```

#### 完整配置

```bash
docker run -d \
  --name auto-rss \
  --restart unless-stopped \
  -p 7892:7892 \
  -e DB_PATH=/data/auto-rss.db \
  -e QB_HOST=http://192.168.1.100:8080 \
  -e QB_USERNAME=admin \
  -e QB_PASSWORD=yourpassword \
  -e RSS_INTERVAL=30m \
  -e LOG_LEVEL=info \
  -e SERVER_PORT=7892 \
  -e DOWNLOAD_PATH=/downloads \
  -e TZ=Asia/Shanghai \
  -v $(pwd)/data:/data \
  -v $(pwd)/downloads:/downloads \
  --network bridge \
  auto-rss:latest
```

### 方式 2: Docker Compose

#### 创建 docker-compose.yml

```yaml
version: '3.8'

services:
  auto-rss:
    image: auto-rss:latest
    container_name: auto-rss
    restart: unless-stopped
    ports:
      - "7892:7892"
    environment:
      # 数据库配置
      - DB_PATH=/data/auto-rss.db

      # qBittorrent 配置
      - QB_HOST=http://192.168.1.100:8080
      - QB_USERNAME=admin
      - QB_PASSWORD=yourpassword

      # RSS 配置
      - RSS_INTERVAL=30m

      # 日志配置
      - LOG_LEVEL=info

      # 服务配置
      - SERVER_PORT=7892
      - DOWNLOAD_PATH=/downloads

      # 访问保护（可选）
      # 启用前必须修改 JWT_SECRET 和 JWT_PASSWORD，不能使用默认值。
      - AUTH_ENABLED=false
      - JWT_SECRET=change-this-to-a-long-random-secret-at-least-32-chars
      - JWT_USERNAME=admin
      - JWT_PASSWORD=change-this-password

      # 时区配置
      - TZ=Asia/Shanghai

    volumes:
      # 数据持久化
      - ./data:/data

      # 下载目录 (与 qBittorrent 共享)
      - ./downloads:/downloads

    networks:
      - media

    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:7892/api/v1/config"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s

networks:
  media:
    driver: bridge
```

#### 启动服务

```bash
# 创建必要目录
mkdir -p data downloads

# 启动服务
docker-compose up -d

# 查看日志
docker-compose logs -f auto-rss

# 停止服务
docker-compose down

# 停止并删除数据
docker-compose down -v
```

### 多架构支持

Auto-RSS 提供多架构 Docker 镜像：

```bash
# AMD64 (x86_64)
docker pull auto-rss:latest

# ARM64 (aarch64, Apple Silicon, Raspberry Pi 4+)
docker pull auto-rss:latest

# Docker 会自动选择对应架构的镜像
```

### Docker 网络配置

#### 与 qBittorrent 同网络

如果 qBittorrent 也运行在 Docker 中，可以使用相同网络：

```yaml
version: '3.8'

services:
  qbittorrent:
    image: linuxserver/qbittorrent:latest
    container_name: qbittorrent
    ports:
      - "8080:8080"
    networks:
      - media

  auto-rss:
    image: auto-rss:latest
    container_name: auto-rss
    ports:
      - "7892:7892"
    environment:
      # 使用容器名称作为 host
      - QB_HOST=http://qbittorrent:8080
      - QB_USERNAME=admin
      - QB_PASSWORD=yourpassword
    volumes:
      - ./data:/data
      # 共享下载目录
      - qb-downloads:/downloads
    networks:
      - media
    depends_on:
      - qbittorrent

networks:
  media:
    driver: bridge

volumes:
  qb-downloads:
```

---

## 二进制部署

### 下载二进制文件

访问 [Releases](https://github.com/WormW/auto-rss/releases) 页面下载对应平台的二进制文件。

**支持的平台**:
- `auto-rss-linux-amd64` - Linux (x86_64)
- `auto-rss-linux-arm64` - Linux (ARM64)
- `auto-rss-darwin-amd64` - macOS (Intel)
- `auto-rss-darwin-arm64` - macOS (Apple Silicon)
- `auto-rss-windows-amd64.exe` - Windows (x86_64)

### Linux / macOS 部署

```bash
# 1. 下载二进制
wget https://github.com/WormW/auto-rss/releases/download/v0.1.0/auto-rss-linux-amd64

# 2. 添加执行权限
chmod +x auto-rss-linux-amd64

# 3. 移动到系统目录 (可选)
sudo mv auto-rss-linux-amd64 /usr/local/bin/auto-rss

# 4. 创建工作目录
mkdir -p /opt/auto-rss/{data,downloads}
cd /opt/auto-rss

# 5. 创建配置文件
cat > .env <<EOF
DB_PATH=./data/auto-rss.db
QB_HOST=http://localhost:8080
QB_USERNAME=admin
QB_PASSWORD=yourpassword
RSS_INTERVAL=30m
LOG_LEVEL=info
SERVER_PORT=7892
DOWNLOAD_PATH=./downloads
EOF

# 6. 运行程序
auto-rss
```

### Windows 部署

```powershell
# 1. 下载 auto-rss-windows-amd64.exe

# 2. 创建工作目录
mkdir C:\auto-rss
cd C:\auto-rss
mkdir data
mkdir downloads

# 3. 创建配置文件 .env
@"
DB_PATH=./data/auto-rss.db
QB_HOST=http://localhost:8080
QB_USERNAME=admin
QB_PASSWORD=yourpassword
RSS_INTERVAL=30m
LOG_LEVEL=info
SERVER_PORT=7892
DOWNLOAD_PATH=./downloads
"@ | Out-File -FilePath .env -Encoding UTF8

# 4. 运行程序
.\auto-rss-windows-amd64.exe
```

### Systemd 服务 (Linux)

#### 创建服务文件

```bash
sudo nano /etc/systemd/system/auto-rss.service
```

```ini
[Unit]
Description=Auto-RSS Service
After=network.target

[Service]
Type=simple
User=youruser
Group=yourgroup
WorkingDirectory=/opt/auto-rss
ExecStart=/usr/local/bin/auto-rss
Restart=on-failure
RestartSec=10
EnvironmentFile=/opt/auto-rss/.env

# 安全加固
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/auto-rss/data /opt/auto-rss/downloads

[Install]
WantedBy=multi-user.target
```

#### 启动服务

```bash
# 重新加载 systemd
sudo systemctl daemon-reload

# 启动服务
sudo systemctl start auto-rss

# 开机自启
sudo systemctl enable auto-rss

# 查看状态
sudo systemctl status auto-rss

# 查看日志
sudo journalctl -u auto-rss -f

# 停止服务
sudo systemctl stop auto-rss

# 重启服务
sudo systemctl restart auto-rss
```

---

## 源码部署

### 环境准备

```bash
# 安装 Go 1.25+
wget https://golang.org/dl/go1.25.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# 安装 Node.js 18+
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt-get install -y nodejs

# 验证安装
go version
node --version
npm --version
```

### 克隆代码

```bash
git clone https://github.com/WormW/auto-rss.git
cd auto-rss
```

### 构建前端

```bash
cd web
npm install
npm run build
cd ..
```

### 构建后端

```bash
go mod download
go build -o auto-rss ./cmd/server
```

### 运行程序

```bash
# 创建配置文件
cp .env.example .env
# 编辑 .env 配置

# 运行程序
./auto-rss
```

### 开发模式

```bash
# 后端开发 (热重载需要安装 air)
go install github.com/cosmtrek/air@latest
air

# 前端开发
cd web
npm run dev
```

---

## 配置说明

### 访问保护

Auto-RSS 默认保持本地单用户/NAS 兼容模式：`AUTH_ENABLED=false` 时不要求登录。将服务暴露到家庭局域网以外，或希望限制同网段访问时，可以启用认证：

```env
AUTH_ENABLED=true
JWT_SECRET=change-this-to-a-long-random-secret-at-least-32-chars
JWT_USERNAME=admin
JWT_PASSWORD=change-this-password
```

启用认证时程序会拒绝默认 `JWT_SECRET=your-secret-key-change-in-production` 和默认 `JWT_PASSWORD=admin`，`JWT_SECRET` 至少需要 32 个字符。

### 环境变量

| 变量名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `DB_PATH` | string | `./data/auto-rss.db` | SQLite 数据库路径 |
| `QB_HOST` | string | `http://localhost:8080` | qBittorrent 地址 |
| `QB_USERNAME` | string | `admin` | qBittorrent 用户名 |
| `QB_PASSWORD` | string | `""` | qBittorrent 密码 |
| `RSS_INTERVAL` | string | `30m` | RSS 更新间隔 (5m, 15m, 30m, 1h 等) |
| `LOG_LEVEL` | string | `info` | 日志级别 (debug, info, warn, error) |
| `SERVER_PORT` | int | `7892` | Web 服务端口 |
| `DOWNLOAD_PATH` | string | `/downloads` | 默认下载路径 |

### 配置文件示例

**.env**:
```bash
# 数据库配置
DB_PATH=./data/auto-rss.db

# qBittorrent 配置
QB_HOST=http://192.168.1.100:8080
QB_USERNAME=admin
QB_PASSWORD=yourpassword

# RSS 配置
RSS_INTERVAL=30m

# 日志配置
LOG_LEVEL=info

# 服务配置
SERVER_PORT=7892
DOWNLOAD_PATH=/downloads
```

### 目录结构

```
/opt/auto-rss/              # 工作目录
├── .env                    # 环境变量配置
├── data/                   # 数据目录
│   └── auto-rss.db         # SQLite 数据库
├── downloads/              # 下载目录
│   └── anime/              # 番剧目录 (自动创建)
└── auto-rss                # 程序二进制 (可选)
```

---

## 常见问题

### 1. qBittorrent 连接失败

**问题**: API 返回 "qBittorrent 连接失败"

**解决方案**:
```bash
# 1. 检查 qBittorrent Web UI 是否启用
# 设置 → Web UI → 启用 Web 用户界面

# 2. 检查 qBittorrent 地址和端口
# 确保 QB_HOST 正确: http://ip:port

# 3. 检查用户名和密码
# 使用 qBittorrent Web UI 的登录凭据

# 4. 测试连接
curl -u admin:password http://192.168.1.100:8080/api/v2/app/version
```

### 2. 下载任务无法添加

**问题**: RSS 解析成功但无法添加下载

**解决方案**:
```bash
# 1. 检查 qBittorrent 是否运行
ps aux | grep qbittorrent

# 2. 检查下载路径是否存在
# qBittorrent 设置中配置的下载路径必须存在

# 3. 检查 qBittorrent 日志
# 工具 → 日志

# 4. 手动测试添加种子
# 在 qBittorrent Web UI 手动添加种子测试
```

### 3. 文件重命名失败

**问题**: 下载完成但文件未重命名

**解决方案**:
```bash
# 1. 检查重命名配置
# Web UI → 系统配置 → 重命名 → 是否启用

# 2. 检查目标目录权限
ls -la /downloads/anime/

# 3. 检查日志
# Web UI → 日志查看 → 搜索 "重命名"

# 4. 手动测试重命名逻辑
# 查看 qBittorrent 完成后的文件路径
```

### 4. RSS 解析无新内容

**问题**: 手动刷新 RSS 但无新下载任务

**解决方案**:
```bash
# 1. 检查 RSS URL 是否有效
curl "https://mikanani.me/RSS/Bangumi?bangumiId=3080"

# 2. 检查过滤规则
# 过滤关键词是否过于严格

# 3. 检查去重逻辑
# 查看日志是否有 "已存在" 提示

# 4. 清空下载历史 (谨慎)
# 删除数据库中的 downloads 表记录
```

### 5. Docker 容器无法访问 qBittorrent

**问题**: Docker 容器内无法连接到宿主机的 qBittorrent

**解决方案**:
```bash
# 1. 使用宿主机 IP 而非 localhost
# 错误: QB_HOST=http://localhost:8080
# 正确: QB_HOST=http://192.168.1.100:8080

# 2. 或使用 Docker 特殊地址 (Linux)
QB_HOST=http://host.docker.internal:8080

# 3. 或将 qBittorrent 也放入 Docker 网络
# 参考 Docker 部署章节的网络配置
```

### 6. 权限问题

**问题**: "Permission denied" 错误

**解决方案**:
```bash
# 1. 检查数据目录权限
sudo chown -R youruser:yourgroup /opt/auto-rss/data

# 2. 检查下载目录权限
sudo chown -R youruser:yourgroup /opt/auto-rss/downloads

# 3. Docker 容器权限
# 确保宿主机目录权限与容器用户一致
sudo chown -R 1000:1000 ./data ./downloads
```

---

## 升级指南

### Docker 升级

```bash
# 1. 停止容器
docker stop auto-rss

# 2. 备份数据
cp -r data data.backup

# 3. 拉取新镜像
docker pull auto-rss:latest

# 4. 删除旧容器
docker rm auto-rss

# 5. 启动新容器 (使用相同配置)
docker run -d \
  --name auto-rss \
  -p 7892:7892 \
  -e QB_HOST=http://192.168.1.100:8080 \
  -e QB_USERNAME=admin \
  -e QB_PASSWORD=yourpassword \
  -v $(pwd)/data:/data \
  -v $(pwd)/downloads:/downloads \
  auto-rss:latest
```

### 二进制升级

```bash
# 1. 停止服务
sudo systemctl stop auto-rss  # 或 Ctrl+C

# 2. 备份数据
cp -r /opt/auto-rss/data /opt/auto-rss/data.backup

# 3. 备份旧二进制
sudo mv /usr/local/bin/auto-rss /usr/local/bin/auto-rss.old

# 4. 下载新版本
wget https://github.com/WormW/auto-rss/releases/download/v0.2.0/auto-rss-linux-amd64

# 5. 安装新版本
chmod +x auto-rss-linux-amd64
sudo mv auto-rss-linux-amd64 /usr/local/bin/auto-rss

# 6. 启动服务
sudo systemctl start auto-rss

# 7. 验证版本
curl http://localhost:7892/api/v1/config | jq .version
```

### 数据库迁移

如果新版本有数据库架构变更，程序会自动迁移。

**手动迁移** (如果需要):
```bash
# 1. 备份数据库
cp data/auto-rss.db data/auto-rss.db.backup

# 2. 运行迁移 (程序启动时自动执行)
./auto-rss migrate

# 3. 如果迁移失败，恢复备份
cp data/auto-rss.db.backup data/auto-rss.db
```

---

## 性能优化

### 调整 RSS 更新间隔

```bash
# 高频更新 (5 分钟, 适合追新番)
RSS_INTERVAL=5m

# 标准更新 (30 分钟, 推荐)
RSS_INTERVAL=30m

# 低频更新 (1 小时, 适合老番)
RSS_INTERVAL=1h
```

### SQLite 优化

```bash
# 定期执行 VACUUM (清理碎片)
sqlite3 data/auto-rss.db "VACUUM;"

# 优化索引
sqlite3 data/auto-rss.db "ANALYZE;"
```

### 日志轮转

```bash
# 配置日志文件大小限制
# 修改 .env
LOG_MAX_SIZE=100M
LOG_MAX_BACKUPS=3
```

---

## 监控与维护

### 健康检查

```bash
# 检查服务状态
curl http://localhost:7892/api/v1/config

# 检查 qBittorrent 连接
curl http://localhost:7892/api/v1/downloads

# 检查日志
curl http://localhost:7892/api/v1/logs?level=ERROR
```

### 定期备份

```bash
#!/bin/bash
# backup.sh

# 备份数据库
DATE=$(date +%Y%m%d)
cp /opt/auto-rss/data/auto-rss.db /opt/backups/auto-rss-$DATE.db

# 保留最近 7 天的备份
find /opt/backups -name "auto-rss-*.db" -mtime +7 -delete
```

```bash
# 添加到 crontab (每天凌晨 3 点备份)
0 3 * * * /opt/auto-rss/backup.sh
```

---

## 安全建议

1. **修改默认密码**: 使用强密码保护 qBittorrent
2. **防火墙配置**: 限制 7892 端口访问来源
3. **定期更新**: 及时更新到最新版本
4. **数据备份**: 定期备份数据库文件
5. **日志审计**: 定期检查错误日志

---

## 卸载

### Docker 卸载

```bash
# 停止并删除容器
docker stop auto-rss
docker rm auto-rss

# 删除镜像
docker rmi auto-rss:latest

# 删除数据 (可选)
rm -rf data downloads
```

### 二进制卸载

```bash
# 停止服务
sudo systemctl stop auto-rss
sudo systemctl disable auto-rss

# 删除服务文件
sudo rm /etc/systemd/system/auto-rss.service
sudo systemctl daemon-reload

# 删除程序
sudo rm /usr/local/bin/auto-rss

# 删除数据 (可选)
rm -rf /opt/auto-rss
```

---

## 技术支持

- **GitHub Issues**: https://github.com/WormW/auto-rss/issues
- **文档**: https://docs.auto-rss.example.com
- **Telegram 群组**: https://t.me/auto_rss

---

**文档版本**: v1.0
**最后更新**: 2025-10-19
**维护者**: Auto-RSS Team
