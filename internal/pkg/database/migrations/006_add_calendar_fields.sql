-- Migration: 006_add_calendar_fields
-- Description: 添加追番日历相关字段
-- Created: 2026-04-01

-- 为 subscriptions 表添加追番日历字段
ALTER TABLE subscriptions ADD COLUMN air_day VARCHAR(10);
ALTER TABLE subscriptions ADD COLUMN air_time VARCHAR(10);
ALTER TABLE subscriptions ADD COLUMN air_timezone VARCHAR(10) DEFAULT 'JST';
ALTER TABLE subscriptions ADD COLUMN notify_enabled BOOLEAN DEFAULT 1;
ALTER TABLE subscriptions ADD COLUMN notify_before_min INTEGER DEFAULT 10;

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_subscriptions_air_day ON subscriptions(air_day);
CREATE INDEX IF NOT EXISTS idx_subscriptions_notify ON subscriptions(notify_enabled);
