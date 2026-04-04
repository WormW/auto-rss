# Phase 1: Bug 修复与安全 - Context

**Gathered:** 2025-04-05
**Status:** Ready for planning

<domain>
## Phase Boundary

修复 7 个已知 Bug 和安全问题，确保系统稳定运行：
- BUG-01 ~ BUG-05：功能缺陷修复
- SEC-01 ~ SEC-02：安全隐患消除

不添加新功能，只修复现有代码问题。

</domain>

<decisions>
## Implementation Decisions

### BUG-01 重试逻辑修复 (download.go:146)
- **D-01:** 完整重试流程：先从 qBittorrent 删除旧种子 → 重置重试计数 → 调用 scheduler 重新添加下载任务
- **D-02:** 用户主动点击"重试"时应立即执行，不等待定时任务

### BUG-02 日历下载状态修复 (calendar.go:123)
- **D-03:** 严格判断：只查询数据库中状态为 `completed` 的下载记录才算"已下载"
- **D-04:** 不将 `downloading` 状态算作已下载，避免误导用户

### BUG-03 磁盘监控暂停功能 (monitor.go:520-527)
- **D-05:** 使用全局原子变量 `var downloadPaused atomic.Bool`
- **D-06:** scheduler 在添加新下载任务前检查该标志，如为 true 则跳过
- **D-07:** 不需要持久化，服务重启后自动恢复下载

### BUG-04 Task Manager 竞态条件修复 (manager.go:115-160)
- **D-08:** 重排锁顺序：在 `CancelTask` 中持有锁期间检查 `currentTask` 状态
- **D-09:** 确保任务已完成（`currentTask == nil` 或状态非 running）时不调用 `cancelFunc`

### BUG-05 文件移动事务保护 (organizer.go:536-567)
- **D-10:** 采用"先数据库，后文件"策略
- **D-11:** 数据库状态机：pending → organizing → completed/failed
- **D-12:** 崩溃后可根据数据库状态检测不一致（organizing 状态超时可重试）

### SEC-01 路径遍历防护 (subscription.go, organizer.go)
- **D-13:** 路径规范化：使用 `filepath.Clean()` 清理用户输入路径
- **D-14:** 前缀检查：验证最终路径是否以允许的根目录为前缀
- **D-15:** 允许合理的子目录（如 `Anime/Action/`），不只是禁止 `..`

### SEC-02 SQL 注入审计 (repository/*.go)
- **D-16:** 重点审计动态条件查询（List 的 status 过滤等）
- **D-17:** 确认使用 GORM 参数化查询：`Where("status = ?", status)`
- **D-18:** 检查是否有 `Raw()` 或 `Exec()` 的字符串拼接

### Claude's Discretion
- 具体错误信息格式
- 日志输出级别选择
- 超时时长的具体数值

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### 相关代码文件
- `internal/api/handler/download.go` — BUG-01 Retry 接口
- `internal/service/calendar/calendar.go` — BUG-02 IsDownloaded 逻辑
- `internal/service/disk/monitor.go` — BUG-03 pauseDownloads/resumeDownloads
- `internal/service/task/manager.go` — BUG-04 竞态条件修复
- `internal/service/organizer/organizer.go` — BUG-05 文件移动事务
- `internal/api/handler/subscription.go` — SEC-01 路径输入验证
- `internal/repository/download.go` — SEC-02 SQL 注入审计

### 现有模式参考
- `internal/service/scheduler/scheduler.go` — scheduler 实现，需集成暂停检查
- `internal/repository/*.go` — GORM 查询模式参考

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `scheduler` 已有定期扫描 pending 下载的机制
- `repository.DownloadRepository` 已支持事务方法 `CreateInTx`, `UpdateInTx`
- `filepath.Clean()` 和 `filepath.IsLocal()` 可用于路径安全

### Established Patterns
- 使用 GORM 链式查询，参数化条件
- 使用 `sync.RWMutex` 保护共享状态
- 使用 `atomic.Bool` 作为简单标志位（已有 precedent）

### Integration Points
- BUG-01 需要调用 scheduler 的添加任务方法
- BUG-03 需要在 scheduler 的调度循环中检查暂停标志
- BUG-05 状态机需要与现有的 download status 字段兼容

</code_context>

<specifics>
## Specific Ideas

- Retry 时应重置 `retry_count` 为 0，让用户有完整的重试机会
- 磁盘暂停时应在通知中明确告知用户原因和恢复条件
- 文件事务保护可以考虑添加 `organized_path` 字段记录最终位置

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 01-bug*
*Context gathered: 2025-04-05*
