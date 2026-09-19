# Auto-RSS

面向 API 和 MCP 的番剧 RSS 订阅、下载与完成后整理服务。

服务负责持续抓取 RSS、维护订阅和剧集台账、向 qBittorrent 提交任务、同步下载状态，并在下载完成后按订阅配置重命名、移动、生成 NFO。管理入口是 REST API 和可选的 MCP；不再提供管理页面或依赖 Node.js 构建。

## 服务边界

- 保留多 feed 订阅、字幕组和语言过滤、集数偏移、基线、去重、候选资源与人工替换。
- 保留搜索、批量导入、分组、标签、剧集状态、下载历史、通知、日历、备份和迁移 API。
- 保留下载完成后的重命名、移动、NFO 生成和媒体库刷新；由 `rename_enabled` 等现有配置控制。
- 移除目录监听及其递归目录遍历；不进行媒体扫描、磁盘容量监控或自动空间清理。
- qBittorrent 状态同步仍每 30 秒运行，用于识别下载完成和失败。这是下载任务同步，不遍历媒体目录。
- 日志默认仅以 JSON 输出到 stderr。`LOG_DB_ENABLED=true` 可恢复数据库日志查询；每轮维护分批清理过期与超额日志，目标保留最近 10,000 条、最长 30 天，两轮之间可能暂时超额。

下载完成后的整理仍会通过 qBittorrent 操作文件，并访问配置路径写入 NFO。启用这项能力时，服务必须能够访问相应媒体路径。

## 快速开始

需要 Go 1.25+ 和用于 SQLite 的 C 编译器。构建不需要前端资源：

```bash
go build -o auto-rss ./cmd/server
cp .env.example .env
# 编辑 qBittorrent、下载路径和认证配置，然后将配置导入进程环境
set -a
. ./.env
set +a
./auto-rss
```

服务启动后验证：

```bash
curl --fail http://localhost:7892/live
curl --fail http://localhost:7892/ready
curl http://localhost:7892/api/v1/subscriptions
```

最后一个请求适用于默认的本地无认证模式；启用认证后添加 `Authorization: Bearer <access_token>`。根路径 `/` 返回 JSON 404，不提供管理页面。

### Docker

```bash
docker build -t wormw/auto-rss:service .
docker run -d --name auto-rss --restart unless-stopped \
  -p 7892:7892 \
  --env-file .env \
  -e DB_PATH=/app/data/auto-rss.db \
  -v "$(pwd)/data:/app/data" \
  -v /your/media/path:/downloads \
  wormw/auto-rss:service
```

容器内的 `QB_HOST` 需要指向可达的 qBittorrent 地址；`DOWNLOAD_PATH` 和挂载路径应与 qBittorrent 返回的路径保持一致。Compose 示例见 [docker-compose.yml](docker-compose.yml)，运行前将镜像标签改为本次构建的标签。

## 管理入口

### REST API

[API 文档](docs/API.md) 列出新增、编辑、多 feed、分组、批量启停、导入导出、下载重试和剧集管理接口。所有操作都可由脚本或其他客户端调用。

新增订阅和 feed 会先校验 RSS，事务性创建订阅及剧集台账。首次自动同步建立历史基线，不自动补下全部历史内容；补集使用手动采集接口。

### MCP

```env
MCP_ENABLED=true
MCP_TOKEN=replace-with-a-long-random-token
```

客户端连接 `http://localhost:7892/mcp`，通过 Bearer token 认证。支持搜索、新建、启停订阅、查询和重试下载等工具，详见 [MCP 文档](docs/MCP.md)。MCP 新增订阅与 REST 共用创建流程。

目前编辑 feed、批量管理、导入导出等完整管理能力通过 REST 提供；MCP 的工具范围见文档，尚未覆盖所有 REST 操作。后台调度独立运行，不依赖 AI 对话保持在线。

## 配置

| 变量 | 默认值 | 用途 |
|---|---|---|
| `DB_PATH` | `./data/auto-rss.db` | 业务数据库 |
| `QB_HOST` | `http://localhost:8080` | qBittorrent Web API |
| `QB_USERNAME` / `QB_PASSWORD` | `admin` / 空 | 下载器认证 |
| `DOWNLOAD_PATH` | `/downloads` | 下载根路径 |
| `RSS_INTERVAL` | `30m` | 自动抓取间隔 |
| `BANGUMI_UPDATE_INTERVAL` | `6` | 元数据更新间隔（小时），`0` 禁用 |
| `SERVER_PORT` | `7892` | HTTP 端口 |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_DB_ENABLED` | `false` | 是否额外把日志写入 SQLite |
| `MCP_ENABLED` | `false` | 是否启用 `/mcp` |
| `MCP_TOKEN` | 空 | MCP 独立 Bearer token |
| `AUTH_ENABLED` | `false` | REST 单用户 JWT 认证 |

认证和限流的完整配置见 [.env.example](.env.example)。启用 REST 认证时必须设置用户名、非默认密码和至少 32 字符的非默认 `JWT_SECRET`；MCP 始终独立校验 `MCP_TOKEN`。无认证模式仅适合可信内网。

运行时业务配置通过 `/api/v1/config` 存入数据库；下载路径和 qBittorrent 等已有数据库配置会覆盖对应启动配置。已废弃的 `file_organizer_enabled`、`file_organizer_dir` 及 `FILE_ORGANIZER_*` 环境变量不再启动目录监听。

## 验证与文档

```bash
go test ./...
go vet ./...
```

- [部署与升级](docs/DEPLOY.md)
- [服务化改造说明](docs/SERVICE_REFACTOR.md)
- [REST API](docs/API.md)
- [MCP](docs/MCP.md)
- [Webhook 通知](docs/WEBHOOK_NOTIFICATION.md)

`docs/superpowers/` 和原 PRD 保留历史设计记录；当前服务范围以本 README、API 文档和服务化说明为准。
