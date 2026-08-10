# Auto-RSS 产品需求文档 (PRD)

> **项目定位**: 基于 RSS 的番剧自动订阅下载工具 (Golang 实现)
> **版本**: v0.1.0 MVP
> **创建日期**: 2025-10-19
> **参考项目**: auto_bangumi (业务逻辑), ani-rss (架构参考)

---

## 📋 目录

1. [项目概述](#项目概述)
2. [功能需求](#功能需求)
3. [技术架构](#技术架构)
4. [数据模型](#数据模型)
5. [API 设计](#api-设计)
6. [UI 设计](#ui-设计)
7. [部署方案](#部署方案)
8. [开发计划](#开发计划)
9. [质量标准](#质量标准)
10. [后续版本规划](#后续版本规划)

---

## 项目概述

### 项目背景

Auto-RSS 是一个基于 Golang 开发的番剧自动订阅下载工具，旨在提供与 auto_bangumi 类似的功能体验，同时具备更好的性能和部署便捷性。

### 核心价值

- ✅ **全自动追番**: RSS 订阅 → 解析 → 下载 → 重命名，全流程自动化
- ✅ **零配置部署**: Docker 一键启动，或使用单文件二进制部署
- ✅ **媒体库友好**: 自动重命名为 Plex/Jellyfin 兼容格式
- ✅ **轻量高效**: Golang 实现，低资源占用，快速响应

### 目标用户

- 🎯 **个人用户**: NAS/家庭服务器用户，需要自动追番
- 🎯 **小团队**: 共享媒体库的小型团队
- 🎯 **技术爱好者**: 追求高性能、可定制化的用户

---

## 功能需求

### P0 核心功能 (v0.1.0 MVP)

#### 1. RSS 订阅管理
- **订阅 CRUD**: 创建、读取、更新、删除订阅
- **订阅配置**:
  - RSS URL (Mikan Project)
  - 番剧名称 (自定义)
  - 季度 (默认 01)
  - 订阅状态 (启用/暂停)
  - 过滤规则 (简单关键词匹配)
- **手动操作**:
  - 手动刷新单个订阅
  - 手动刷新所有订阅
  - 批量启用/暂停

#### 2. RSS 解析引擎
- **数据源**: Mikan Project (蜜柑计划)
- **解析内容**:
  - 番剧标题
  - 集数信息
  - 种子下载链接
  - 发布时间
  - 字幕组信息
    - 字幕组名称 (从 RSS 标题提取)
    - 字幕组 ID (subgroupid, 可选)
      - 支持手动补充 (Web UI 配置)
      - 支持搜索匹配 (预留接口, 后续实现)
      - 用途: Mikan 网站字幕组筛选 (v0.3.0+ 采集功能)
- **定时任务**:
  - 默认间隔: 30 分钟
  - 可配置范围: 5 分钟 - 24 小时
- **智能去重**:
  - 基于种子 Hash (优先)
  - 基于标题相似度 (fallback)
  - 下载历史记录防重

#### 3. 下载器集成
- **支持下载器**: qBittorrent
- **核心功能**:
  - 添加种子任务到 qBittorrent
  - 设置下载分类
  - 设置保存路径
  - 任务状态同步
- **连接管理**:
  - 支持认证 (用户名/密码)
  - 连接状态检测
  - 连接失败告警

#### 4. 文件重命名
- **重命名规则**:
  - 目录结构: `{番剧名}/Season {season}/`
  - 文件命名: `{番剧名} S{season}E{episode}.{ext}`
  - 示例: `葬送的芙莉莲/Season 01/葬送的芙莉莲 S01E12.mp4`
- **自动提取**:
  - 番剧名 (从订阅配置)
  - 季度 (从订阅配置)
  - 集数 (正则表达式提取)
  - 文件扩展名 (保留原始)
- **配置选项**:
  - 是否启用重命名
  - 目标根目录路径

#### 5. 数据持久化
- **数据库**: SQLite
- **核心数据表**:
  - 订阅表 (subscriptions)
  - 下载记录表 (downloads)
  - 系统配置表 (configs)
  - 日志表 (logs, 可选)

#### 6. Web UI
- **订阅管理页**:
  - 订阅列表 (表格)
  - 添加/编辑订阅 (弹窗表单)
  - 订阅状态管理
  - 手动刷新按钮
- **下载管理页**:
  - 下载任务列表
  - 任务状态 (下载中/完成/失败)
  - 任务进度展示
  - 下载历史记录
- **系统配置页**:
  - qBittorrent 连接配置
  - 重命名规则配置
  - RSS 更新频率配置
  - 存储路径配置
- **日志查看页**:
  - 日志列表 (分页)
  - 日志级别过滤
  - 时间范围筛选
  - 日志搜索

#### 7. 配置管理
- **配置层级**:
  1. 默认配置 (代码内嵌)
  2. 环境变量 (Docker 部署优先)
  3. Web UI 动态配置 (运行时修改，存数据库)
- **环境变量示例**:
  ```bash
  DB_PATH=/data/auto-rss.db
  QB_HOST=http://localhost:8080
  QB_USERNAME=admin
  QB_PASSWORD=adminpass
  RSS_INTERVAL=30m
  LOG_LEVEL=info
  ```

#### 8. 错误处理与日志
- **错误处理**:
  - RSS 解析失败: 记录日志，不中断其他订阅
  - qBittorrent 连接失败: 告警，暂停自动任务
  - 重命名失败: 保留原文件，记录错误，支持重试
- **日志系统**:
  - 日志级别: DEBUG, INFO, WARN, ERROR
  - 输出方式: 控制台 + 文件 (可选)
  - 日志格式: 结构化 JSON
  - 日志存储: SQLite (最近 1000 条) + 文件轮转
  - 日志查询: Web UI 查看，支持过滤和搜索

---

### P1 重要功能 (v0.2.0)

#### 1. 番剧信息刮削
- **数据源**: TMDB / Bangumi (BGM.TV)
- **刮削内容**:
  - 番剧封面
  - 番剧简介
  - 评分信息
  - 播出信息
- **应用场景**:
  - Web UI 展示番剧信息
  - 媒体库元数据补充

#### 2. 智能季度识别
- **自动识别**:
  - 从 RSS 标题提取季度信息
  - 关键词匹配 (第二季、Season 2 等)
- **手动调整**:
  - Web UI 手动设置季度

#### 3. 通知系统
- **通知渠道**:
  - Telegram Bot
  - 邮件 (SMTP)
- **通知场景**:
  - 新番剧下载成功
  - 下载失败告警
  - qBittorrent 连接异常

#### 4. 自定义重命名规则
- **模板变量**:
  - `{title}`: 番剧名
  - `{season}`: 季度
  - `{episode}`: 集数
  - `{fansub}`: 字幕组
  - `{resolution}`: 分辨率
  - `{ext}`: 文件扩展名
- **规则引擎**:
  - 正则表达式提取
  - 可视化规则测试

---

### P2 扩展功能 (后续版本)

- ❌ 多用户支持 (v0.4.0)
- ❌ OpenAI 集成 (v0.5.0)
- ❌ 高级搜索功能 (v0.3.0)
- ❌ RSS 聚合支持 (v0.3.0)

---

## 技术架构

### 技术选型

#### 后端技术栈
```yaml
语言: Go 1.25+
框架与库:
  - Web框架: Gin (轻量高性能)
  - ORM: GORM v2 (SQLite 支持)
  - RSS解析: gofeed (成熟稳定)
  - qBittorrent客户端: go-qbittorrent
  - 配置管理: viper (环境变量 + 文件)
  - 日志: zap (结构化日志、高性能)
  - 定时任务: robfig/cron v3
  - HTTP客户端: resty (简化 API 调用)
  - 依赖注入: wire (Google 官方、编译时注入)
```

#### 前端技术栈
```yaml
框架: Vue 3
语言: TypeScript
构建工具: Vite
UI组件库: Naive UI
状态管理: Pinia
HTTP客户端: Axios
路由: Vue Router
```

#### 数据库
```yaml
默认: SQLite (零配置、轻量级)
可选: PostgreSQL (v0.3.0+, 生产环境高并发)
```

### 架构设计

#### 分层架构
```
┌─────────────────────────────────────────┐
│           Web UI (Vue 3)                │
├─────────────────────────────────────────┤
│         HTTP API Layer (Gin)            │
│  ┌──────────────────────────────────┐   │
│  │  Handler  │  Middleware  │ Router│   │
│  └──────────────────────────────────┘   │
├─────────────────────────────────────────┤
│       Business Logic Layer              │
│  ┌──────────────────────────────────┐   │
│  │ RSS Service │ Download Service   │   │
│  │ Rename Svc  │ Scheduler Service  │   │
│  │ Metadata Svc│ Config Service     │   │
│  └──────────────────────────────────┘   │
├─────────────────────────────────────────┤
│       Repository Layer (GORM)           │
│  ┌──────────────────────────────────┐   │
│  │ Subscription │ Download │ Config │   │
│  └──────────────────────────────────┘   │
├─────────────────────────────────────────┤
│       Database Layer (SQLite)           │
└─────────────────────────────────────────┘

External Integration:
- Mikan Project RSS
- qBittorrent API
- TMDB API (v0.2.0+)
- Mikan Subgroup API (v0.3.0+, 预留)
```

#### 项目目录结构
```
auto-rss/
├── cmd/
│   └── server/
│       └── main.go                 # 程序入口
├── internal/
│   ├── api/                        # HTTP API 层
│   │   ├── handler/                # 请求处理器
│   │   │   ├── subscription.go
│   │   │   ├── download.go
│   │   │   ├── config.go
│   │   │   └── log.go
│   │   ├── middleware/             # 中间件
│   │   │   ├── cors.go
│   │   │   ├── logger.go
│   │   │   └── recovery.go
│   │   └── router/                 # 路由定义
│   │       └── router.go
│   ├── service/                    # 业务逻辑层
│   │   ├── rss/                    # RSS 解析服务
│   │   │   ├── parser.go
│   │   │   └── fetcher.go
│   │   ├── downloader/             # 下载器服务
│   │   │   ├── qbittorrent.go
│   │   │   └── interface.go        # 下载器接口 (扩展性)
│   │   ├── renamer/                # 文件重命名
│   │   │   ├── renamer.go
│   │   │   └── pattern.go
│   │   ├── scheduler/              # 任务调度
│   │   │   └── scheduler.go
│   │   └── metadata/               # 番剧信息刮削 (v0.2.0+)
│   │       ├── tmdb.go
│   │       └── bgm.go
│   ├── repository/                 # 数据访问层
│   │   ├── subscription.go
│   │   ├── download.go
│   │   ├── config.go
│   │   └── log.go
│   ├── model/                      # 数据模型
│   │   ├── subscription.go
│   │   ├── download.go
│   │   ├── config.go
│   │   └── log.go
│   ├── config/                     # 配置管理
│   │   └── config.go
│   └── pkg/                        # 内部公共包
│       ├── logger/                 # 日志封装
│       │   └── logger.go
│       ├── database/               # 数据库连接
│       │   └── sqlite.go
│       └── utils/                  # 工具函数
│           ├── file.go
│           └── string.go
├── web/                            # 前端代码
│   ├── src/
│   │   ├── api/                    # API 调用
│   │   ├── components/             # 公共组件
│   │   ├── views/                  # 页面
│   │   │   ├── Subscription.vue
│   │   │   ├── Download.vue
│   │   │   ├── Config.vue
│   │   │   └── Log.vue
│   │   ├── router/                 # 路由
│   │   ├── store/                  # Pinia 状态管理
│   │   ├── App.vue
│   │   └── main.ts
│   ├── public/
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── scripts/                        # 构建脚本
│   ├── build.sh                    # 构建脚本
│   └── docker-build.sh             # Docker 构建
├── docker/
│   ├── Dockerfile                  # Docker 镜像
│   └── docker-compose.yml          # Compose 示例
├── docs/                           # 文档
│   ├── PRD.md                      # 本文档
│   ├── API.md                      # API 文档
│   └── DEPLOY.md                   # 部署文档
├── .env.example                    # 环境变量示例
├── .gitignore
├── go.mod
├── go.sum
├── Makefile                        # 构建任务
└── README.md                       # 项目说明
```

---

## 数据模型

### ER 图概要
```
┌─────────────────┐         ┌─────────────────┐
│  Subscription   │────────>│    Download     │
│  (订阅)         │  1:N    │   (下载记录)    │
└─────────────────┘         └─────────────────┘
        │
        │ 1:N
        v
┌─────────────────┐
│  FilterRule     │
│  (过滤规则)     │
└─────────────────┘

┌─────────────────┐
│     Config      │
│   (系统配置)    │
└─────────────────┘

┌─────────────────┐
│       Log       │
│     (日志)      │
└─────────────────┘
```

### 表结构设计

#### 1. subscriptions (订阅表)
```sql
CREATE TABLE subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,                    -- 番剧名称
    rss_url TEXT NOT NULL,                 -- RSS URL
    season INTEGER DEFAULT 1,              -- 季度
    status TEXT DEFAULT 'active',          -- 状态: active, paused
    filter_keywords TEXT,                  -- 过滤关键词 (JSON 数组)
    exclude_keywords TEXT,                 -- 排除关键词 (JSON 数组)
    subgroup_id INTEGER,                   -- 字幕组 ID (Mikan subgroupid, 可选)
    download_path TEXT,                    -- 下载路径
    rename_enabled BOOLEAN DEFAULT TRUE,   -- 是否启用重命名
    last_check_time DATETIME,              -- 最后检查时间
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subscriptions_status ON subscriptions(status);
CREATE INDEX idx_subscriptions_subgroup ON subscriptions(subgroup_id);
```

#### 2. downloads (下载记录表)
```sql
CREATE TABLE downloads (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER,               -- 关联订阅 ID
    title TEXT NOT NULL,                   -- 种子标题
    episode INTEGER,                       -- 集数
    fansub TEXT,                           -- 字幕组名称 (从标题提取)
    torrent_url TEXT NOT NULL,             -- 种子 URL
    torrent_hash TEXT UNIQUE,              -- 种子 Hash (去重)
    file_path TEXT,                        -- 文件路径
    renamed_path TEXT,                     -- 重命名后路径
    status TEXT DEFAULT 'pending',         -- 状态: pending, downloading, completed, failed
    qb_task_id TEXT,                       -- qBittorrent 任务 ID
    error_message TEXT,                    -- 错误信息
    downloaded_at DATETIME,                -- 下载完成时间
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE
);

CREATE INDEX idx_downloads_subscription ON downloads(subscription_id);
CREATE INDEX idx_downloads_status ON downloads(status);
CREATE INDEX idx_downloads_hash ON downloads(torrent_hash);
CREATE INDEX idx_downloads_fansub ON downloads(fansub);
```

#### 3. configs (配置表)
```sql
CREATE TABLE configs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT UNIQUE NOT NULL,              -- 配置键
    value TEXT NOT NULL,                   -- 配置值 (JSON)
    description TEXT,                      -- 配置说明
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 预置配置示例
INSERT INTO configs (key, value, description) VALUES
('qbittorrent.host', '"http://localhost:8080"', 'qBittorrent 地址'),
('qbittorrent.username', '"admin"', 'qBittorrent 用户名'),
('qbittorrent.password', '""', 'qBittorrent 密码'),
('rss.interval', '"30m"', 'RSS 更新间隔'),
('rename.enabled', 'true', '是否启用重命名'),
('rename.template', '"{title}/Season {season}/{title} S{season}E{episode}.{ext}"', '重命名模板');
```

#### 4. logs (日志表, 可选)
```sql
CREATE TABLE logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    level TEXT NOT NULL,                   -- 日志级别: DEBUG, INFO, WARN, ERROR
    message TEXT NOT NULL,                 -- 日志消息
    context TEXT,                          -- 上下文信息 (JSON)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_logs_level ON logs(level);
CREATE INDEX idx_logs_created ON logs(created_at);
```

### Go 数据模型 (GORM)

```go
// internal/model/subscription.go
package model

import "time"

type Subscription struct {
    ID              uint      `json:"id" gorm:"primaryKey"`
    Name            string    `json:"name" gorm:"not null"`
    RssURL          string    `json:"rss_url" gorm:"not null"`
    Season          int       `json:"season" gorm:"default:1"`
    Status          string    `json:"status" gorm:"default:active"` // active, paused
    FilterKeywords  string    `json:"filter_keywords" gorm:"type:text"` // JSON array
    ExcludeKeywords string    `json:"exclude_keywords" gorm:"type:text"` // JSON array
    SubgroupID      *int      `json:"subgroup_id"` // Mikan 字幕组 ID (可选)
    DownloadPath    string    `json:"download_path"`
    RenameEnabled   bool      `json:"rename_enabled" gorm:"default:true"`
    LastCheckTime   *time.Time `json:"last_check_time"`
    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`

    Downloads       []Download `json:"downloads,omitempty" gorm:"foreignKey:SubscriptionID"`
}

// internal/model/download.go
type Download struct {
    ID             uint      `json:"id" gorm:"primaryKey"`
    SubscriptionID uint      `json:"subscription_id"`
    Title          string    `json:"title" gorm:"not null"`
    Episode        int       `json:"episode"`
    Fansub         string    `json:"fansub"` // 字幕组名称 (从标题提取)
    TorrentURL     string    `json:"torrent_url" gorm:"not null"`
    TorrentHash    string    `json:"torrent_hash" gorm:"unique"`
    FilePath       string    `json:"file_path"`
    RenamedPath    string    `json:"renamed_path"`
    Status         string    `json:"status" gorm:"default:pending"` // pending, downloading, completed, failed
    QbTaskID       string    `json:"qb_task_id"`
    ErrorMessage   string    `json:"error_message"`
    DownloadedAt   *time.Time `json:"downloaded_at"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`

    Subscription   Subscription `json:"subscription,omitempty" gorm:"foreignKey:SubscriptionID"`
}

// internal/model/config.go
type Config struct {
    ID          uint      `json:"id" gorm:"primaryKey"`
    Key         string    `json:"key" gorm:"unique;not null"`
    Value       string    `json:"value" gorm:"not null"` // JSON string
    Description string    `json:"description"`
    UpdatedAt   time.Time `json:"updated_at"`
}

// internal/model/log.go
type Log struct {
    ID        uint      `json:"id" gorm:"primaryKey"`
    Level     string    `json:"level" gorm:"not null"` // DEBUG, INFO, WARN, ERROR
    Message   string    `json:"message" gorm:"not null"`
    Context   string    `json:"context" gorm:"type:text"` // JSON object
    CreatedAt time.Time `json:"created_at"`
}
```

---

## API 设计

### API 规范

- **协议**: RESTful API over HTTP/HTTPS
- **版本**: `/api/v1`
- **认证**: 暂不实现 (v0.4.0+)
- **响应格式**: JSON

### 统一响应格式

#### 成功响应
```json
{
  "code": 0,
  "message": "success",
  "data": {
    // 实际数据
  }
}
```

#### 错误响应
```json
{
  "code": 4001,
  "message": "订阅不存在",
  "data": null
}
```

#### 错误码定义
```
0     - 成功
1000+ - 通用错误
2000+ - 订阅相关错误
3000+ - 下载相关错误
4000+ - 配置相关错误
5000+ - 系统错误
```

### API 端点

#### 1. 订阅管理 API

##### 获取订阅列表
```
GET /api/v1/subscriptions

Query Parameters:
- status: string (可选, active/paused)
- page: int (可选, 默认 1)
- page_size: int (可选, 默认 20)

Response:
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 10,
    "page": 1,
    "page_size": 20,
    "items": [
      {
        "id": 1,
        "name": "葬送的芙莉莲",
        "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
        "season": 1,
        "status": "active",
        "filter_keywords": ["1080p", "简体"],
        "exclude_keywords": ["720p"],
        "download_path": "/downloads/anime",
        "rename_enabled": true,
        "last_check_time": "2025-10-19T12:00:00Z",
        "created_at": "2025-10-01T10:00:00Z",
        "updated_at": "2025-10-19T12:00:00Z"
      }
    ]
  }
}
```

##### 获取订阅详情
```
GET /api/v1/subscriptions/:id

Response:
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "葬送的芙莉莲",
    // ... 同上
    "downloads": [
      {
        "id": 1,
        "title": "[ANi] 葬送的芙莉莲 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT]",
        "episode": 12,
        "status": "completed",
        // ...
      }
    ]
  }
}
```

##### 创建订阅
```
POST /api/v1/subscriptions

Request Body:
{
  "name": "葬送的芙莉莲",
  "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
  "season": 1,
  "filter_keywords": ["1080p", "简体"],
  "exclude_keywords": ["720p"],
  "download_path": "/downloads/anime",
  "rename_enabled": true
}

Response:
{
  "code": 0,
  "message": "订阅创建成功",
  "data": {
    "id": 1,
    // ... 完整订阅信息
  }
}
```

##### 更新订阅
```
PUT /api/v1/subscriptions/:id

Request Body:
{
  "name": "葬送的芙莉莲 第一季",
  "status": "paused",
  // ... 其他可更新字段
}

Response:
{
  "code": 0,
  "message": "订阅更新成功",
  "data": {
    // ... 更新后的订阅信息
  }
}
```

##### 删除订阅
```
DELETE /api/v1/subscriptions/:id

Response:
{
  "code": 0,
  "message": "订阅删除成功",
  "data": null
}
```

#### 2. 下载管理 API

##### 获取下载任务列表
```
GET /api/v1/downloads

Query Parameters:
- subscription_id: int (可选)
- status: string (可选, pending/downloading/completed/failed)
- page: int (可选, 默认 1)
- page_size: int (可选, 默认 20)

Response:
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 50,
    "page": 1,
    "page_size": 20,
    "items": [
      {
        "id": 1,
        "subscription_id": 1,
        "title": "[ANi] 葬送的芙莉莲 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT]",
        "episode": 12,
        "torrent_url": "https://...",
        "torrent_hash": "abc123...",
        "file_path": "/downloads/temp/...",
        "renamed_path": "/downloads/anime/葬送的芙莉莲/Season 01/葬送的芙莉莲 S01E12.mp4",
        "status": "completed",
        "qb_task_id": "task123",
        "downloaded_at": "2025-10-19T14:00:00Z",
        "created_at": "2025-10-19T12:00:00Z",
        "updated_at": "2025-10-19T14:00:00Z"
      }
    ]
  }
}
```

##### 获取下载任务详情
```
GET /api/v1/downloads/:id

Response:
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    // ... 完整下载信息
    "subscription": {
      "id": 1,
      "name": "葬送的芙莉莲"
    }
  }
}
```

##### 手动添加下载任务
```
POST /api/v1/downloads

Request Body:
{
  "subscription_id": 1,
  "torrent_url": "https://...",
  "title": "[ANi] 葬送的芙莉莲 - 13 [1080P]...",
  "episode": 13
}

Response:
{
  "code": 0,
  "message": "下载任务添加成功",
  "data": {
    "id": 2,
    // ... 任务信息
  }
}
```

#### 3. RSS 刷新 API

##### 手动刷新所有订阅
```
POST /api/v1/rss/refresh

Response:
{
  "code": 0,
  "message": "RSS 刷新任务已启动",
  "data": {
    "task_id": "refresh_20251019_120000"
  }
}
```

##### 手动刷新单个订阅
```
POST /api/v1/rss/refresh/:subscription_id

Response:
{
  "code": 0,
  "message": "订阅刷新成功",
  "data": {
    "new_downloads": 2
  }
}
```

#### 4. 配置管理 API

##### 获取系统配置
```
GET /api/v1/config

Response:
{
  "code": 0,
  "message": "success",
  "data": {
    "qbittorrent": {
      "host": "http://localhost:8080",
      "username": "admin",
      "password": "******"
    },
    "rss": {
      "interval": "30m"
    },
    "rename": {
      "enabled": true,
      "template": "{title}/Season {season}/{title} S{season}E{episode}.{ext}"
    }
  }
}
```

##### 更新系统配置
```
PUT /api/v1/config

Request Body:
{
  "qbittorrent": {
    "host": "http://192.168.1.100:8080",
    "username": "admin",
    "password": "newpassword"
  },
  "rss": {
    "interval": "15m"
  }
}

Response:
{
  "code": 0,
  "message": "配置更新成功",
  "data": null
}
```

#### 5. 日志查询 API

##### 获取日志列表
```
GET /api/v1/logs

Query Parameters:
- level: string (可选, DEBUG/INFO/WARN/ERROR)
- start_time: string (可选, ISO8601 格式)
- end_time: string (可选, ISO8601 格式)
- keyword: string (可选, 日志消息关键词)
- page: int (可选, 默认 1)
- page_size: int (可选, 默认 100, 最大 1000)

Response:
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 500,
    "page": 1,
    "page_size": 100,
    "items": [
      {
        "id": 1,
        "level": "INFO",
        "message": "RSS 刷新成功",
        "context": {
          "subscription_id": 1,
          "new_items": 2
        },
        "created_at": "2025-10-19T12:00:00Z"
      }
    ]
  }
}
```

---

## UI 设计

### 页面结构

```
Auto-RSS Web UI
├── 导航栏 (顶部)
│   ├── Logo + 项目名称
│   ├── 订阅管理
│   ├── 下载管理
│   ├── 系统配置
│   └── 日志查看
└── 主内容区
    ├── 订阅管理页
    ├── 下载管理页
    ├── 系统配置页
    └── 日志查看页
