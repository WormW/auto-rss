# Webhook 通知渠道

## 功能概述

通用 Webhook 通知渠道，支持将 Auto-RSS 的通知发送到任何支持 HTTP 接收的服务，包括 Nanobot、OpenClaw、Discord、Slack 等。

## 核心特性

- **自定义 URL**：支持任意 HTTP/HTTPS 端点
- **自定义 Headers**：支持认证、Content-Type 等
- **模板引擎**：使用 Go template 自定义消息格式
- **HMAC 签名**：支持请求签名验证（可选）
- **自动重试**：失败时自动重试 3 次

## 配置结构

```json
{
  "name": "nanobot",                    // 渠道名称（如 nanobot、openclaw）
  "url": "http://localhost:8080/webhook", // Webhook URL
  "method": "POST",                      // HTTP 方法：POST/PUT/PATCH
  "headers": {                           // 自定义 Headers
    "Content-Type": "application/json",
    "Authorization": "Bearer xxx",
    "X-Custom-Header": "value"
  },
  "body_template": "...",                // 消息模板（Go template）
  "content_type": "application/json",    // Content-Type
  "secret": "",                          // HMAC 签名密钥（可选）
  "retry_enabled": true,                 // 是否启用重试
  "timeout_sec": 30                      // 超时时间（秒）
}
```

## 模板变量

| 变量 | 类型 | 说明 |
|-----|------|------|
| `{{.Event}}` | string | 事件类型（如 download.complete） |
| `{{.Title}}` | string | 通知标题 |
| `{{.Message}}` | string | 通知内容 |
| `{{.Data}}` | map | 事件数据 |
| `{{.DataJSON}}` | string | Data 的 JSON 字符串 |
| `{{.Timestamp}}` | int64 | Unix 时间戳 |
| `{{.EventID}}` | string | 事件唯一 ID |

## 预定义模板

### 1. 默认模板
```json
{
  "event": "{{.Event}}",
  "title": "{{.Title}}",
  "message": "{{.Message}}",
  "timestamp": {{.Timestamp}},
  "event_id": "{{.EventID}}",
  "data": {{.DataJSON}}
}
```

### 2. Nanobot 模板
```json
{
  "source": "auto-rss",
  "event": "{{.Event}}",
  "title": "{{.Title}}",
  "message": "{{.Message}}",
  "timestamp": {{.Timestamp}},
  "data": {{.DataJSON}}
}
```

### 3. OpenClaw 模板
```json
{
  "session": "auto-rss-notifications",
  "message": "**{{.Title}}**\n\n{{.Message}}"
}
```

### 4. Discord 模板
```json
{
  "username": "Auto RSS",
  "embeds": [{
    "title": "{{.Title}}",
    "description": "{{.Message}}",
    "timestamp": "{{.Timestamp}}",
    "color": 3447003
  }]
}
```

### 5. Slack 模板
```json
{
  "text": "*{{.Title}}*\n{{.Message}}"
}
```

## API 接口

### 获取 Webhook 模板
```bash
GET /api/v1/notifications/webhook/templates
```

### 创建 Webhook 配置
```bash
PUT /api/v1/notifications/settings
{
  "channel": "webhook.nanobot",
  "enabled": true,
  "config": {
    "name": "nanobot",
    "url": "http://localhost:8080/webhook",
    "method": "POST",
    "headers": {
      "Content-Type": "application/json"
    },
    "body_template": "{\"source\": \"auto-rss\", \"title\": \"{{.Title}}\", \"message\": \"{{.Message}}\"}",
    "retry_enabled": true
  }
}
```

### 测试 Webhook
```bash
POST /api/v1/notifications/test
{
  "channel": "webhook.nanobot",
  "config": {
    "name": "nanobot",
    "url": "http://localhost:8080/webhook",
    ...
  }
}
```

## 配置示例

### Nanobot 集成

```bash
# 1. 获取 Nanobot Gateway 地址（默认 http://localhost:8080）
# 2. 配置 Webhook

curl -X PUT http://auto-rss:7892/api/v1/notifications/settings \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "webhook.nanobot",
    "enabled": true,
    "config": {
      "name": "nanobot",
      "url": "http://localhost:8080/webhook",
      "method": "POST",
      "headers": {
        "Content-Type": "application/json"
      },
      "body_template": "{\n  \"source\": \"auto-rss\",\n  \"event\": \"{{.Event}}\",\n  \"title\": \"{{.Title}}\",\n  \"message\": \"{{.Message}}\",\n  \"data\": {{.DataJSON}}\n}"
    }
  }'
```

### OpenClaw 集成

```bash
# 1. 在 ~/.openclaw/openclaw.json 启用 hooks
# {
#   "hooks": {
#     "enabled": true,
#     "token": "your-secret-token",
#     "path": "/hooks"
#   }
# }

# 2. 配置 Webhook
curl -X PUT http://auto-rss:7892/api/v1/notifications/settings \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "webhook.openclaw",
    "enabled": true,
    "config": {
      "name": "openclaw",
      "url": "http://localhost:18789/hooks/auto-rss",
      "method": "POST",
      "headers": {
        "Content-Type": "application/json",
        "X-Openclaw-Token": "your-secret-token"
      },
      "body_template": "{\n  \"session\": \"auto-rss-notifications\",\n  \"message\": \"**{{.Title}}**\\n\\n{{.Message}}\"\n}"
    }
  }'
```

### Discord Webhook

```bash
curl -X PUT http://auto-rss:7892/api/v1/notifications/settings \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "webhook.discord",
    "enabled": true,
    "config": {
      "name": "discord",
      "url": "https://discord.com/api/webhooks/xxx/yyy",
      "method": "POST",
      "headers": {
        "Content-Type": "application/json"
      },
      "body_template": "{{.Template.discord}}"
    }
  }'
```

## 消息流转

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  Auto-RSS 事件   │────→│  Webhook Channel │────→│  Nanobot Gateway │
│  (下载完成等)    │     │                  │     │                 │
└─────────────────┘     │  - 模板渲染       │     └─────────────────┘
                        │  - 签名计算       │              │
                        │  - HTTP 发送      │              ↓
                        └──────────────────┘     ┌─────────────────┐
                                                   │  Telegram/Discord │
                                                   │  Slack/Email      │
                                                   └─────────────────┘
```

## 安全考虑

### HMAC 签名
如果配置了 `secret`，请求会包含 `X-Webhook-Signature` 头部：

```
X-Webhook-Signature: sha256=<hex_encoded_hmac>
```

接收方验证：
```python
import hmac
import hashlib

expected = hmac.new(secret, request_body, hashlib.sha256).hexdigest()
if not hmac.compare_digest(expected, signature):
    raise Unauthorized()
```

### 网络隔离
- 建议将 Nanobot/OpenClaw 与 Auto-RSS 部署在同一内网
- 使用防火墙限制 Webhook 端口访问
- 生产环境使用 HTTPS + Token 认证

## 故障排查

| 问题 | 可能原因 | 解决方案 |
|-----|---------|---------|
| Webhook 发送失败 | Gateway 未启动 | 检查 Nanobot/OpenClaw 是否运行 |
| 签名验证失败 | Secret 不匹配 | 检查双方配置的密钥 |
| 格式错误 | 模板语法错误 | 使用预定义模板测试 |
| 超时 | 网络延迟 | 增加 timeout_sec |

## 日志

```
DEBUG Webhook sent successfully
    channel=webhook.nanobot
    url=http://localhost:8080/webhook
    status=200

WARN Webhook send failed, will retry
    attempt=1
    max_attempts=3
    channel=webhook.nanobot
    error="connection refused"
```
