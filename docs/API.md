# Auto-RSS API 文档

> **版本**: v0.1.0
> **协议**: RESTful API over HTTP/HTTPS
> **基础路径**: `/api/v1`
> **数据格式**: JSON

---

## 📋 目录

1. [API 概述](#api-概述)
2. [通用规范](#通用规范)
3. [认证机制](#认证机制)
4. [错误处理](#错误处理)
5. [API 端点](#api-端点)
6. [数据模型](#数据模型)

---

## API 概述

Auto-RSS 提供完整的 RESTful API，用于管理番剧订阅、下载任务、系统配置和日志查询。

### 基础信息

- **服务地址**: `http://localhost:7892` (默认)
- **API 版本**: `v1`
- **Content-Type**: `application/json`
- **字符编码**: UTF-8

### 支持的操作

- ✅ 订阅管理 (CRUD)
- ✅ 下载任务管理
- ✅ RSS 手动刷新
- ✅ 系统配置管理
- ✅ 日志查询

---

## 通用规范

### 统一响应格式

所有 API 响应遵循统一的 JSON 格式：

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

### 错误码定义

| 错误码范围 | 说明 | 示例 |
|-----------|------|------|
| 0 | 成功 | - |
| 1000-1999 | 通用错误 | 1001: 参数错误, 1002: 请求方法错误 |
| 2000-2999 | 订阅相关错误 | 2001: 订阅不存在, 2002: RSS URL 无效 |
| 3000-3999 | 下载相关错误 | 3001: 下载任务不存在, 3002: qBittorrent 连接失败 |
| 4000-4999 | 配置相关错误 | 4001: 配置项不存在, 4002: 配置值无效 |
| 5000-5999 | 系统错误 | 5001: 数据库错误, 5002: 内部服务错误 |

### 常见错误码

```yaml
0:    成功
1001: 参数错误
1002: 请求方法错误
1003: Content-Type 错误
1004: 请求体解析失败
2001: 订阅不存在
2002: RSS URL 无效
2003: 订阅名称重复
2004: 订阅创建失败
2005: 订阅更新失败
2006: 订阅删除失败
3001: 下载任务不存在
3002: qBittorrent 连接失败
3003: 种子添加失败
4001: 配置项不存在
4002: 配置值无效
5001: 数据库错误
5002: 内部服务错误
```

### 分页参数

所有列表接口支持分页查询：

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| page | int | 1 | 页码 (从 1 开始) |
| page_size | int | 20 | 每页数量 (最大 100) |

分页响应格式：
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 100,
    "page": 1,
    "page_size": 20,
    "items": [...]
  }
}
```

### 时间格式

所有时间字段使用 **ISO 8601** 格式：

```
2025-10-19T12:00:00Z
2025-10-19T12:00:00+08:00
```

---

## 认证机制

**v0.1.0**: 暂不实现认证

**v0.4.0+**: 将支持 JWT 认证

```http
Authorization: Bearer <token>
```

---

## 错误处理

### 错误响应示例

#### 参数错误 (1001)
```json
{
  "code": 1001,
  "message": "参数错误: name 不能为空",
  "data": null
}
```

#### 资源不存在 (2001)
```json
{
  "code": 2001,
  "message": "订阅不存在: ID=999",
  "data": null
}
```

#### 服务器错误 (5001)
```json
{
  "code": 5001,
  "message": "数据库错误: connection timeout",
  "data": null
}
```

### HTTP 状态码映射

| HTTP 状态码 | 错误码范围 | 说明 |
|------------|-----------|------|
| 200 | 0 | 成功 |
| 400 | 1000-1999 | 客户端请求错误 |
| 404 | 2000-4999 | 资源不存在 |
| 500 | 5000-5999 | 服务器内部错误 |

---

## API 端点

### 1. 订阅管理

#### 1.1 获取订阅列表

```http
GET /api/v1/subscriptions
```

**Query Parameters**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| status | string | 否 | - | 订阅状态 (active, paused) |
| page | int | 否 | 1 | 页码 |
| page_size | int | 否 | 20 | 每页数量 |

**Response**:

```json
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
        "subgroup_id": 615,
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

#### 1.2 获取订阅详情

```http
GET /api/v1/subscriptions/:id
```

**Path Parameters**:

| 参数 | 类型 | 说明 |
|------|------|------|
| id | int | 订阅 ID |

**Response**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "葬送的芙莉莲",
    "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
    "season": 1,
    "status": "active",
    "filter_keywords": ["1080p", "简体"],
    "exclude_keywords": ["720p"],
    "subgroup_id": 615,
    "download_path": "/downloads/anime",
    "rename_enabled": true,
    "last_check_time": "2025-10-19T12:00:00Z",
    "created_at": "2025-10-01T10:00:00Z",
    "updated_at": "2025-10-19T12:00:00Z",
    "downloads": [
      {
        "id": 1,
        "title": "[ANi] 葬送的芙莉莲 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT]",
        "episode": 12,
        "fansub": "ANi",
        "status": "completed",
        "downloaded_at": "2025-10-19T14:00:00Z"
      }
    ]
  }
}
```

#### 1.3 创建订阅

```http
POST /api/v1/subscriptions
```

**Request Body**:

```json
{
  "name": "葬送的芙莉莲",
  "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
  "season": 1,
  "filter_keywords": ["1080p", "简体"],
  "exclude_keywords": ["720p"],
  "subgroup_id": 615,
  "download_path": "/downloads/anime",
  "rename_enabled": true
}
```

**字段说明**:

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| name | string | 是 | - | 番剧名称 |
| rss_url | string | 是 | - | RSS 订阅地址 |
| season | int | 否 | 1 | 季度 |
| filter_keywords | array | 否 | [] | 过滤关键词 (包含) |
| exclude_keywords | array | 否 | [] | 排除关键词 |
| subgroup_id | int | 否 | null | 字幕组 ID (Mikan) |
| download_path | string | 否 | - | 下载路径 |
| rename_enabled | bool | 否 | true | 是否启用重命名 |

**Response**:

```json
{
  "code": 0,
  "message": "订阅创建成功",
  "data": {
    "id": 1,
    "name": "葬送的芙莉莲",
    "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
    "season": 1,
    "status": "active",
    "created_at": "2025-10-19T15:00:00Z"
  }
}
```

#### 1.4 更新订阅

```http
PUT /api/v1/subscriptions/:id
```

**Path Parameters**:

| 参数 | 类型 | 说明 |
|------|------|------|
| id | int | 订阅 ID |

**Request Body**:

```json
{
  "name": "葬送的芙莉莲 第一季",
  "status": "paused",
  "season": 1,
  "filter_keywords": ["1080p", "简繁"],
  "subgroup_id": 615
}
```

**Response**:

```json
{
  "code": 0,
  "message": "订阅更新成功",
  "data": {
    "id": 1,
    "name": "葬送的芙莉莲 第一季",
    "status": "paused",
    "updated_at": "2025-10-19T15:30:00Z"
  }
}
```

#### 1.5 删除订阅

```http
DELETE /api/v1/subscriptions/:id
```

**Path Parameters**:

| 参数 | 类型 | 说明 |
|------|------|------|
| id | int | 订阅 ID |

**Response**:

```json
{
  "code": 0,
  "message": "订阅删除成功",
  "data": null
}
```

#### 1.6 批量重命名订阅文件

```http
POST /api/v1/subscriptions/:id/rename-files
```

**功能说明**:
当订阅信息（如番剧名称、季度等）被修改后，批量重命名该订阅的所有已下载文件，并移动到新的目录结构。

**Path Parameters**:

| 参数 | 类型 | 说明 |
|------|------|------|
| id | int | 订阅 ID |

**处理流程**:
1. 查询订阅的所有已完成下载记录
2. 对每个下载记录：
   - 获取种子信息和文件列表
   - 根据新的订阅信息生成新文件名和目录
   - 调用qBittorrent API进行重命名和移动
   - 更新数据库中的下载记录

**Response**:

```json
{
  "code": 0,
  "message": "重命名任务已启动",
  "data": {
    "task_id": "task_123456"
  }
}
```

**任务结果查询**:
通过任务管理接口查询任务进度和结果：

```http
GET /api/v1/tasks/current
```

任务完成后的结果示例：

```json
{
  "moved": 12,      // 移动的文件数
  "renamed": 10,    // 重命名的文件数
  "errors": 1       // 错误数
}
```

**注意事项**:
- 只处理 status='completed' 的下载记录
- 如果种子不存在或已删除，会跳过并记录警告
- 重命名失败不会影响其他文件的处理
- 使用与系统配置相同的重命名模板

**使用场景**:
- 修改番剧名称后，需要更新所有已下载文件的命名
- 修改季度信息后，需要重新组织文件目录结构
- 手工补全订阅信息后，统一文件命名规范

**自动触发**:
- 当使用 `POST /api/v1/subscriptions/:id/enrich-bangumi` 补全Bangumi数据时，如果番剧名称发生变化，会自动触发文件重命名任务
- 自动触发的重命名任务会在后台异步执行，不会阻塞补全接口的响应

---

### 2. 下载管理

#### 2.1 获取下载任务列表

```http
GET /api/v1/downloads
```

**Query Parameters**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| subscription_id | int | 否 | - | 订阅 ID |
| status | string | 否 | - | 任务状态 (pending, downloading, completed, failed) |
| page | int | 否 | 1 | 页码 |
| page_size | int | 否 | 20 | 每页数量 |

**Response**:

```json
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
        "fansub": "ANi",
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

#### 2.2 获取下载任务详情

```http
GET /api/v1/downloads/:id
```

**Path Parameters**:

| 参数 | 类型 | 说明 |
|------|------|------|
| id | int | 下载任务 ID |

**Response**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "subscription_id": 1,
    "title": "[ANi] 葬送的芙莉莲 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT]",
    "episode": 12,
    "fansub": "ANi",
    "torrent_url": "https://...",
    "torrent_hash": "abc123...",
    "file_path": "/downloads/temp/...",
    "renamed_path": "/downloads/anime/葬送的芙莉莲/Season 01/葬送的芙莉莲 S01E12.mp4",
    "status": "completed",
    "qb_task_id": "task123",
    "error_message": null,
    "downloaded_at": "2025-10-19T14:00:00Z",
    "created_at": "2025-10-19T12:00:00Z",
    "updated_at": "2025-10-19T14:00:00Z",
    "subscription": {
      "id": 1,
      "name": "葬送的芙莉莲"
    }
  }
}
```

#### 2.3 手动添加下载任务

```http
POST /api/v1/downloads
```

**Request Body**:

```json
{
  "subscription_id": 1,
  "torrent_url": "https://...",
  "title": "[ANi] 葬送的芙莉莲 - 13 [1080P]...",
  "episode": 13
}
```

**字段说明**:

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| subscription_id | int | 是 | 订阅 ID |
| torrent_url | string | 是 | 种子 URL |
| title | string | 是 | 种子标题 |
| episode | int | 否 | 集数 (可自动提取) |

**Response**:

```json
{
  "code": 0,
  "message": "下载任务添加成功",
  "data": {
    "id": 2,
    "subscription_id": 1,
    "title": "[ANi] 葬送的芙莉莲 - 13 [1080P]...",
    "episode": 13,
    "status": "pending",
    "created_at": "2025-10-19T15:00:00Z"
  }
}
```

---

### 3. RSS 刷新

#### 3.1 手动刷新所有订阅

```http
POST /api/v1/rss/refresh
```

**Request Body**: 无

**Response**:

```json
{
  "code": 0,
  "message": "RSS 刷新任务已启动",
  "data": {
    "task_id": "refresh_20251019_120000",
    "subscriptions_count": 10
  }
}
```

#### 3.2 手动刷新单个订阅

```http
POST /api/v1/rss/refresh/:subscription_id
```

**Path Parameters**:

| 参数 | 类型 | 说明 |
|------|------|------|
| subscription_id | int | 订阅 ID |

**Response**:

```json
{
  "code": 0,
  "message": "订阅刷新成功",
  "data": {
    "subscription_id": 1,
    "new_downloads": 2,
    "items": [
      {
        "title": "[ANi] 葬送的芙莉莲 - 14 [1080P]...",
        "episode": 14
      },
      {
        "title": "[ANi] 葬送的芙莉莲 - 15 [1080P]...",
        "episode": 15
      }
    ]
  }
}
```

---

### 4. 配置管理

#### 4.1 获取系统配置

```http
GET /api/v1/config
```

**Response**:

```json
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
    },
    "download": {
      "path": "/downloads"
    }
  }
}
```

#### 4.2 更新系统配置

```http
PUT /api/v1/config
```

**Request Body**:

```json
{
  "qbittorrent": {
    "host": "http://192.168.1.100:8080",
    "username": "admin",
    "password": "newpassword"
  },
  "rss": {
    "interval": "15m"
  },
  "rename": {
    "enabled": true
  }
}
```

**Response**:

```json
{
  "code": 0,
  "message": "配置更新成功",
  "data": null
}
```

**配置项说明**:

| 配置项 | 类型 | 说明 | 示例 |
|--------|------|------|------|
| qbittorrent.host | string | qBittorrent 地址 | `http://localhost:8080` |
| qbittorrent.username | string | qBittorrent 用户名 | `admin` |
| qbittorrent.password | string | qBittorrent 密码 | `password` |
| rss.interval | string | RSS 更新间隔 | `30m`, `1h`, `15m` |
| rename.enabled | bool | 是否启用重命名 | `true`, `false` |
| rename.template | string | 重命名模板 | `{title} S{season}E{episode}` |
| download.path | string | 下载路径 | `/downloads` |