```

### 页面功能描述

#### 1. 订阅管理页
**核心功能**:
- 订阅列表展示 (表格)
  - 列: 番剧名、RSS URL、季度、状态、最后检查时间、操作
  - 操作按钮: 编辑、暂停/启用、刷新、删除
- 添加订阅按钮 (右上角)
  - 弹窗表单: 番剧名、RSS URL、季度、过滤规则等
- 批量操作
  - 批量暂停/启用
  - 批量删除
- 搜索与过滤
  - 按番剧名搜索
  - 按状态过滤

**UI 参考**: Naive UI DataTable + Modal

#### 2. 下载管理页
**核心功能**:
- 下载任务列表 (表格)
  - 列: 番剧名、集数、标题、状态、进度、完成时间、操作
  - 状态标签: 下载中 (蓝色)、完成 (绿色)、失败 (红色)
- 下载统计卡片 (顶部)
  - 总下载数、成功数、失败数、下载中
- 任务详情弹窗
  - 种子信息、文件路径、错误信息等
- 手动添加下载按钮
- 刷新按钮 (实时同步 qBittorrent 状态)

**UI 参考**: Naive UI DataTable + Badge + Progress

#### 3. 系统配置页
**核心功能**:
- 配置表单 (分组)
  - qBittorrent 配置
    - 地址、用户名、密码
    - 连接测试按钮
  - RSS 配置
    - 更新间隔 (下拉选择: 5分钟, 15分钟, 30分钟, 1小时等)
  - 重命名配置
    - 是否启用重命名
    - 重命名模板 (v0.1.0 固定，v0.3.0 可编辑)
    - 目标根目录路径
  - 存储配置
    - 下载临时目录
    - 最终存储目录
- 保存按钮 (底部)
- 重置为默认按钮

**UI 参考**: Naive UI Form + Input + Select

#### 4. 日志查看页
**核心功能**:
- 日志列表 (表格)
  - 列: 时间、级别、消息、详情
  - 级别标签颜色: DEBUG (灰), INFO (蓝), WARN (橙), ERROR (红)
- 过滤器 (顶部)
  - 日志级别下拉选择
  - 时间范围选择器
  - 关键词搜索框
- 刷新按钮
- 清空日志按钮 (危险操作，需确认)
- 日志详情弹窗 (JSON 格式化展示上下文)

**UI 参考**: Naive UI DataTable + Tag + DatePicker

### UI 技术实现

#### 组件库选择
- **Naive UI**: 现代化、组件丰富、中文文档完善
- **核心组件**:
  - `n-data-table`: 订阅、下载、日志列表
  - `n-form`: 订阅表单、配置表单
  - `n-modal`: 弹窗 (添加订阅、任务详情等)
  - `n-button`: 操作按钮
  - `n-tag`/`n-badge`: 状态标签
  - `n-input`: 输入框
  - `n-select`: 下拉选择
  - `n-date-picker`: 时间选择器
  - `n-progress`: 下载进度条
  - `n-card`: 统计卡片

#### 前端项目结构
```
web/src/
├── api/                    # API 调用封装
│   ├── subscription.ts
│   ├── download.ts
│   ├── config.ts
│   └── log.ts
├── components/             # 公共组件
│   ├── Layout.vue          # 布局组件
│   ├── Navbar.vue          # 导航栏
│   └── ConfirmDialog.vue   # 确认对话框
├── views/                  # 页面组件
│   ├── Subscription.vue    # 订阅管理页
│   ├── Download.vue        # 下载管理页
│   ├── Config.vue          # 系统配置页
│   └── Log.vue             # 日志查看页
├── router/                 # 路由配置
│   └── index.ts
├── store/                  # Pinia 状态管理
│   ├── subscription.ts
│   ├── download.ts
│   └── config.ts
├── types/                  # TypeScript 类型定义
│   ├── subscription.ts
│   ├── download.ts
│   └── api.ts
├── utils/                  # 工具函数
│   ├── request.ts          # Axios 封装
│   └── format.ts           # 格式化工具
├── App.vue
└── main.ts
```

---

## 部署方案

### 部署方式

#### 1. Docker 部署 (推荐)

**Dockerfile** (多阶段构建):
```dockerfile
# Stage 1: 构建前端
FROM node:18-alpine AS web-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: 构建后端
FROM golang:1.21-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 嵌入前端静态资源
COPY --from=web-builder /app/web/dist ./web/dist
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o auto-rss ./cmd/server

