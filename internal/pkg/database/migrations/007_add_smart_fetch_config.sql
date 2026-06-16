-- Migration: 007_add_smart_fetch_config
-- Description: 添加智能拉取策略配置
-- Created: 2026-04-01

-- 智能拉取策略配置
INSERT OR IGNORE INTO configs (key, value, description) VALUES 
('smart_fetch.enabled', 'true', '启用智能拉取策略'),
('smart_fetch.before_air_day', '1', '更新日前N天开始拉取'),
('smart_fetch.after_air_day', '2', '更新日后N天继续拉取'),
('smart_fetch.skip_completed', 'false', '是否跳过已完结的订阅'),
('smart_fetch.completed_stop_days', '30', '完结后N天停止常规检查，0表示不停止'),
('smart_fetch.check_local_complete', 'true', '是否检查本地完整性');