---

### 5. 日志查询

#### 5.1 获取日志列表

```http
GET /api/v1/logs
```

**Query Parameters**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| level | string | 否 | - | 日志级别 (DEBUG, INFO, WARN, ERROR) |
| start_time | string | 否 | - | 开始时间 (ISO8601) |
| end_time | string | 否 | - | 结束时间 (ISO8601) |
| keyword | string | 否 | - | 关键词搜索 |
| page | int | 否 | 1 | 页码 |
| page_size | int | 否 | 100 | 每页数量 (最大 1000) |

**Response**:

```json
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
      },
      {
        "id": 2,
        "level": "ERROR",
        "message": "qBittorrent 连接失败",
        "context": {
          "host": "http://localhost:8080",
          "error": "connection refused"
        },
        "created_at": "2025-10-19T12:05:00Z"
      }
    ]
  }
}
```

---

## 数据模型

### Subscription (订阅)

```typescript
interface Subscription {
  id: number;
  name: string;
  rss_url: string;
  season: number;
  status: 'active' | 'paused';
  filter_keywords: string[];
  exclude_keywords: string[];
  subgroup_id?: number;          // 字幕组 ID (可选)
  download_path: string;
  rename_enabled: boolean;
  last_check_time?: string;      // ISO8601
  created_at: string;            // ISO8601
  updated_at: string;            // ISO8601
  downloads?: Download[];        // 关联的下载记录
}
```