# Stage 3: 最终镜像
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=go-builder /app/auto-rss .
EXPOSE 7892
ENV TZ=Asia/Shanghai
ENTRYPOINT ["./auto-rss"]
```

**Docker Compose 示例**:
```yaml
version: '3.8'

services:
  auto-rss:
    image: auto-rss:latest
    container_name: auto-rss
    ports:
      - "7892:7892"
    environment:
      - DB_PATH=/data/auto-rss.db
      - QB_HOST=http://192.168.1.100:8080
      - QB_USERNAME=admin
      - QB_PASSWORD=yourpassword
      - RSS_INTERVAL=30m
      - LOG_LEVEL=info
    volumes:
      - ./data:/data                    # 数据库存储
      - ./downloads:/downloads          # 下载目录 (与 qBittorrent 共享)
    restart: unless-stopped
```

**部署命令**:
```bash
# 拉取镜像
docker pull auto-rss:latest

# 运行容器
docker run -d \
  --name auto-rss \
  -p 7892:7892 \
  -e QB_HOST=http://192.168.1.100:8080 \
  -e QB_USERNAME=admin \
  -e QB_PASSWORD=yourpassword \
  -v $(pwd)/data:/data \
  -v $(pwd)/downloads:/downloads \
  auto-rss:latest
```

#### 2. 二进制部署

**交叉编译脚本** (`scripts/build.sh`):
```bash
#!/bin/bash
set -e

