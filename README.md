# Auto-RSS

<p align="center">
  <img src="docs/images/logo.png" alt="Auto-RSS Logo" width="200"/>
</p>

<p align="center">
  <strong>基于 Golang 的番剧自动订阅下载工具</strong>
</p>

<p align="center">
  <a href="#特性">特性</a> •
  <a href="#快速开始">快速开始</a> •
  <a href="#文档">文档</a> •
  <a href="#开发">开发</a> •
  <a href="#贡献">贡献</a>
</p>

<p align="center">
  <img alt="GitHub release" src="https://img.shields.io/github/v/release/WormW/auto-rss">
  <img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/WormW/auto-rss">
  <img alt="License" src="https://img.shields.io/github/license/WormW/auto-rss">
</p>

---

## 📖 简介

Auto-RSS 是一个基于 Golang 开发的番剧自动订阅下载工具，灵感来源于 [auto_bangumi](https://github.com/EstrellaXD/Auto_Bangumi) 和 [ani-rss](https://github.com/wushuo894/ani-rss)。通过 RSS 订阅，自动解析、下载、重命名番剧，并与 Plex/Jellyfin 等媒体库无缝集成。

### 为什么选择 Auto-RSS？

- 🚀 **高性能**: Golang 实现，低资源占用，快速响应
- 🐳 **易部署**: Docker 一键启动，或使用单文件二进制
- 🎯 **智能化**: 自动解析 RSS、去重、重命名，全流程自动化
- 🎨 **现代化**: Vue 3 前端，清晰直观的 Web UI
- 📦 **轻量级**: SQLite 数据库，零配置依赖
- 🔧 **可扩展**: 预留接口，支持后续功能扩展

---

## ✨ 特性

### 核心功能 (v0.1.0)

- ✅ **RSS 订阅管理**
  - 创建、编辑、删除订阅
  - 订阅状态管理 (启用/暂停)
  - 手动/自动刷新 RSS

- ✅ **智能解析引擎**
  - Mikan Project RSS 解析
  - 自动提取番剧信息 (标题、集数、字幕组)
  - 智能去重 (种子 Hash + 标题相似度)

- ✅ **下载器集成**
  - qBittorrent API 集成
  - 自动添加下载任务
  - 任务状态同步

- ✅ **文件重命名**
  - 自动重命名为 `SxxExx` 格式
  - Plex/Jellyfin 兼容目录结构
  - 示例: `葬送的芙莉莲/Season 01/葬送的芙莉莲 S01E12.mp4`

- ✅ **Web UI**
  - 订阅管理页 (增删改查)
  - 下载监控页 (任务列表、进度)
  - 系统配置页 (qBittorrent、重命名规则)
  - 日志查看页 (错误追踪、调试)

- ✅ **配置管理**
  - 环境变量配置 (Docker 友好)
  - Web UI 动态配置 (运行时修改)
  - SQLite 数据持久化

### 计划功能

- 🔜 **番剧信息刮削** (v0.2.0)
  - TMDB / BGM 集成
  - 封面、简介、评分展示

- 🔜 **通知系统** (v0.2.0)
  - Telegram Bot 通知
  - 邮件 (SMTP) 通知

- 🔜 **高级功能** (v0.3.0+)
  - 字幕组搜索与匹配
  - 自定义重命名规则
  - 多 RSS 源支持 (dmhy, Nyaa)
  - PostgreSQL 支持

---

## 🚀 快速开始

### 使用 Docker (推荐)

```bash
# 1. 拉取镜像
docker pull auto-rss:latest

# 2. 创建目录
mkdir -p data downloads

# 3. 运行容器
docker run -d \
  --name auto-rss \
  -p 7892:7892 \
  -e QB_HOST=http://192.168.1.100:8080 \
  -e QB_USERNAME=admin \
  -e QB_PASSWORD=yourpassword \
  -v $(pwd)/data:/data \
  -v $(pwd)/downloads:/downloads \
  auto-rss:latest

# 4. 访问 Web UI
# 浏览器打开: http://localhost:7892
```

### 使用 Docker Compose

创建 `docker-compose.yml`:

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
      - QB_HOST=http://192.168.1.100:8080
      - QB_USERNAME=admin
      - QB_PASSWORD=yourpassword
      - RSS_INTERVAL=30m
      - LOG_LEVEL=info
    volumes:
      - ./data:/data
      - ./downloads:/downloads
```

启动服务:

```bash
docker-compose up -d
```

### 使用二进制

```bash
# 1. 下载二进制
wget https://github.com/WormW/auto-rss/releases/download/v0.1.0/auto-rss-linux-amd64

# 2. 添加执行权限
chmod +x auto-rss-linux-amd64

# 3. 创建配置文件
cat > .env <<EOF
QB_HOST=http://localhost:8080
QB_USERNAME=admin
QB_PASSWORD=yourpassword
EOF

# 4. 运行
./auto-rss-linux-amd64
```

---

## 📚 文档

完整文档请查看 [`docs/`](docs/) 目录：

- [📋 产品需求文档 (PRD)](docs/PRD.md) - 完整的功能需求和技术架构
- [📡 API 文档](docs/API.md) - RESTful API 接口说明
- [🚢 部署文档](docs/DEPLOY.md) - Docker、二进制、源码部署指南
- [👥 字幕组功能设计](docs/FANSUB_DESIGN.md) - 字幕组匹配功能详细设计

---

## 📋 系统要求

### 硬件要求

- **CPU**: 1 核 (推荐 2 核+)
- **内存**: 256 MB (推荐 512 MB+)
- **磁盘**: 100 MB (程序) + 下载空间

### 软件要求

**运行环境**:
- Docker 20.10+ (Docker 部署)
- 或 Linux / macOS / Windows (二进制部署)

**外部依赖**:
- qBittorrent 3.0+ (必须)
  - 需要开启 Web UI
  - 推荐版本: 4.5+

---

## 🛠️ 配置说明

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `DB_PATH` | `./data/auto-rss.db` | SQLite 数据库路径 |
| `QB_HOST` | `http://localhost:8080` | qBittorrent 地址 |
| `QB_USERNAME` | `admin` | qBittorrent 用户名 |
| `QB_PASSWORD` | `""` | qBittorrent 密码 |
| `RSS_INTERVAL` | `30m` | RSS 更新间隔 |
| `LOG_LEVEL` | `info` | 日志级别 (debug/info/warn/error) |
| `SERVER_PORT` | `7892` | Web 服务端口 |
| `DOWNLOAD_PATH` | `/downloads` | 默认下载路径 |

详细配置说明请参考 [部署文档](docs/DEPLOY.md#配置说明)。

---

## 💻 开发

### 环境准备

```bash
# Go 1.21+
go version

# Node.js 18+
node --version

# SQLite 3.40+
sqlite3 --version
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

### 运行开发环境

```bash
# 后端 (需要安装 air 实现热重载)
go install github.com/cosmtrek/air@latest
air

# 前端 (新终端)
cd web
npm run dev
```

### 运行测试

```bash
# 单元测试
go test ./...

# 带覆盖率
go test -cover ./...

# 生成覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 代码规范

```bash
# 代码格式化
gofmt -w .

# 代码检查
golangci-lint run

# 前端检查
cd web
npm run lint
```

---

## 🏗️ 技术栈

### 后端

- **语言**: Go 1.21+
- **框架**: [Gin](https://gin-gonic.com/) - Web 框架
- **ORM**: [GORM](https://gorm.io/) - 数据库 ORM
- **RSS**: [gofeed](https://github.com/mmcdole/gofeed) - RSS 解析
- **日志**: [zap](https://github.com/uber-go/zap) - 结构化日志
- **任务**: [cron](https://github.com/robfig/cron) - 定时任务
- **数据库**: SQLite

### 前端

- **框架**: [Vue 3](https://vuejs.org/)
- **语言**: TypeScript
- **构建**: [Vite](https://vitejs.dev/)
- **UI**: [Naive UI](https://www.naiveui.com/)
- **状态**: [Pinia](https://pinia.vuejs.org/)
- **路由**: Vue Router

### 部署

- **容器**: Docker + Docker Compose
- **多架构**: amd64, arm64

---

## 📸 截图

### 订阅管理
![订阅管理](docs/images/screenshot-subscription.png)

### 下载监控
![下载监控](docs/images/screenshot-download.png)

### 系统配置
![系统配置](docs/images/screenshot-config.png)

---

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

### 贡献指南

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'feat: 添加某个功能'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

### 提交规范

使用 [Conventional Commits](https://www.conventionalcommits.org/) 格式：

```
feat: 添加新功能
fix: 修复 bug
docs: 更新文档
style: 代码格式调整
refactor: 重构代码
test: 添加测试
chore: 构建/工具链更新
```

---

## 📄 许可证

本项目采用 [MIT License](LICENSE) 开源协议。

---

## 🙏 致谢

感谢以下优秀项目的启发和参考：

- [auto_bangumi](https://github.com/EstrellaXD/Auto_Bangumi) - 业务逻辑参考
- [ani-rss](https://github.com/wushuo894/ani-rss) - 架构设计参考
- [Mikan Project](https://mikanani.me/) - RSS 数据源

---

## 📞 联系方式

- **GitHub Issues**: [提交问题](https://github.com/WormW/auto-rss/issues)

---

## ⭐ Star History

[![Star History Chart](https://api.star-history.com/svg?repos=WormW/auto-rss&type=Date)](https://star-history.com/#WormW/auto-rss&Date)

---

<p align="center">
  Made with ❤️ by Auto-RSS Team
</p>
