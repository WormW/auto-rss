# Auto-RSS 服务部署

当前版本仅提供 REST API、可选 MCP 和后台订阅任务，不提供网页管理界面。构建需要 Go 1.25+、C 编译器；无需 Node.js。

## 本地二进制

```bash
go build -o auto-rss ./cmd/server
cp .env.example .env
# 编辑配置后导入环境变量
set -a
. ./.env
set +a
./auto-rss
```

相对 `DB_PATH` 以进程启动目录为基准。服务不再为了寻找网页资源切换工作目录。后台服务管理器应设置稳定的工作目录，或使用绝对路径。

必要配置：

- `QB_HOST`、`QB_USERNAME`、`QB_PASSWORD`：可达的 qBittorrent Web API 及凭据。
- `DB_PATH`：建议放在本地 SSD 的业务数据库路径。
- `DOWNLOAD_PATH`：与 qBittorrent 保存路径一致的媒体根目录。
- `AUTH_ENABLED`、`JWT_SECRET`、`JWT_USERNAME`、`JWT_PASSWORD`：REST 认证，默认仅适用于可信内网的无认证模式。
- `MCP_ENABLED=true`、`MCP_TOKEN`：启用 AI 客户端管理入口。

## 容器

```bash
docker build -t wormw/auto-rss:service .
docker run -d --name auto-rss --restart unless-stopped \
  --env-file .env \
  -e DB_PATH=/app/data/auto-rss.db \
  -p 7892:7892 \
  -v "$(pwd)/data:/app/data" \
  -v /your/media/path:/downloads \
  wormw/auto-rss:service
```

容器内 `localhost` 指向本容器，需要将 `QB_HOST` 设置为下载器的可达地址。启用重命名/NFO 时，媒体挂载路径必须与下载器返回路径一致。只挂载业务需要的媒体目录。

仓库的 Compose 示例使用 `wormw/auto-rss:latest`；要运行本地改造后的代码，应先修改为上面的 `:service` 标签。仓库变更不会自动更新已运行容器或远端服务。

## 升级行为与回退

1. 停止旧进程后备份整个数据目录和配置，避免复制到不一致的 SQLite/WAL 组合。
2. 保存旧可执行文件或镜像引用。
3. 部署新构建，沿用业务数据和下载器配置。
4. 验证订阅、剧集台账、下载记录、API、MCP，以及一次真实下载完成后的整理。
5. 如需回退，停止新进程，恢复旧程序与对应的数据备份。

本次改造不会主动删除既有订阅、下载记录、日志或废弃配置。启动迁移沿用已有机制，没有新增删除数据的迁移。页面和 `/api/v1/file-organizer/reload` 已移除；旧的目录监听配置不会生效。

## 日志与磁盘

默认 `LOG_DB_ENABLED=false`，运行日志仅输出 JSON 到 stderr，由容器或服务管理器收集并轮转。标准输出/错误输出的文件也需要配置保留策略；关闭数据库日志不等于系统不写任何日志文件。

需要历史日志查询时设置 `LOG_DB_ENABLED=true`。数据库日志遵循 `LOG_LEVEL`，最低持久化级别为 `info`；每小时最多启动一轮维护，分批清除 30 天以前及超过 10,000 条的记录。维护间隔内允许短暂超过上限。该模式会额外产生数据库写入，繁忙环境优先使用 stderr 收集。

关闭数据库日志后，`GET /api/v1/logs` 和 MCP `list_logs` 只能看到既有数据库日志。不会自动清空历史日志，也不会自动 `VACUUM`。如历史数据库很大，应在停服备份后单独处理，不能把启动新版本当作磁盘空间已回收的证明。

不再运行媒体目录监听、目录扫描、磁盘容量监控或自动空间清理。下载器状态同步、业务数据库读写、元数据封面缓存，以及下载完成后的重命名/移动/NFO 仍可能产生 I/O。

## 验证与排障

```bash
curl --fail http://localhost:7892/live
curl --fail http://localhost:7892/ready
curl http://localhost:7892/health
curl http://localhost:7892/api/v1/subscriptions
```

- `/live`：进程存活。
- `/ready`：就绪/数据库连接；不代表下载器或实际下载成功。
- `/health`：综合依赖状态，查看响应体中的降级信息。
- `/api/v1/subscriptions`：业务查询；启用认证后需 Bearer token。
- `/mcp`：开启后先验证无 token 被拒绝，再通过 MCP 客户端执行查询和创建。
- `/`：预期返回 JSON 404。

下载器连接失败时，核查 qBittorrent Web API 地址与账号。整理失败时，核查订阅的 `rename_enabled`、保存路径映射和 NFO 目录写权限；保留错误记录并通过下载重试/重新整理接口处理。诊断与管理 API 详见 [API 文档](API.md)。