# 构建前端
cd web
npm install
npm run build
cd ..

# 构建 Go 二进制
VERSION=$(git describe --tags --always --dirty)
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

platforms=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
)

for platform in "${platforms[@]}"; do
  GOOS=${platform%/*}
  GOARCH=${platform#*/}
  output="auto-rss-${GOOS}-${GOARCH}"

  if [ "$GOOS" = "windows" ]; then
    output+=".exe"
  fi

  echo "Building $output..."
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build \
    -ldflags "-X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" \
    -o "dist/$output" \
    ./cmd/server
done

echo "Build completed!"
```

**部署步骤**:
```bash
# 1. 下载二进制
wget https://github.com/yourrepo/auto-rss/releases/download/v0.1.0/auto-rss-linux-amd64

# 2. 添加执行权限
chmod +x auto-rss-linux-amd64

# 3. 创建配置文件 .env
cat > .env <<EOF
DB_PATH=./data/auto-rss.db
QB_HOST=http://localhost:8080
QB_USERNAME=admin
QB_PASSWORD=yourpassword
RSS_INTERVAL=30m
LOG_LEVEL=info
EOF

# 4. 运行
./auto-rss-linux-amd64
```

**Systemd 服务** (Linux):
```ini
[Unit]
Description=Auto-RSS Service
After=network.target

