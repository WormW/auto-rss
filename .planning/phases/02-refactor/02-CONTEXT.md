# Phase 2: 代码重构 - Context

**Gathered:** 2026-04-05
**Status:** Ready for planning

<domain>
## Phase Boundary

拆分三个臃肿代码文件（subscription.go 2345行、monitor.go 959行、organizer.go 755行）为更小、更专注的服务，提升代码可维护性和可测试性。

目标行数：
- subscription.go: 2345行 → < 800行
- monitor.go: 959行 → < 500行
- organizer.go: 755行 → < 400行

</domain>

<decisions>
## Implementation Decisions

### 服务拆分策略
- **D-01:** 采用功能垂直拆分方式
  - `bangumi/enrich.go` - Bangumi 元数据获取服务（从 subscription.go 提取）
  - `renamer/service.go` - 文件重命名服务（从 subscription.go 和 monitor.go 提取）
  - `subscription/batch.go` - 批量导入服务（从 subscription.go 提取）
  - `subscription/collection.go` - 集合下载服务（从 subscription.go 提取）
  - `downloader/status_sync.go` - 状态同步组件（从 monitor.go 提取）
  - `downloader/completion_handler.go` - 完成处理组件（从 monitor.go 提取）
  - `downloader/retry_service.go` - 重试服务（从 monitor.go 提取）
  - `organizer/parser.go` - 文件名解析器（从 organizer.go 提取）
  - `organizer/matcher.go` - 剧集匹配器（从 organizer.go 提取）
  - `organizer/mover.go` - 文件移动器（从 organizer.go 提取）

### 向后兼容性
- **D-02:** 严格保持现有 API 和行为不变
  - Handler 方法签名完全不变
  - API 响应格式完全不变
  - 仅内部实现改为调用新服务
  - 零破坏性变更

### 错误处理策略
- **D-03:** 使用简单原始错误 + 日志记录
  - 服务返回原始错误
  - 在关键点使用 logger 记录上下文
  - 不引入自定义错误类型或错误码体系
  - 保持与现有代码风格一致

### 服务接口设计
- **D-04:** 每个服务定义一个主接口
  - `BangumiEnricher` 接口：Enrich(subscription) error
  - `RenamerService` 接口：RenameFile(), RenameCollection() 方法
  - `BatchImporter` 接口：Import(items) error
  - `CollectionDownloader` 接口：Download(subscription) error
  - `StatusSync` 接口：Sync(), UpdateStatus() 方法
  - `CompletionHandler` 接口：HandleComplete(download) error
  - `RetryService` 接口：ShouldRetry(), PrepareRetry() 方法
  - 便于 mock 测试和依赖注入

### Claude's Discretion
- 日志消息的具体内容（保持与现有风格一致即可）
- 接口方法的具体命名（遵循 Go 命名规范）
- 服务间的依赖注入方式（构造函数注入或 setter 注入）

</decisions>

<canonical_refs>
## Canonical References

### 重构目标文件
- `internal/api/handler/subscription.go` - 当前 2354 行，需拆分
- `internal/service/downloader/monitor.go` - 当前 959 行，需拆分
- `internal/service/organizer/organizer.go` - 当前 755 行，需拆分

### 现有服务参考
- `internal/service/downloader/retry.go` - 已实现的重试服务模式
- `internal/service/organizer/parser.go` - 已提取的文件名解析器

### 需求文档
- `.planning/REQUIREMENTS.md` - REF-01 ~ REF-10 详细需求

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `downloader.RenameService` - 已存在的重命名服务，可在多个地方复用
- `downloader.RetryService` - 已存在的重试服务，monitor 可直接使用
- `organizer.FileNameParser` - 已存在的文件名解析器
- `task.GetManager()` - 全局任务管理器，异步操作使用

### Established Patterns
- 服务通过构造函数接收依赖（如 `NewDownloadMonitor`）
- 使用接口定义服务契约（如 `QBittorrentClient`）
- Handler 层保持 thin，业务逻辑下沉到 service 层
- 异步操作通过 task manager 启动

### Integration Points
- Handler 通过 `router.go` 初始化，需更新构造函数调用
- 服务间依赖通过接口解耦，便于测试
- 数据库操作通过 repository 接口

</code_context>

<specifics>
## Specific Ideas

- 参考 `downloader/retry.go` 的实现模式提取其他服务
- 订阅 handler 中的 `doCollectEpisodes` 方法很长，需要仔细拆分
- monitor 中的状态映射逻辑 `mapQBStateToStatus` 保持独立函数
- organizer 的文件匹配逻辑 `findMatchingSubscription` 移到 matcher 组件

</specifics>

<deferred>
## Deferred Ideas

- 优化 API 设计（需要前端配合，超出当前阶段范围）
- 引入错误码体系（当前阶段保持简单）
- 服务间的异步消息通信（当前阶段保持同步调用）

</deferred>

---

*Phase: 02-refactor*
*Context gathered: 2026-04-05*
