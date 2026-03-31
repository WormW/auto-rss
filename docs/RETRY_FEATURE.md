# 智能失败重试功能文档

## 功能概述

当下载任务失败时，系统会自动进行重试，使用指数退避策略避免频繁请求，同时减少人工干预。

## 核心特性

### 1. 指数退避重试策略

| 重试次数 | 等待时间 | 说明 |
|---------|---------|------|
| 第 0 次 | 1 分钟 | 首次重试（网络瞬时故障） |
| 第 1 次 | 2 分钟 | DNS 解析问题 |
| 第 2 次 | 4 分钟 | 种子源暂时不可用 |
| 第 3 次 | 8 分钟 | 源站维护 |
| 第 4 次 | 16 分钟 | 连接超时 |
| 第 5+ 次 | 30 分钟 | 最大间隔（封顶） |

### 2. 智能错误分类

**可重试的错误**（临时性问题）：
- 连接超时
- 网络错误
- DNS 解析失败
- 服务器 5xx 错误

**不可重试的错误**（永久性问题）：
- 种子被禁用（banned）
- 种子未注册（unregistered）
- 账户被封禁
- 磁盘已满
- 权限拒绝

### 3. 重试状态跟踪

每个下载任务记录以下重试信息：
- `retry_count`: 已重试次数
- `max_retries`: 最大重试次数（默认 5）
- `next_retry_at`: 下次重试时间
- `last_error`: 最后一次错误信息
- `retry_reason`: 重试原因

## 工作流程

```
下载失败
    ↓
检查错误类型
    ↓
┌─────────────────────────────────┐
│ 可重试？                         │
│   ├─ 否 → 标记为最终失败，通知用户 │
│   └─ 是 → 计算下次重试时间         │
└─────────────────────────────────┘
    ↓
设置状态为 failed，记录重试信息
    ↓
监控服务定期检查可重试任务
    ↓
到达重试时间？
    ↓
重置状态为 pending，加入下载队列
    ↓
重新尝试下载
    ↓
成功？ → 完成
失败？ → 进入下一轮重试
```

## 配置方式

### 任务级别配置

每个下载任务可以设置最大重试次数（通过 API）：

```json
{
  "title": "芙莉莲 - 12",
  "torrent_url": "...",
  "max_retries": 5  // 最大重试次数，0 表示不重试
}
```

### 系统默认配置

- 默认最大重试次数：5 次
- 重试间隔：指数退避，最大 30 分钟

## API 接口

### 手动触发重试

```bash
POST /api/v1/downloads/:id/retry
```

### 获取重试统计

```bash
GET /api/v1/downloads/retry-stats
```

## 日志输出

```
# 准备重试
INFO Download prepared for retry
    download_id=123
    retry_count=2
    max_retries=5
    next_retry_at=2026-04-01 12:05:00
    reason=qbittorrent_add_failed

# 跳过重试（时间未到）
DEBUG Skipping retry for download
    download_id=123
    reason=retry_time_not_reached
    retry_count=1

# 达到最大重试次数
WARN Download marked as failed
    download_id=123
    retry_count=5
    max_retries=5
    error=connection timeout
```

## 数据库变更

### downloads 表新增字段

```sql
retry_count    INTEGER DEFAULT 0
max_retries    INTEGER DEFAULT 5
next_retry_at  DATETIME
last_error     TEXT
retry_reason   VARCHAR(50)
```

创建索引优化重试查询：
```sql
CREATE INDEX idx_downloads_retry ON downloads(status, retry_count, next_retry_at);
```

## 向后兼容

- 未设置 `max_retries` 的任务默认使用 5 次
- 现有失败任务不会自动重试（需要手动触发或重新添加）
- 不影响现有功能，纯增量增强

## 注意事项

1. **重试不等于无限循环**：达到最大重试次数后任务会被标记为最终失败
2. **错误分类是启发式的**：部分错误可能被误判，需要根据实际情况调整
3. **重试时间不是精确的**：受监控服务检查间隔影响，可能有几分钟的延迟
4. **通知策略**：只在最终失败时发送通知，避免频繁打扰用户

## 未来扩展

- [ ] Web UI 重试管理页面
- [ ] 手动重试按钮
- [ ] 批量重试功能
- [ ] 重试历史记录
- [ ] 自定义重试间隔配置
- [ ] 多源自动切换（与源切换功能结合）