[Service]
Type=simple
User=youruser
WorkingDirectory=/opt/auto-rss
ExecStart=/opt/auto-rss/auto-rss
Restart=on-failure
EnvironmentFile=/opt/auto-rss/.env

[Install]
WantedBy=multi-user.target
```

### 环境变量说明

| 变量名 | 说明 | 默认值 | 示例 |
|--------|------|--------|------|
| `DB_PATH` | SQLite 数据库路径 | `./data/auto-rss.db` | `/data/auto-rss.db` |
| `QB_HOST` | qBittorrent 地址 | `http://localhost:8080` | `http://192.168.1.100:8080` |
| `QB_USERNAME` | qBittorrent 用户名 | `admin` | `admin` |
| `QB_PASSWORD` | qBittorrent 密码 | `""` | `yourpassword` |
| `RSS_INTERVAL` | RSS 更新间隔 | `30m` | `15m`, `1h` |
| `LOG_LEVEL` | 日志级别 | `info` | `debug`, `info`, `warn`, `error` |
| `SERVER_PORT` | Web 服务端口 | `7892` | `8080` |
| `DOWNLOAD_PATH` | 默认下载路径 | `/downloads` | `/data/anime` |

---

## 开发计划

### 开发阶段

#### 阶段 1: 后端核心逻辑 (约 1 周)

