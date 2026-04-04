---
phase: 01-bug
plan: 02
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/service/calendar/calendar.go
autonomous: true
requirements:
  - BUG-02
must_haves:
  truths:
    - 日历中日历条目的 IsDownloaded 反映真实下载状态
    - 只有状态为 completed 的下载记录才视为已下载
    - downloading 状态不算已下载
  artifacts:
    - path: internal/service/calendar/calendar.go
      provides: "真实的 IsDownloaded 判断"
      contains: "GetBySubscriptionAndEpisode"
      contains2: "completed"
  key_links:
    - from: Calendar.GetWeekSchedule
      to: downloadRepo.GetBySubscriptionAndEpisode
      pattern: "GetBySubscriptionAndEpisode"
    - from: CalendarItem.IsDownloaded
      to: download.Status == "completed"
      pattern: 'status == "completed"'
---

<objective>
修复 calendar.go 中日历条目 IsDownloaded 字段硬编码为 false 的问题，改为查询 download repository 判断真实下载状态。

Purpose: 当前日历界面所有条目都显示为未下载，无法区分已下载和未下载的剧集，误导用户。
Output: 日历条目正确反映剧集是否已完成下载。
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/01-bug/01-CONTEXT.md
@internal/service/calendar/calendar.go
@internal/model/download.go
@internal/repository/download.go

<!-- Current broken code (calendar.go line 122-124): -->
```go
item := CalendarItem{
    // ...
    IsDownloaded: false, // TODO: 检查是否已下载
    // ...
}
```

<!-- Calendar struct (no download repo): -->
```go
type Calendar struct {
    subscriptionRepo repository.SubscriptionRepository
    notificationSvc  NotificationService
}
```

<!-- DownloadRepository interface relevant methods: -->
```go
GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error)
```

<!-- Download model status field: -->
```go
type Download struct {
    Status string // pending, downloading, stalled, completed, failed
}
```

<!-- Per D-03 and D-04: only "completed" status counts as downloaded -->
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Inject downloadRepo and fix IsDownloaded logic</name>
  <files>internal/service/calendar/calendar.go</files>
  <read_first>
    - internal/service/calendar/calendar.go (full file)
    - internal/repository/download.go (GetBySubscriptionAndEpisode signature)
  </read_first>
  <behavior>
    - Test 1: Calendar struct contains downloadRepo field of type repository.DownloadRepository
    - Test 2: NewCalendar accepts downloadRepo parameter
    - Test 3: IsDownloaded is true when GetBySubscriptionAndEpisode returns a download with Status == "completed"
    - Test 4: IsDownloaded is false when no download found for subscription+episode
    - Test 5: IsDownloaded is false when download exists but status is "downloading"
    - Test 6: IsDownloaded is false when download exists but status is "failed"
    - Test 7: IsDownloaded is false when download exists but status is "pending"
  </behavior>
  <action>
Modify `internal/service/calendar/calendar.go`:

1. Add `downloadRepo` field to Calendar struct:
   ```go
   type Calendar struct {
       subscriptionRepo repository.SubscriptionRepository
       downloadRepo     repository.DownloadRepository
       notificationSvc  NotificationService
   }
   ```

2. Update `NewCalendar` to accept downloadRepo:
   ```go
   func NewCalendar(subscriptionRepo repository.SubscriptionRepository, downloadRepo repository.DownloadRepository) *Calendar {
       return &Calendar{
           subscriptionRepo: subscriptionRepo,
           downloadRepo:     downloadRepo,
       }
   }
   ```

3. In `GetWeekSchedule`, around line 115-125, replace the hardcoded `IsDownloaded: false`:
   ```go
   isDownloaded := false
   if f.downloadRepo != nil {
       if download, err := f.downloadRepo.GetBySubscriptionAndEpisode(sub.ID, sub.CurrentEpisode+1); err == nil && download != nil {
           isDownloaded = download.Status == "completed"
       }
   }
   ```
   Then set `IsDownloaded: isDownloaded,` in the CalendarItem.

   Wait — `CurrentEpisode` is the latest collected episode. The calendar shows "下一集" (CurrentEpisode + 1). We want to check if episode `sub.CurrentEpisode+1` is downloaded. This is correct.

4. Find all callers of `NewCalendar` and update them to pass `downloadRepo`. Search: `NewCalendar(` across the codebase.
  </action>
  <verify>
    <automated>go build ./...</automated>
    <automated>grep -n "downloadRepo" internal/service/calendar/calendar.go | head -5</automated>
    <automated>grep -n "GetBySubscriptionAndEpisode" internal/service/calendar/calendar.go</automated>
    <automated>grep -n 'IsDownloaded: false' internal/service/calendar/calendar.go || echo "OK: hardcoded false removed"</automated>
  </verify>
  <acceptance_criteria>
    - `internal/service/calendar/calendar.go` contains `downloadRepo repository.DownloadRepository` in Calendar struct
    - `NewCalendar` signature includes `downloadRepo repository.DownloadRepository` parameter
    - `GetWeekSchedule` calls `GetBySubscriptionAndEpisode` with `sub.ID` and `sub.CurrentEpisode+1`
    - `IsDownloaded` assignment uses `download.Status == "completed"` comparison
    - Hardcoded `IsDownloaded: false` is removed
    - All `NewCalendar` callers are updated with the new parameter
  </acceptance_criteria>
  <done>
    - Calendar struct includes downloadRepo
    - IsDownloaded is computed from real download status
    - Only "completed" status yields true
    - All callers compile
    - `go build ./...` succeeds
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Calendar service -> Download repo | No untrusted input; episode number derived from subscription data |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-02-01 | Information Disclosure | CalendarItem | accept | IsDownloaded is read-only; no sensitive data exposed |
</threat_model>

<verification>
- `go test ./internal/service/calendar/...` passes (if tests exist)
- `go build ./...` succeeds
- IsDownloaded logic uses real download repository query
</verification>

<success_criteria>
- BUG-02 resolved: Calendar shows true download status
- IsDownloaded is true only for downloads with Status == "completed"
- downloading/pending/failed statuses show as false
</success_criteria>

<output>
After completion, create `.planning/phases/01-bug/01-02-fix-calendar-status-SUMMARY.md`
</output>
