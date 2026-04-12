# Phase 7 Research: API层功能增强

## 现有基础设施分析

### 1. 标签系统 (Tag System)

**已存在:**
- `model/subscription.go`: `SubscriptionTag`, `SubscriptionTagRelation` 模型已定义
- `repository/subscription.go`: 完整的标签管理方法
  - `CreateTag`, `UpdateTag`, `DeleteTag`
  - `GetTagByID`, `GetTagByName`, `ListTags`
  - `AddTagsToSubscription`, `RemoveTagsFromSubscription`
  - `GetSubscriptionTags`, `GetSubscriptionsByTag`
  - `SearchSubscriptions` 已支持 tagIDs 筛选

**缺失:**
- Handler API 端点
- Router 路由注册

### 2. 下载历史和统计 (Download History & Statistics)

**已存在:**
- `repository/download.go`:
  - `GetDownloadHistory(filter, offset, limit)` - 支持时间、状态、订阅筛选
  - `GetDownloadStatistics(days)` - 获取各状态数量、每日统计、订阅排行

**缺失:**
- Handler API 端点
- Router 路由注册

### 3. RSS健康检查 (RSS Health Check)

**已存在:**
- `service/rss/health.go`: `RSSHealthChecker` 服务完整实现
  - `CheckSubscription` - 检查单个订阅
  - `CheckAllSubscriptions` - 批量检查
  - `GetDeadSubscriptions` - 获取失效订阅
  - `StartPeriodicCheck` - 定时检查
- `internal/api/handler/health.go`: 基础健康检查端点

**缺失:**
- 专门的 RSS 健康检查 handler
- API 端点暴露

## API设计建议

### 标签系统 API
```
GET    /api/v1/tags                    # 列出所有标签
POST   /api/v1/tags                    # 创建标签
PUT    /api/v1/tags/:id                 # 更新标签
DELETE /api/v1/tags/:id                 # 删除标签
GET    /api/v1/tags/:id/subscriptions   # 获取标签下的订阅

GET    /api/v1/subscriptions/:id/tags   # 获取订阅的标签
POST   /api/v1/subscriptions/:id/tags   # 为订阅添加标签
DELETE /api/v1/subscriptions/:id/tags   # 移除订阅的标签
```

### 下载历史和统计 API
```
GET /api/v1/downloads/history           # 下载历史（支持筛选）
GET /api/v1/downloads/statistics        # 下载统计
GET /api/v1/downloads/timeline          # 下载时间线
```

### RSS健康检查 API
```
GET /api/v1/rss/health                  # 检查所有订阅的RSS健康
GET /api/v1/rss/health/:id              # 检查单个订阅的RSS健康
GET /api/v1/rss/dead                    # 获取失效的RSS订阅
POST /api/v1/rss/health-check           # 触发健康检查任务
```

## 技术决策

1. **标签颜色**: 使用前端传入的颜色值，默认 `#18a058`
2. **统计时间范围**: 默认7天，可通过参数调整
3. **健康检查超时**: 复用现有配置 30s
4. **响应格式**: 统一使用 `{code, message, data}` 格式

## 依赖关系

无额外依赖，全部基于现有基础设施。