**Day 1-2: 项目初始化与数据层**
- [x] Go module 初始化
- [x] 项目目录结构创建
- [x] GORM 数据模型定义 (Subscription, Download, Config, Log)
- [x] 数据库迁移脚本
- [x] Repository 层实现 (CRUD)
- [x] SQLite 连接与配置

**Day 3-4: 核心服务实现**
- [x] RSS 解析服务
  - gofeed 集成
  - Mikan Project RSS 解析
  - 种子信息提取
- [x] qBittorrent 客户端封装
  - 连接管理
  - 添加任务 API
  - 状态查询 API
- [x] 文件重命名逻辑
  - 集数提取正则
  - 重命名模板引擎
  - 文件移动操作

**Day 5-7: 业务逻辑与调度**
- [x] 定时任务调度器 (robfig/cron)
- [x] RSS 刷新服务
  - 定时刷新所有订阅
  - 智能去重逻辑
  - 下载任务创建
- [x] 下载管理服务
  - 任务状态同步
  - 完成后重命名触发
- [x] 配置管理服务
  - 环境变量加载
  - 数据库配置读写
  - 配置合并逻辑
- [x] 日志系统 (zap)
- [x] 单元测试 (核心逻辑)

#### 阶段 2: API 与前端 (约 1 周)

**Day 1-2: RESTful API 实现**
- [x] Gin 路由配置
- [x] 中间件 (CORS, Logger, Recovery)
- [x] 订阅管理 API Handler
- [x] 下载管理 API Handler
- [x] RSS 刷新 API Handler
- [x] 配置管理 API Handler
- [x] 日志查询 API Handler
- [x] Swagger 文档生成
- [x] API 测试 (Postman/curl)

