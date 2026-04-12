# Auto-RSS 服务管理

## 快速开始

### 1. 构建项目

```bash
make build
```

构建产物将输出到项目根目录：`./auto-rss`

### 2. 安装为系统服务

```bash
make install-service
```

或手动运行脚本：

```bash
./scripts/install-service.sh
```

### 3. 管理服务

#### macOS (launchd)

```bash
# 启动服务
launchctl start com.wormw.auto-rss

# 停止服务
launchctl stop com.wormw.auto-rss

# 查看状态
launchctl list | grep auto-rss

# 查看日志
tail -f ~/service/auto-rss/logs/auto-rss.out.log
tail -f ~/service/auto-rss/logs/auto-rss.err.log
```

#### Linux (systemd)

```bash
# 启动服务
sudo systemctl start auto-rss

# 停止服务
sudo systemctl stop auto-rss

# 查看状态
sudo systemctl status auto-rss

# 查看日志
sudo journalctl -u auto-rss -f
```

### 4. 卸载服务

```bash
make uninstall-service
```

或：

```bash
./scripts/uninstall-service.sh
```

## 配置说明

### 环境变量

服务使用的环境变量在以下位置配置：

- **macOS**: `~/Library/LaunchAgents/com.wormw.auto-rss.plist`
- **Linux**: `/etc/systemd/system/auto-rss.service`

可配置项：

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `DB_PATH` | `./data/auto-rss.db` | 数据库文件路径 |
| `SERVER_PORT` | `7892` | HTTP 服务端口 |
| `LOG_LEVEL` | `info` | 日志级别 (debug/info/warn/error) |
| `DOWNLOAD_PATH` | `/downloads` | 下载文件保存路径 |
| `QB_HOST` | `http://localhost:8080` | qBittorrent 地址 |
| `QB_USERNAME` | `admin` | qBittorrent 用户名 |
| `QB_PASSWORD` | - | qBittorrent 密码 |
| `RSS_INTERVAL` | `30m` | RSS 检查间隔 |

### 修改配置后重启

**macOS:**
```bash
launchctl stop com.wormw.auto-rss
launchctl start com.wormw.auto-rss
```

**Linux:**
```bash
sudo systemctl restart auto-rss
```

## 文件说明

```
scripts/
├── auto-rss.plist          # macOS launchd 配置
├── auto-rss.service        # Linux systemd 配置
├── install-service.sh      # 服务安装脚本
├── uninstall-service.sh    # 服务卸载脚本
└── README.md               # 本文件
```

## 目录结构

```
~/service/auto-rss/
├── auto-rss                # 构建产物（二进制文件）
├── data/                   # 数据库目录
├── logs/                   # 日志目录
└── scripts/                # 服务脚本
```
