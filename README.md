# Auto-RSS

<p align="center">
  <strong>基于 Go + Vue 的番剧 RSS 自动订阅、下载、整理工具</strong>
</p>

<p align="center">
  <a href="#功能状态">功能状态</a> •
  <a href="#快速开始">快速开始</a> •
  <a href="#配置说明">配置说明</a> •
  <a href="#文档">文档</a> •
  <a href="#开发">开发</a>
</p>

---

## 简介

Auto-RSS 面向 NAS、家庭服务器和个人媒体库场景，通过 RSS 订阅番剧更新，自动解析条目、推送到 qBittorrent、同步下载状态，并按媒体库友好的目录和文件名整理成片。项目当前使用 SQLite 持久化数据，后端提供 REST API，前端使用 Vue 3 管理订阅、下载、配置、通知、日历、磁盘和备份迁移。

---

## 功能状态

### 已支持

- **订阅与下载**
  - 创建、编辑、删除、启用/停用订阅。
  - 订阅预览、手动采集缺失剧集、合集种子下载、批量导入、导入/导出订阅。
  - RSS 定时刷新、手动全局刷新、增量水位线、种子 Hash 去重、同集替换。
  - qBittorrent Web API 集成，下载状态同步，失败诊断、手动重试、批量删除和清空。

- **番剧元数据与封面**
  - Mikan 搜索、季度查询、字幕组列表。
  - Bangumi 搜索、按名称匹配、条目详情、订阅元数据补全和后台更新。
  - Bangumi 封面下载、本地封面访问和缺失封面 fallback。

- **文件整理与恢复**
  - 自定义重命名模板、模板预设、重命名预览。
  - 下载完成后的目录整理和命名。
  - 手动重新整理、批量重命名、文件夹扫描、恢复扫描。
  - 订阅名称或季度变化后可触发相关文件重命名任务。

- **组织与筛选**
  - 字幕组、语言、总集数、集数偏移、包含/排除关键词和过滤规则。
  - 订阅分组、批量启用、批量删除、批量移动分组。
  - 标签创建、更新、删除和订阅标签关联。
  - 下载历史查询、下载统计和 RSS 健康检查 API 已发布，可用于 Web UI、脚本和后续集成验证。

- **通知、日历与磁盘**
  - 通知历史、Telegram、Email、Webhook、WebSocket 通知。
  - Webhook 预设模板，支持默认、Nanobot、OpenClaw、Discord、Slack 等格式。
  - 追番日历、本周/下周/今日排期。
  - 磁盘状态、阈值配置、低空间通知、危险状态暂停新下载。
  - 后台自动清理服务已实现。

- **认证、运维与迁移**
  - `AUTH_ENABLED=false` 默认兼容本地/NAS 部署。
  - `AUTH_ENABLED=true` 时启用单用户 JWT 登录，保护主要 `/api/v1` 业务接口。
  - 登录、刷新、退出登录和认证状态接口已挂载。
  - 启用认证时会拒绝默认 JWT secret 和默认 `admin` 密码。
  - WebSocket 通知会跟随认证开关校验 token。
  - REST API、健康检查、ready/live、Prometheus metrics、请求限流。
  - 配置备份导出、导入预览、导入迁移，支持敏感字段脱敏。

### 实验性或受限

- **磁盘手动清理**：`POST /api/v1/disk/cleanup` 已复用后台清理逻辑，按年龄或空间删除已完成下载记录和文件，并返回真实删除数量、释放字节数、清理前后可用空间和逐项失败原因；清理历史接口会返回持久化的清理摘要。
- **RSS 源适配**：多 RSS 源管理已支持，但具体条目解析能力仍主要围绕 Mikan 风格标题和种子链接。
- **备份导入**：支持 Auto-RSS 和 Auto-Bangumi 数据格式迁移；导入前建议先使用预览接口确认策略和差异。

### 计划中

- 多磁盘/分区监控和更多存储后端空间监控。
- 更多 RSS 站点适配和源级解析规则。
- PostgreSQL 或其他外部数据库支持。
- 更细粒度的多用户权限和审计能力。

---

## 快速开始

### Docker

```bash
mkdir -p data downloads

docker run -d \
  --name auto-rss \
  --restart unless-stopped \
  -p 7892:7892 \
  -e DB_PATH=/data/auto-rss.db \
  -e QB_HOST=http://192.168.1.100:8080 \
  -e QB_USERNAME=admin \
  -e QB_PASSWORD=your-qbittorrent-password \
  -e DOWNLOAD_PATH=/downloads \
  -v "$(pwd)/data:/data" \
  -v "$(pwd)/downloads:/downloads" \
  auto-rss:latest
```

启动后访问 `http://localhost:7892`。

### Docker Compose

```bash
cp docker-compose.yml docker-compose.local.yml
# 按需修改 qBittorrent 地址、账号、下载目录和 AUTH_ENABLED
docker compose -f docker-compose.local.yml up -d
```

### 二进制

