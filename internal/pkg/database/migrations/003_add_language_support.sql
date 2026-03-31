-- Migration: 003_add_language_support
-- Description: 添加语言支持相关字段
-- Created: 2026-04-01

-- 1. 为 subscriptions 表添加语言偏好字段
ALTER TABLE subscriptions ADD COLUMN language_preference VARCHAR(10) DEFAULT 'auto';

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_subscriptions_lang_pref ON subscriptions(language_preference);

-- 2. 为 downloads 表添加语言字段
ALTER TABLE downloads ADD COLUMN language VARCHAR(10) DEFAULT 'unknown';

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_downloads_language ON downloads(language);
CREATE INDEX IF NOT EXISTS idx_downloads_sub_ep_lang ON downloads(subscription_id, episode, language);

-- 3. 迁移现有数据：根据标题推断语言
-- 更新已有下载记录的语言字段（基于标题关键词）
UPDATE downloads SET language = 'chs' WHERE 
    LOWER(title) LIKE '%[chs]%' OR 
    LOWER(title) LIKE '%[sc]%' OR 
    LOWER(title) LIKE '%简体%' OR 
    LOWER(title) LIKE '%简中%' OR 
    LOWER(title) LIKE '%gb%';

UPDATE downloads SET language = 'cht' WHERE 
    LOWER(title) LIKE '%[cht]%' OR 
    LOWER(title) LIKE '%[tc]%' OR 
    LOWER(title) LIKE '%繁体%' OR 
    LOWER(title) LIKE '%繁中%' OR 
    LOWER(title) LIKE '%big5%';

-- 4. 更新订阅的默认语言偏好
-- 根据历史下载记录推断偏好
-- 注：这部分逻辑在应用启动时会通过代码自动处理