**Day 3-5: Vue 3 前端开发**
- [x] Vue 3 + Vite 项目初始化
- [x] Naive UI 集成
- [x] 路由配置 (Vue Router)
- [x] 状态管理 (Pinia)
- [x] Axios API 封装
- [x] 订阅管理页开发
  - 订阅列表展示
  - 添加/编辑订阅表单
  - 订阅操作 (暂停/刷新/删除)
- [x] 下载管理页开发
  - 下载任务列表
  - 任务状态展示
  - 任务详情弹窗
- [x] 系统配置页开发
  - 配置表单
  - 配置保存/重置
- [x] 日志查看页开发
  - 日志列表
  - 过滤器
  - 日志详情

**Day 6-7: 前后端联调**
- [x] API 接口联调
- [x] 错误处理完善
- [x] 加载状态优化
- [x] 交互体验优化

#### 阶段 3: 完善与部署 (约 3-4 天)

**Day 1-2: 功能完善**
- [x] 错误处理完善
- [x] 日志系统优化
- [x] 边界情况处理
- [x] 集成测试
- [x] 性能优化

**Day 3-4: 部署与发布**
- [x] Dockerfile 编写
- [x] 多阶段构建优化
- [x] 多架构镜像构建 (amd64, arm64)
- [x] Docker Compose 示例
- [x] 二进制交叉编译脚本
- [x] README 编写 (中文)
- [x] 快速开始文档
- [x] API 文档完善
- [x] 部署文档编写
- [x] 发布 v0.1.0

### 开发里程碑