### Download (下载任务)

```typescript
interface Download {
  id: number;
  subscription_id: number;
  title: string;
  episode: number;
  fansub: string;                // 字幕组名称
  torrent_url: string;
  torrent_hash: string;
  file_path: string;
  renamed_path: string;
  status: 'pending' | 'downloading' | 'completed' | 'failed';
  qb_task_id: string;
  error_message?: string;
  downloaded_at?: string;        // ISO8601
  created_at: string;            // ISO8601
  updated_at: string;            // ISO8601
  subscription?: Subscription;   // 关联的订阅
}
```

### Config (配置)

```typescript
interface Config {
  qbittorrent: {
    host: string;
    username: string;
    password: string;
  };
  rss: {
    interval: string;            // 如: "30m", "1h"
  };
  rename: {
    enabled: boolean;
    template: string;
  };
  download: {
    path: string;
  };
}
```

### Log (日志)

```typescript
interface Log {
  id: number;
  level: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';
  message: string;
  context: Record<string, any>;  // JSON object
  created_at: string;            // ISO8601
}
```

---

## 使用示例

### 示例 1: 创建订阅并刷新

```bash
# 1. 创建订阅
curl -X POST http://localhost:7892/api/v1/subscriptions \
  -H "Content-Type: application/json" \
  -d '{
    "name": "葬送的芙莉莲",
    "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
    "season": 1,
    "filter_keywords": ["1080p", "简体"],
    "subgroup_id": 615
  }'

# 2. 手动刷新订阅
curl -X POST http://localhost:7892/api/v1/rss/refresh/1

# 3. 查看下载任务
curl http://localhost:7892/api/v1/downloads?subscription_id=1
```

### 示例 2: 更新配置

```bash
# 更新 qBittorrent 连接配置
curl -X PUT http://localhost:7892/api/v1/config \
  -H "Content-Type: application/json" \
  -d '{
    "qbittorrent": {
      "host": "http://192.168.1.100:8080",
      "username": "admin",
      "password": "yourpassword"
    },
    "rss": {
      "interval": "15m"
    }
  }'
```

### 示例 3: 查询日志

```bash
# 查询错误日志
curl "http://localhost:7892/api/v1/logs?level=ERROR&page=1&page_size=50"

# 查询包含关键词的日志
curl "http://localhost:7892/api/v1/logs?keyword=qBittorrent"
```

---

## 速率限制

**v0.1.0**: 暂无速率限制

**v0.4.0+**: 将实施以下限制：
- 普通接口: 60 次/分钟
- RSS 刷新: 10 次/分钟

---

## 更新日志

### v0.1.0 (2025-10-19)
- ✅ 初始版本
- ✅ 订阅管理 API
- ✅ 下载管理 API
- ✅ RSS 刷新 API
- ✅ 配置管理 API
- ✅ 日志查询 API

---

**文档版本**: v1.0
**最后更新**: 2025-10-19
**维护者**: Auto-RSS Team
