# Auto-RSS API 文档

> 基础路径：`/api/v1`
> 默认地址：`http://localhost:7892`
> 数据格式：JSON

本文档按当前路由注册整理，面向需要调用 Auto-RSS REST API、调试 Web UI 请求或编写自动化脚本的用户。

---

## 当前状态

- `AUTH_ENABLED=false` 默认兼容本地/NAS 部署，业务 API 不要求登录。
- `AUTH_ENABLED=true` 时，除健康检查、静态资源、封面和 `/api/v1/auth/*` 外，主要 `/api/v1` 业务接口都需要 `Authorization: Bearer <access_token>`。
- `/ws/notifications` 跟随认证开关：认证关闭时允许匿名连接；认证开启时必须带 `?token=<access_token>`。
- `/metrics`、`/health`、`/ready`、`/live` 不走业务认证。
- 请求限流已启用，登录和刷新接口使用独立的认证端点限流配置。
- 部分旧 handler 仍返回 `{ "error": "..." }` 或 `{ "data": ... }`，多数新接口返回 `{ "code": 0, "message": "Success", "data": ... }`。调用方应同时按 HTTP 状态码和响应体判断结果。

---

## 通用响应

成功响应通常为：

```json
{
  "code": 0,
  "message": "Success",
  "data": {}
}
```

错误响应通常为：

```json
{
  "code": 400,
  "message": "Invalid request body"
}
```

列表接口常用 `page`、`page_size` 查询参数。时间字段通常使用 RFC3339 格式，例如 `2026-06-07T12:00:00Z`。

---

## 认证

### 配置

启用认证：

```env
AUTH_ENABLED=true
JWT_SECRET=change-this-to-a-long-random-secret-at-least-32-chars
JWT_USERNAME=admin
JWT_PASSWORD=change-this-password
JWT_ACCESS_TOKEN_EXPIRY=30m
JWT_REFRESH_TOKEN_EXPIRY=168h
```

启用认证时，服务启动会拒绝默认 `JWT_SECRET`、长度不足 32 字符的 secret、空用户名、空密码和默认 `admin` 密码。

### 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/auth/status` | 获取认证开关和当前用户名 |
| `POST` | `/auth/login` | 登录并获取 token pair |
| `POST` | `/auth/refresh` | 用 refresh token 换取新的 token pair |
| `POST` | `/auth/logout` | 撤销提交的 refresh token |

登录请求：

```json
{
  "username": "admin",
  "password": "change-this-password"
}
```

登录响应中的 `token_type` 为 `Bearer`。后续业务请求：

```http
Authorization: Bearer <access_token>
```

刷新请求：

```json
{
  "refresh_token": "<refresh_token>"
}
```

refresh token 采用轮换机制。已使用过的 refresh token 再次刷新会触发重用检测，并清理该用户已有 refresh token。

退出登录请求：

```json
{
  "refresh_token": "<refresh_token>"
}
```

---

## 健康检查、指标与静态资源

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 服务健康检查 |
| `GET` | `/ready` | 就绪检查 |
| `GET` | `/live` | 存活检查 |
| `GET` | `/api/v1/health` | API 路径下的健康检查 |
| `GET` | `/metrics` | Prometheus 指标 |
| `GET` | `/covers/*filepath` | 本地封面访问；本地缺失时按 Bangumi 原图 fallback |
| `GET` | `/ws/notifications` | 通知 WebSocket |
| `GET` | `/` | Web UI 入口 |
| `GET` | `/assets/*filepath` | Web UI 静态资源 |

WebSocket 连接：

```text
ws://localhost:7892/ws/notifications
ws://localhost:7892/ws/notifications?token=<access_token>
```

`AUTH_ENABLED=true` 时必须使用第二种形式。

---

## 路由总览

### Mikan 与 Bangumi

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/mikan/search` | 搜索 Mikan 番剧 |
| `GET` | `/mikan/season` | 按季度查询 Mikan 番剧 |
| `GET` | `/mikan/fansub-groups` | 查询番剧页面中的字幕组 RSS |
| `GET` | `/bangumi/search?keyword=...` | 搜索 Bangumi 动画条目 |
| `GET` | `/bangumi/search-by-name?name=...` | 按名称返回最佳匹配 |
| `GET` | `/bangumi/subjects/:id` | 获取 Bangumi 条目详情 |

这些接口会读取 `system_proxy` 配置并应用到外部请求。

### RSS 源

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/rss-sources` | 创建 RSS 源 |
| `GET` | `/rss-sources` | 获取 RSS 源列表 |
| `GET` | `/rss-sources/:id` | 获取单个 RSS 源 |
| `PUT` | `/rss-sources/:id` | 更新 RSS 源 |
| `DELETE` | `/rss-sources/:id` | 删除 RSS 源 |
| `GET` | `/rss-sources/:id/animes` | 拉取源内容并按番剧名聚合 |

