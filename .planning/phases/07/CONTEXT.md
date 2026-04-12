# Phase 7 Context: API层功能增强

## Phase Goal

为 Auto-RSS 的现有后端功能（标签系统、下载历史、RSS健康检查）添加完整的 REST API 层，使前端能够调用这些功能。

## System State

### 已完成的基础设施

1. **标签系统（Models + Repository）**
   - `SubscriptionTag` 和 `SubscriptionTagRelation` 模型
   - 完整的 CRUD 和关联管理方法
   - 订阅搜索已支持按标签筛选

2. **下载历史和统计（Repository）**
   - `GetDownloadHistory` 支持筛选和分页
   - `GetDownloadStatistics` 提供多维度统计
   - `DownloadHistoryFilter` 筛选结构

3. **RSS健康检查（Service）**
   - `RSSHealthChecker` 服务实现
   - 支持单个/批量检查
   - 支持定时检查任务

### 需要添加的API层

| 功能 | 现有 | 需添加 |
|------|------|--------|
| 标签系统 | Model, Repository | Handler, Router |
| 下载历史 | Repository | Handler, Router |
| 下载统计 | Repository | Handler, Router |
| RSS健康检查 | Service | Handler, Router |

## API端点设计

### 标签系统
```
GET    /api/v1/tags
POST   /api/v1/tags
PUT    /api/v1/tags/:id
DELETE /api/v1/tags/:id

GET    /api/v1/subscriptions/:id/tags
POST   /api/v1/subscriptions/:id/tags
DELETE /api/v1/subscriptions/:id/tags/:tag_id
```

### 下载管理扩展
```
GET /api/v1/downloads/history?subscription_id=&status=&start_date=&end_date=
GET /api/v1/downloads/statistics?days=7
```

### RSS健康检查
```
GET  /api/v1/rss/health
GET  /api/v1/rss/health/:subscription_id
GET  /api/v1/rss/dead
POST /api/v1/rss/health-check
```

## 关键代码参考

### 标签 Repository 接口
```go
CreateTag(tag *model.SubscriptionTag) error
UpdateTag(tag *model.SubscriptionTag) error
DeleteTag(id uint) error
GetTagByID(id uint) (*model.SubscriptionTag, error)
ListTags() ([]model.SubscriptionTag, error)
AddTagsToSubscription(subscriptionID uint, tagIDs []uint) error
RemoveTagsFromSubscription(subscriptionID uint, tagIDs []uint) error
GetSubscriptionTags(subscriptionID uint) ([]model.SubscriptionTag, error)
```

### 下载历史 Repository 接口
```go
GetDownloadHistory(filter *DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error)
GetDownloadStatistics(days int) (*DownloadStatistics, error)
```

### RSS健康检查服务
```go
type RSSHealthChecker struct {
    subscriptionRepo repository.SubscriptionRepository
    httpClient       *http.Client
}

func (c *RSSHealthChecker) CheckSubscription(ctx context.Context, sub *model.Subscription) *HealthCheckResult
func (c *RSSHealthChecker) CheckAllSubscriptions(ctx context.Context) ([]*HealthCheckResult, error)
func (c *RSSHealthChecker) GetDeadSubscriptions(ctx context.Context) ([]*HealthCheckResult, error)
```

## 响应格式

统一使用：
```json
{
  "code": 0,
  "message": "Success",
  "data": { ... }
}
```

错误码：
- `400` - 请求参数错误
- `404` - 资源不存在
- `500` - 服务器内部错误

## 参考实现

查看现有 Handler 实现模式：
- `internal/api/handler/subscription.go` - 分组管理实现
- `internal/api/handler/download.go` - 下载管理
- `internal/api/handler/health.go` - 健康检查
