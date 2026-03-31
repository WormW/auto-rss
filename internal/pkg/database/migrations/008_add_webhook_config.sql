-- Migration: 008_add_webhook_config
-- Description: 添加 Webhook 通知渠道配置支持
-- Created: 2026-04-01

-- Webhook 通知配置示例（通过 API 插入）
-- 这里提供几个常见配置的示例 SQL

-- Nanobot 配置示例
INSERT OR IGNORE INTO notification_settings (channel, enabled, config) VALUES (
  'webhook.nanobot',
  0,
  '{
    "name": "nanobot",
    "url": "http://localhost:8080/webhook",
    "method": "POST",
    "headers": {
      "Content-Type": "application/json"
    },
    "body_template": "{\\n  \\"source\\": \\"auto-rss\\",\\n  \\"event\\": \\"{{.Event}}\\",\\n  \\"title\\": \\"{{.Title}}\\",\\n  \\"message\\": \\"{{.Message}}\\",\\n  \\"timestamp\\": {{.Timestamp}},\\n  \\"data\\": {{.DataJSON}}\\n}",
    "content_type": "application/json",
    "retry_enabled": true,
    "timeout_sec": 30
  }'
);

-- OpenClaw 配置示例
INSERT OR IGNORE INTO notification_settings (channel, enabled, config) VALUES (
  'webhook.openclaw',
  0,
  '{
    "name": "openclaw",
    "url": "http://localhost:18789/hooks/auto-rss",
    "method": "POST",
    "headers": {
      "Content-Type": "application/json",
      "X-Openclaw-Token": "your-token-here"
    },
    "body_template": "{\\n  \\"session\\": \\"auto-rss-notifications\\",\\n  \\"message\\": \\"**{{.Title}}**\\n\\n{{.Message}}\\"\\n}",
    "content_type": "application/json",
    "retry_enabled": true,
    "timeout_sec": 30
  }'
);

-- Discord 配置示例
INSERT OR IGNORE INTO notification_settings (channel, enabled, config) VALUES (
  'webhook.discord',
  0,
  '{
    "name": "discord",
    "url": "https://discord.com/api/webhooks/xxx/yyy",
    "method": "POST",
    "headers": {
      "Content-Type": "application/json"
    },
    "body_template": "{\\n  \\"username\\": \\"Auto RSS\\",\\n  \\"embeds\\": [{\\n    \\"title\\": \\"{{.Title}}\\",\\n    \\"description\\": \\"{{.Message}}\\",\\n    \\"timestamp\\": \\"{{.Timestamp}}\\",\\n    \\"color\\": 3447003\\n  }]\\n}",
    "content_type": "application/json",
    "retry_enabled": true,
    "timeout_sec": 30
  }'
);

-- Slack 配置示例
INSERT OR IGNORE INTO notification_settings (channel, enabled, config) VALUES (
  'webhook.slack',
  0,
  '{
    "name": "slack",
    "url": "https://hooks.slack.com/services/xxx/yyy/zzz",
    "method": "POST",
    "headers": {
      "Content-Type": "application/json"
    },
    "body_template": "{\\n  \\"text\\": \\"*{{.Title}}*\\n{{.Message}}\\"\\n}",
    "content_type": "application/json",
    "retry_enabled": true,
    "timeout_sec": 30
  }'
);