创建请求：

```json
{
  "name": "Mikan",
  "base_url": "https://mikanani.me/RSS/Classic",
  "description": "Mikan RSS",
  "enabled": true
}
```

列表支持 `page`、`page_size`、`enabled` 查询参数。

### 订阅

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/subscriptions` | 创建订阅；会尝试自动补全 Bangumi 数据 |
| `GET` | `/subscriptions` | 获取订阅列表和下载统计 |
| `POST` | `/subscriptions/preview` | 预览 RSS 条目匹配、去重和命名结果 |
| `GET` | `/subscriptions/:id` | 获取订阅详情 |
| `PUT` | `/subscriptions/:id` | 更新订阅 |
| `DELETE` | `/subscriptions/:id` | 删除订阅 |
| `POST` | `/subscriptions/:id/toggle` | 切换订阅启用状态 |
| `POST` | `/subscriptions/:id/enrich-bangumi` | 强制补全 Bangumi 数据 |
| `POST` | `/subscriptions/:id/download-collection` | 手动触发合集种子下载 |
| `POST` | `/subscriptions/:id/collect-episodes` | 手动采集缺失剧集 |
| `POST` | `/subscriptions/:id/reorganize-files` | 重新整理订阅文件 |
| `POST` | `/subscriptions/:id/rename-files` | 批量重命名订阅文件 |
| `POST` | `/subscriptions/:id/scan-folder` | 扫描指定订阅文件夹并更新记录 |
| `POST` | `/subscriptions/batch-import-from-rss` | 从 RSS 批量导入订阅 |
| `POST` | `/subscriptions/batch/enable` | 批量启用或停用 |
| `POST` | `/subscriptions/batch/delete` | 批量删除 |
| `POST` | `/subscriptions/batch/group` | 批量设置分组 |
| `GET` | `/subscriptions/export` | 导出订阅 |
| `POST` | `/subscriptions/import` | 导入订阅 |
| `GET` | `/subscriptions/statistics` | 获取订阅统计 |

常用字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 番剧名称 |
| `rss_url` | string | RSS 地址 |
| `season` | int | 季度 |
| `enabled` | bool | 是否启用自动检查 |
| `rename_enabled` | bool | 是否启用重命名 |
| `fansub` | string | 字幕组 |
| `language` | string | 字幕语言 |
| `language_preference` | string | `auto`、`chs`、`cht`、`both` |
| `filter_keywords` | string | 包含关键词 |
| `exclude_keywords` | string | 排除关键词 |
| `filter_rules` | string | 支持 `include:`、`exclude:`、`+`、`-` 等规则前缀 |
| `total_episodes` | int | 总集数，0 表示未知 |
| `episode_offset` | int | RSS 集数偏移 |
| `collection_torrent` | string | 合集种子地址 |
| `bangumi_id` | int | Bangumi 条目 ID |
| `air_day` | string | 更新星期，`0` 表示周日 |
| `air_time` | string | 更新时间 |
| `notify_enabled` | bool | 是否开启排期通知 |

预览请求：

```json
{
  "name": "葬送的芙莉莲",
  "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
  "season": 1,
  "filter_rules": "include:1080p\nexclude:720p",
  "language_preference": "auto",
  "limit": 30
}
```

文件夹扫描请求：

```json
{
  "folder_path": "/downloads/葬送的芙莉莲",
  "dry_run": true,
  "rename_files": false
}
```

### 分组与标签

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/subscriptions/groups` | 获取订阅分组 |
| `POST` | `/subscriptions/groups` | 创建分组 |
| `GET` | `/subscriptions/groups/:id` | 获取分组 |
| `PUT` | `/subscriptions/groups/:id` | 更新分组 |
| `DELETE` | `/subscriptions/groups/:id` | 删除分组 |
| `GET` | `/tags` | 获取标签列表 |
| `POST` | `/tags` | 创建标签 |
| `PUT` | `/tags/:id` | 更新标签 |
| `DELETE` | `/tags/:id` | 删除标签 |
| `GET` | `/subscriptions/:id/tags` | 获取订阅标签 |
| `POST` | `/subscriptions/:id/tags` | 为订阅添加标签 |
| `DELETE` | `/subscriptions/:id/tags/:tag_id` | 从订阅移除标签 |

创建标签：

```json
{
  "name": "追更",
  "color": "#18a058",
  "description": "正在追的新番"
}
```

为订阅添加标签：

```json
{
  "tag_ids": [1, 2]
}
```

