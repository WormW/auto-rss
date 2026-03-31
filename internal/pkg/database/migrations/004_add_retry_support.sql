-- Migration: 004_add_retry_support
-- Description: 添加下载重试相关字段
-- Created: 2026-04-01

-- 为 downloads 表添加重试相关字段
ALTER TABLE downloads ADD COLUMN retry_count INTEGER DEFAULT 0;
ALTER TABLE downloads ADD COLUMN max_retries INTEGER DEFAULT 5;
ALTER TABLE downloads ADD COLUMN next_retry_at DATETIME;
ALTER TABLE downloads ADD COLUMN last_error TEXT;
ALTER TABLE downloads ADD COLUMN retry_reason VARCHAR(50);

-- 创建索引优化重试查询
CREATE INDEX IF NOT EXISTS idx_downloads_retry ON downloads(status, retry_count, next_retry_at);
