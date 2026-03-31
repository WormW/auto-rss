-- Migration: 005_add_disk_config
-- Description: 添加磁盘监控配置
-- Created: 2026-04-01

-- 插入磁盘监控配置
INSERT OR IGNORE INTO configs (key, value, description) VALUES 
('disk.warning_threshold_gb', '10', '磁盘空间警告阈值(GB)'),
('disk.critical_threshold_gb', '5', '磁盘空间危险阈值(GB)'),
('disk.auto_cleanup_enabled', 'false', '自动清理开关'),
('disk.cleanup_strategy', 'age', '清理策略: age/space/hybrid'),
('disk.cleanup_keep_days', '30', '保留天数(age策略)'),
('disk.cleanup_keep_gb', '50', '预留空间(GB)(space策略)'),
('disk.cleanup_protect_watching', 'true', '保护正在观看的番剧'),
('disk.pause_on_critical', 'true', '危险时暂停新下载');