### 下载

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/downloads` | 获取下载列表 |
| `GET` | `/downloads/:id/diagnostics` | 获取失败诊断 |
| `GET` | `/downloads/:id` | 获取下载详情 |
| `DELETE` | `/downloads/:id` | 删除单个下载，并尽量删除 qBittorrent 种子 |
| `POST` | `/downloads/:id/retry` | 手动重试下载 |
| `POST` | `/downloads/batch-delete` | 批量删除下载 |
| `DELETE` | `/downloads/clear` | 清空下载记录，可按状态筛选 |
| `GET` | `/downloads/history` | 下载历史 |
| `GET` | `/downloads/statistics` | 下载统计 |

列表支持 `page`、`page_size`、`status` 查询参数。当前没有 `POST /api/v1/downloads` 手动创建下载任务路由；新增下载主要由 RSS 刷新、订阅采集和合集种子流程产生。

批量删除请求：

```json
{
  "ids": [1, 2, 3]
}
```

清空失败下载：

```http
DELETE /api/v1/downloads/clear?status=failed
```

### RSS 刷新与健康

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/rss/refresh` | 立即触发一次全局 RSS 检查 |
| `GET` | `/rss/health` | 检查全部订阅 RSS 健康 |
| `GET` | `/rss/health/:subscription_id` | 检查单个订阅 RSS 健康 |
| `GET` | `/rss/dead` | 获取疑似失效 RSS |
| `POST` | `/rss/health-check` | 手动触发 RSS 健康检查 |

当前没有单个订阅的 `/rss/refresh/:subscription_id` 路由。单订阅补剧请使用 `/subscriptions/:id/collect-episodes`。

### 配置

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/config` | 获取数据库中的运行时配置 |
| `PUT` | `/config` | 写入单个配置项 |
| `POST` | `/config/qbittorrent/test` | 测试 qBittorrent 连接 |
| `POST` | `/config/qbittorrent/save` | 保存 qBittorrent 配置 |
| `GET` | `/config/rename/presets` | 获取重命名模板预设和变量 |
| `GET` | `/config/rename/template` | 获取当前重命名模板 |
| `POST` | `/config/rename/template` | 保存重命名模板 |
| `POST` | `/config/rename/preview` | 预览重命名模板 |

写入配置：

```json
{
  "key": "download_path",
  "value": "/downloads"
}
```

常见配置键：

| 配置键 | 说明 |
|--------|------|
| `qbittorrent_host` | qBittorrent 地址 |
| `qbittorrent_username` | qBittorrent 用户名 |
| `qbittorrent_password` | qBittorrent 密码 |
| `download_path` | 下载根目录 |
| `system_proxy` | 外部请求代理 |
| `rename_template` | 重命名模板 |
| `disk.warning_threshold_gb` | 磁盘警告阈值 |
| `disk.critical_threshold_gb` | 磁盘危险阈值 |
| `disk.auto_cleanup_enabled` | 自动清理开关 |
| `disk.cleanup_strategy` | `age`、`space`、`hybrid` |
| `disk.cleanup_keep_days` | 按年龄清理的保留天数 |
| `disk.cleanup_keep_gb` | 按空间清理的目标剩余空间 |

### 日志与文件整理

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/logs` | 查询日志 |
| `POST` | `/logs/clear` | 清空日志 |
| `POST` | `/file-organizer/trigger` | 手动触发文件整理扫描 |
| `POST` | `/file-organizer/reload` | 重新加载文件整理配置 |

日志查询支持 `level`、`start_time`、`end_time`、`keyword`、`page`、`page_size`。

### 恢复扫描与任务

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/recovery/scan` | 扫描下载目录并生成或应用修复计划 |
| `GET` | `/tasks/current` | 获取当前后台任务 |
| `GET` | `/tasks/history` | 获取任务历史 |
| `POST` | `/tasks/cancel` | 取消当前任务 |

恢复扫描请求：

```json
{
  "dry_run": true,
  "subscription_id": 1
}
```

`subscription_id` 可省略，省略时扫描全部订阅。`dry_run=false` 会尝试写入修复结果。

### 通知

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/notifications` | 获取通知历史 |
| `GET` | `/notifications/settings` | 获取全部通知渠道配置 |
| `GET` | `/notifications/settings/:channel` | 获取单个渠道配置 |
| `PUT` | `/notifications/settings` | 新增或更新渠道配置 |
| `DELETE` | `/notifications/settings/:channel` | 删除渠道配置 |
| `POST` | `/notifications/test` | 测试通知渠道 |
| `GET` | `/notifications/websocket/status` | 获取 WebSocket 连接状态 |
| `GET` | `/notifications/webhook/templates` | 获取 Webhook 预设模板 |

支持渠道：`telegram`、`email`、`webhook` 和 `webhook.{name}`。

保存配置：

