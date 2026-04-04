---
phase: 01-bug
plan: 03
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/service/disk/monitor.go
  - internal/service/scheduler/scheduler.go
autonomous: true
requirements:
  - BUG-03
must_haves:
  truths:
    - 磁盘空间危险时新下载任务被暂停
    - 磁盘空间恢复时下载任务自动恢复
    - scheduler 在添加下载前检查暂停标志
    - 不需要持久化，服务重启后自动恢复
  artifacts:
    - path: internal/service/disk/monitor.go
      provides: "暂停/恢复实现"
      contains: "atomic.Bool"
      contains2: "Load()"
      contains3: "Store(true)"
    - path: internal/service/scheduler/scheduler.go
      provides: "暂停检查"
      contains: "downloadPaused"
      contains2: "IsPaused()"
  key_links:
    - from: scheduler.processDownloadItem
      to: disk.IsPaused()
      pattern: "IsPaused"
    - from: disk.Monitor
      to: global atomic flag
      pattern: "downloadPaused"
---

<objective>
实现磁盘监控的 pauseDownloads() 和 resumeDownloads() 功能。使用全局原子标志，scheduler 在添加新下载前检查该标志。

Purpose: 当前 pauseDownloads() 和 resumeDownloads() 是空实现，磁盘空间不足时无法阻止新下载，导致磁盘完全填满。
Output: 完整的磁盘空间保护机制，危险时自动暂停，恢复时自动继续。
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/01-bug/01-CONTEXT.md
@internal/service/disk/monitor.go
@internal/service/scheduler/scheduler.go

<!-- Current empty implementation (monitor.go lines 517-527): -->
```go
// pauseDownloads 暂停新下载
func (m *Monitor) pauseDownloads() {
    logger.Info("Pausing new downloads due to critical disk space")
    // TODO: 设置一个标志位或配置，让 scheduler 暂停添加新下载
}

// resumeDownloads 恢复下载
func (m *Monitor) resumeDownloads() {
    logger.Info("Resuming downloads after disk space recovered")
    // TODO: 清除暂停标志
}
```

<!-- Scheduler's processDownloadItem (line 439-531): this is where downloads are added to qBittorrent -->
```go
func (s *scheduler) processDownloadItem(sub *model.Subscription, item *rss.RSSItem, replaceDownloadID uint) (bool, error) {
    // Creates download record in DB, then adds to qBittorrent
}
```

<!-- Decisions D-05, D-06, D-07: -->
- D-05: Use `var downloadPaused atomic.Bool`
- D-06: scheduler checks flag before adding new download
- D-07: No persistence needed, auto-resume on restart
</context>

<tasks>

<task type="auto">
  <name>Task 1: Implement global atomic pause flag and scheduler check</name>
  <files>internal/service/disk/monitor.go, internal/service/scheduler/scheduler.go</files>
  <read_first>
    - internal/service/disk/monitor.go (lines 1-20 for imports, lines 517-527 for empty methods)
    - internal/service/scheduler/scheduler.go (lines 1-20 for imports, lines 439-470 for processDownloadItem entry)
  </read_first>
  <action>
Modify two files:

**File 1: internal/service/disk/monitor.go**

1. Add `sync/atomic` import at the top.

2. Add a package-level variable after the constants block:
   ```go
   var downloadPaused atomic.Bool
   ```

3. Replace `pauseDownloads()` (line 518-521) with:
   ```go
   func (m *Monitor) pauseDownloads() {
       if downloadPaused.CompareAndSwap(false, true) {
           logger.Info("Pausing new downloads due to critical disk space")
           // Notify user
           m.sendCriticalNotificationWithPauseInfo(m.GetDiskInfo(m.getDownloadPath()))
       }
   }
   ```
   Actually, the existing `sendCriticalNotification` already says "新下载任务已暂停". Just use the existing one, or enhance it. The simplest:
   ```go
   func (m *Monitor) pauseDownloads() {
       if downloadPaused.CompareAndSwap(false, true) {
           logger.Info("Pausing new downloads due to critical disk space")
       }
   }
   ```

4. Replace `resumeDownloads()` (line 524-527) with:
   ```go
   func (m *Monitor) resumeDownloads() {
       if downloadPaused.CompareAndSwap(true, false) {
           logger.Info("Resuming downloads after disk space recovered")
       }
   }
   ```

5. Add a public accessor method for the scheduler to check:
   ```go
   // IsDownloadsPaused 检查下载是否被暂停
   func IsDownloadsPaused() bool {
       return downloadPaused.Load()
   }
   ```

**File 2: internal/service/scheduler/scheduler.go**

1. Add the disk package import:
   ```go
   "github.com/WormW/auto-rss/internal/service/disk"
   ```

2. In `processDownloadItem`, right at the beginning (before creating the download struct at line 441), add:
   ```go
   // Check if downloads are paused due to critical disk space
   if disk.IsDownloadsPaused() {
       logger.Info("Skipping download creation because downloads are paused",
           "subscription", sub.Name,
           "title", item.Title)
       return false, nil
   }
   ```
  </action>
  <verify>
    <automated>go build ./...</automated>
    <automated>grep -n "atomic.Bool" internal/service/disk/monitor.go</automated>
    <automated>grep -n "downloadPaused" internal/service/disk/monitor.go</automated>
    <automated>grep -n "IsDownloadsPaused" internal/service/disk/monitor.go</automated>
    <automated>grep -n "IsDownloadsPaused" internal/service/scheduler/scheduler.go</automated>
    <automated>grep -n "disk.IsDownloadsPaused" internal/service/scheduler/scheduler.go</automated>
  </verify>
  <acceptance_criteria>
    - `internal/service/disk/monitor.go` imports `sync/atomic`
    - `internal/service/disk/monitor.go` has package-level `var downloadPaused atomic.Bool`
    - `pauseDownloads()` calls `downloadPaused.CompareAndSwap(false, true)`
    - `resumeDownloads()` calls `downloadPaused.CompareAndSwap(true, false)`
    - `IsDownloadsPaused()` function exists and returns `downloadPaused.Load()`
    - `internal/service/scheduler/scheduler.go` imports `"github.com/WormW/auto-rss/internal/service/disk"`
    - `processDownloadItem` calls `disk.IsDownloadsPaused()` before creating download records
    - When paused, `processDownloadItem` returns `false, nil` with a log message containing "Skipping download creation because downloads are paused"
  </acceptance_criteria>
  <done>
    - Global atomic flag controls download pause state
    - pauseDownloads sets flag, resumeDownloads clears flag
    - Scheduler checks flag before adding new downloads
    - Both files compile, project builds
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Monitor -> Scheduler | Atomic flag is the only shared state; no data crosses boundaries |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-01 | Denial of Service | pauseDownloads | mitigate | CompareAndSwap prevents double-logging; atomic.Bool is race-safe |
| T-03-02 | Tampering | Global flag | accept | Flag is read-only from scheduler; only Monitor can modify it |
</threat_model>

<verification>
- `go build ./...` succeeds
- `grep "IsDownloadsPaused" internal/service/scheduler/scheduler.go` returns at least one match
- `grep "downloadPaused" internal/service/disk/monitor.go` returns multiple matches
</verification>

<success_criteria>
- BUG-03 resolved: Disk monitor pause/resume functions are fully implemented
- Global atomic flag controls download pause state
- Scheduler checks flag before adding new downloads
- No persistence needed (D-07), service restart auto-resumes
</success_criteria>

<output>
After completion, create `.planning/phases/01-bug/01-03-implement-disk-pause-SUMMARY.md`
</output>