```bash
cp .env.example .env
# 编辑 .env 中的 qBittorrent、下载路径和可选认证配置
./auto-rss
```

---

## 配置说明

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `DB_PATH` | `./data/auto-rss.db` | SQLite 数据库路径 |
| `QB_HOST` | `http://localhost:8080` | qBittorrent Web UI 地址 |
| `QB_USERNAME` | `admin` | qBittorrent 用户名 |
| `QB_PASSWORD` | 空 | qBittorrent 密码 |
| `RSS_INTERVAL` | `30m` | RSS 自动检查间隔 |
| `LOG_LEVEL` | `info` | 日志级别：`debug`、`info`、`warn`、`error` |
| `SERVER_PORT` | `7892` | Web 服务端口 |
| `MCP_ENABLED` | `false` | 是否启用 MCP Streamable HTTP 端点 `/mcp` |
| `MCP_TOKEN` | `""` | MCP Bearer token，启用 MCP 时必填 |
| `MCP_ALLOWED_ORIGINS` | `localhost/127.0.0.1` | 允许访问 `/mcp` 的浏览器 Origin，逗号分隔 |
| `DOWNLOAD_PATH` | `/downloads` | 默认下载路径 |
| `AUTH_ENABLED` | `false` | 是否启用单用户 JWT 访问保护 |
| `JWT_SECRET` | 默认占位值 | JWT 签名密钥；启用认证时至少 32 字符且不能使用默认值 |
| `JWT_USERNAME` | `admin` | 登录用户名 |
| `JWT_PASSWORD` | `admin` | 登录密码；启用认证时不能使用默认值 |
| `JWT_ACCESS_TOKEN_EXPIRY` | `30m` | access token 过期时间 |
| `JWT_REFRESH_TOKEN_EXPIRY` | `168h` | refresh token 过期时间 |
| `RATE_LIMIT_RPS` | `10` | 通用 API 每秒请求数 |
| `RATE_LIMIT_BURST` | `20` | 通用 API 突发请求数 |
| `RATE_LIMIT_AUTH_RPM` | `5` | 登录和刷新接口每分钟请求数 |
| `RATE_LIMIT_MAX_ENTRIES` | `10000` | 限流客户端缓存上限 |
| `RATE_LIMIT_TTL` | `1h` | 限流客户端不活跃清理时间 |

运行时配置也可通过 Web UI 或 `/api/v1/config` 写入数据库，例如 qBittorrent 连接、下载路径、代理、重命名模板、磁盘阈值和通知渠道。自动 RSS 下载会跳过 RSS enclosure 明确标注小于 `min_torrent_size_bytes` 的条目，默认值为 `52428800`（50 MiB），设置为 `0` 可关闭该保护；RSS 未提供大小时会继续按原流程处理。

详细配置说明请参考 [部署文档](docs/DEPLOY.md#配置说明)。MCP 使用说明请参考 [MCP 服务文档](docs/MCP.md)。

---

## 认证与部署安全

默认 `AUTH_ENABLED=false`，用于兼容已有内网部署。启用认证后：

- `/api/v1/auth/status`、`/login`、`/refresh`、`/logout` 保持公开。
- 健康检查、静态资源、封面和 metrics 不需要登录。
- 主要 `/api/v1` 业务接口需要 `Authorization: Bearer <access_token>`。
- `/ws/notifications` 需要携带有效 token。
- refresh token 采用轮换机制；退出登录只撤销提交的 refresh token。

如果服务不只暴露在可信内网，即使启用了应用认证，也建议配合防火墙、VPN 或反向代理访问控制。

---

## 文档

- [API 文档](docs/API.md)
- [部署文档](docs/DEPLOY.md)
- [Webhook 通知](docs/WEBHOOK_NOTIFICATION.md)
- [磁盘监控说明](docs/DISK_MONITOR_FEATURE.md)
- [追番日历说明](docs/CALENDAR_FEATURE.md)
- [字幕组功能设计](docs/FANSUB_DESIGN.md)
- [MCP 服务文档](docs/MCP.md)

---

## 开发

### 环境要求

- Go 1.25+
- Node.js 18+
- SQLite

### 构建

```bash
cd web
npm install
npm run build
cd ..

go mod download
go build -o auto-rss ./cmd/server
```

构建包含前端资源的单文件可执行：

```bash
make build-embed
```

### 测试

```bash
go test ./...

cd web
npm run build
```

---

## 技术栈

- 后端：Go、Gin、GORM、SQLite、zap、cron、gofeed
- 前端：Vue 3、TypeScript、Vite、Naive UI、Pinia、Vue Router
- 部署：Docker、Docker Compose、多架构二进制

---

## 致谢

- [auto_bangumi](https://github.com/EstrellaXD/Auto_Bangumi)
- [ani-rss](https://github.com/wushuo894/ani-rss)
- [Mikan Project](https://mikanani.me/)
- [Bangumi](https://bangumi.tv/)

---

## 许可证

本项目采用 MIT License。