```json
{
  "channel": "webhook.openclaw",
  "enabled": true,
  "config": {
    "url": "https://example.com/webhook",
    "method": "POST",
    "headers": {
      "Content-Type": "application/json"
    },
    "body_template": "{\"title\":\"{{.Title}}\",\"message\":\"{{.Message}}\"}"
  }
}
```

通知事件包括下载完成、下载失败、RSS 更新、系统错误、磁盘警告、磁盘危险、磁盘恢复、自动清理、即将播出和新集发布。

### 日历

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/calendar` | 获取周排期 |
| `GET` | `/calendar/today` | 获取今日排期 |

`/calendar` 支持 `week` 查询参数，`0` 为本周，`1` 为下周，`-1` 为上周。

### 磁盘

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/disk/status` | 获取下载路径所在磁盘状态 |
| `GET` | `/disk/info` | 等同于 `/disk/status` |
| `GET` | `/disk/settings` | 获取磁盘清理和阈值设置 |
| `PUT` | `/disk/settings` | 更新磁盘设置 |
| `POST` | `/disk/cleanup` | 手动触发清理接口；当前返回简化结果 |
| `GET` | `/disk/history` | 清理历史；当前返回空列表 |

更新设置：

```json
{
  "enabled": false,
  "strategy": "hybrid",
  "retention_days": 30,
  "min_free_gb": 50,
  "warning_threshold_gb": 10,
  "critical_threshold_gb": 5
}
```

后台磁盘监控每 5 分钟检查一次下载路径。低于警告阈值会发送通知；低于危险阈值会发送通知并暂停新下载。自动清理服务会在危险状态且 `disk.auto_cleanup_enabled=true` 时执行。

### 配置备份与迁移

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/backup/export` | 导出完整备份包 |
| `POST` | `/backup/preview` | 预览导入差异 |
| `POST` | `/backup/import` | 执行导入 |

导出：

```http
GET /api/v1/backup/export?include_sensitive=false
```

默认导出订阅、RSS 源、分组、标签、重命名模板、系统配置和通知配置；密码、Token、通知密钥等敏感字段默认替换为 `__AUTO_RSS_REDACTED__`。

导入预览或执行导入：

```json
{
  "source_format": "auto-rss",
  "strategy": "skip",
  "data": {
    "app": "auto-rss",
    "schema_version": "1.0",
    "subscriptions": []
  }
}
```

`source_format` 支持 `auto`、`auto-rss`、`auto-bangumi`。`strategy` 支持 `skip`、`merge`、`overwrite`。脱敏字段在导入时始终跳过。

---

## 数据模型摘要

### Subscription

```typescript
interface Subscription {
  id: number
  name: string
  rss_url: string
  season: number
  enabled: boolean
  rename_enabled: boolean
  fansub: string
  language: string
  language_preference: string
  filter_keywords: string
  exclude_keywords: string
  filter_rules: string
  total_episodes: number
  current_episode: number
  latest_episode: number
  episode_offset: number
  bangumi_id: number
  bangumi_score: number
  bangumi_summary: string
  bangumi_cover: string
  bangumi_cover_local: string
  collection_torrent: string
  group_id?: number
  created_at: string
  updated_at: string
}
```

### Download

```typescript
interface Download {
  id: number
  subscription_id: number
  title: string
  episode: number
  fansub: string
  language: string
  torrent_url: string
  torrent_hash: string
  file_path: string
  renamed_path: string
  status: 'pending' | 'downloading' | 'stalled' | 'completed' | 'failed' | 'organizing'
  retry_count: number
  max_retries: number
  last_error: string
  created_at: string
  updated_at: string
}
```

### NotificationSetting

```typescript
interface NotificationSetting {
  id: number
  channel: string
  enabled: boolean
  config: string
  created_at: string
  updated_at: string
}
```

---

## 调用示例

创建订阅并预览：

```bash
curl -X POST http://localhost:7892/api/v1/subscriptions/preview \
  -H "Content-Type: application/json" \
  -d '{
    "name": "葬送的芙莉莲",
    "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
    "season": 1,
    "filter_rules": "include:1080p\nexclude:720p"
  }'
```

触发全局 RSS 刷新：

```bash
curl -X POST http://localhost:7892/api/v1/rss/refresh
```

查询失败下载并重试：

```bash
curl "http://localhost:7892/api/v1/downloads?status=failed"
curl -X POST http://localhost:7892/api/v1/downloads/12/retry
```

认证开启时登录并调用接口：

```bash
TOKEN=$(curl -s -X POST http://localhost:7892/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"change-this-password"}' \
  | jq -r '.data.access_token')

curl -H "Authorization: Bearer ${TOKEN}" http://localhost:7892/api/v1/subscriptions
```