| 里程碑 | 完成时间 | 交付物 |
|--------|----------|--------|
| M1: 后端核心完成 | Week 1 | 可运行的后端服务 + 单元测试 |
| M2: API 完成 | Week 2 (Day 2) | 完整的 RESTful API + Swagger 文档 |
| M3: 前端完成 | Week 2 (Day 7) | 可用的 Web UI + 前后端联调 |
| M4: MVP 发布 | Week 3 (Day 4) | Docker 镜像 + 二进制发布 + 文档 |

---

## 质量标准

### 代码规范 (适度执行)

#### Go 代码
- ✅ **必须**: golangci-lint 检查通过 (关键规则)
- ✅ **必须**: gofmt / goimports 格式化
- ✅ **必须**: 导出函数有注释
- ✅ **必须**: 错误显式处理 (不忽略错误)
- ✅ **推荐**: 遵循 Effective Go 指南
- ⚠️ **可选**: 100% 测试覆盖率 (核心逻辑有测试即可)

#### 前端代码
- ✅ **必须**: ESLint 检查通过
- ✅ **必须**: Prettier 自动格式化
- ✅ **必须**: TypeScript 严格模式
- ✅ **推荐**: 组件有注释
- ⚠️ **可选**: 单元测试 (核心业务逻辑)

#### Git 提交规范
- ✅ **推荐**: Conventional Commits 格式
  ```
  feat: 添加RSS订阅管理API
  fix: 修复重命名路径错误
  docs: 更新部署文档
  style: 代码格式化
  refactor: 重构下载服务
  test: 添加RSS解析测试
  chore: 更新依赖
  ```
- ✅ **推荐**: 中文提交信息 (清晰易读)

#### 文档要求
- ✅ **必须**: README (中文, 包含快速开始)
- ✅ **必须**: API 文档 (Swagger, 中文)
- ✅ **必须**: 部署文档 (Docker + 二进制)
- ✅ **推荐**: 代码关键逻辑注释 (中文)

### 测试要求 (适度)

#### 单元测试
- ✅ **必须**: 核心业务逻辑有测试
  - RSS 解析逻辑
  - 重命名逻辑
  - 去重逻辑
- ✅ **推荐**: Repository 层测试
- ⚠️ **可选**: Handler 层测试 (可用 Postman 手动测试)

#### 集成测试
- ✅ **必须**: 关键 API 端点测试
  - 订阅 CRUD
  - 下载任务创建
- ⚠️ **可选**: 端到端测试

### 性能要求

- ✅ RSS 解析: < 5s (单个订阅)
- ✅ API 响应: < 500ms (列表查询)
- ✅ 内存占用: < 100MB (空载)
- ✅ Docker 镜像: < 50MB (压缩后)

---

## 后续版本规划

### v0.2.0 (预计 1-2 周)

**核心功能**:
- ✅ TMDB / BGM 番剧信息刮削
- ✅ 番剧封面与详情展示
- ✅ 智能季度识别
- ✅ Telegram 通知
- ✅ 邮件通知 (SMTP)

**UI 增强**:
- ✅ 番剧信息页 (封面、简介、评分)
- ✅ 通知配置页
- ✅ 下载统计图表

### v0.3.0 (预计 2-3 周)

**核心功能**:
- ✅ 字幕组搜索与匹配
  - Mikan 字幕组搜索 API 集成
  - 字幕组名称 → subgroupid 自动匹配
  - 手动绑定字幕组 ID
- ✅ 自定义重命名规则引擎
- ✅ 可视化规则测试
- ✅ 多 RSS 源支持 (dmhy, Nyaa)
- ✅ 高级过滤规则 (正则表达式)
- ✅ PostgreSQL 支持

**UI 增强**:
- ✅ 字幕组搜索与管理页
- ✅ 重命名规则编辑器
- ✅ RSS 源管理页
- ✅ 高级搜索功能

### v0.4.0 (待定)

**核心功能**:
- ✅ 用户认证系统 (JWT)
- ✅ 多用户支持
- ✅ 权限管理

### v0.5.0 (待定)

**核心功能**:
- ✅ OpenAI / LLM 集成 (番剧推荐)
- ✅ 智能标题解析优化
- ✅ 插件系统 (可扩展架构)

---

## 附录

### 技术栈版本
```yaml
后端:
  - Go: 1.21+
  - Gin: v1.9+
  - GORM: v2.0+
  - gofeed: v1.2+
  - zap: v1.26+
  - cron: v3.0+

前端:
  - Node.js: 18+
  - Vue: 3.4+
  - TypeScript: 5.0+
  - Vite: 5.0+
  - Naive UI: 2.38+
  - Pinia: 2.1+

数据库:
  - SQLite: 3.40+

部署:
  - Docker: 20.10+
  - Docker Compose: 2.0+
```

### 参考链接
- [auto_bangumi GitHub](https://github.com/EstrellaXD/Auto_Bangumi)
- [ani-rss GitHub](https://github.com/wushuo894/ani-rss)
- [Mikan Project](https://mikanani.me/)
- [Gin 官方文档](https://gin-gonic.com/zh-cn/)
- [GORM 官方文档](https://gorm.io/zh_CN/)
- [Vue 3 官方文档](https://cn.vuejs.org/)
- [Naive UI 官方文档](https://www.naiveui.com/zh-CN/)

---

**文档版本**: v1.0
**最后更新**: 2025-10-19
**维护者**: Auto-RSS Team
