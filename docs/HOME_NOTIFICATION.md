# home-console 通知

Auto-RSS 可以把下载完成、下载失败、系统错误和日历事件推送到 home-console 的通知中心。

## 配置

在 `.env` 中设置：

```env
HOME_NOTIFICATION_ENABLED=true
HOME_NOTIFICATION_URL=https://home.ts.acg.cx
HOME_NOTIFICATION_TOKEN=<home-console 为 auto-rss source 分配的 token>
HOME_NOTIFICATION_PAGE_URL=https://home.ts.acg.cx/#notifications
```

`HOME_NOTIFICATION_URL` 可以填写 home-console 根地址，也可以直接填写完整的
`/api/notifications/ingest/auto-rss` 地址。服务会使用：

```http
POST /api/notifications/ingest/auto-rss
Authorization: Bearer <HOME_NOTIFICATION_TOKEN>
Content-Type: application/json
```

请求中的 `event_id` 使用 Auto-RSS 原始事件 ID。home-console 按 `(source_id,event_id)`
幂等，因此网络重试或服务重启不会因为同一事件产生重复通知。只有 2xx 响应会被视为成功；
网络错误、429 和 5xx 最多重试 3 次，401/403 等配置错误不会反复重试。

下载完成事件会在订阅已有 `BangumiCover` 远程地址时携带 `image_url`。仅发送公开的
HTTP(S) 地址；本地封面路径会被忽略，因此不会让 home-console 访问 Auto-RSS 的本地磁盘。

未开启该渠道时，现有 WebSocket、Telegram、Email 和通用 Webhook 通知不受影响。
