---
phase: 01-bug
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/api/handler/download.go
autonomous: true
requirements:
  - BUG-01
must_haves:
  truths:
    - Retry 接口触发完整重试流程
    - 旧种子从 qBittorrent 中删除
    - 重试计数重置为 0
    - 下载任务立即重新加入 qBittorrent
  artifacts:
    - path: internal/api/handler/download.go
      provides: "完整重试逻辑"
      contains: "RetryCount = 0"
      contains2: "DeleteTorrent"
      contains3: "AddTorrent"
  key_links:
    - from: Retry handler
      to: qbClient.DeleteTorrent
      pattern: "DeleteTorrent.*download.TorrentHash"
    - from: Retry handler
      to: qbClient.AddTorrent
      pattern: "AddTorrent.*download.TorrentURL"
---

<objective>
修复 DownloadHandler.Retry 接口：实现完整重试流程——删除旧种子、重置重试计数、重新添加到 qBittorrent。

Purpose: 当前 Retry 接口仅将状态改为 pending，未真正重新下载，用户点击重试无效。
Output: 完整的 Retry 处理逻辑，支持即时重新下载。
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/01-bug/01-CONTEXT.md
@internal/api/handler/download.go
@internal/model/download.go

<!-- Key interface from download.go -->
```go
type DownloadHandler struct {
    repo     repository.DownloadRepository
    qbClient downloader.QBittorrentClient
}
```

<!-- Current broken Retry (line 136-159): -->
```go
func (h *DownloadHandler) Retry(c *gin.Context) {
    // ... parse id ...
    // TODO: 实现重试逻辑
    if err := h.repo.UpdateStatus(uint(id), "pending"); err != nil {
        // return 500
    }
    c.JSON(200, ...)
}
```

<!-- Existing qbClient methods used in other handlers: -->
- `h.qbClient.DeleteTorrent(hash, true) error` — used in Delete handler
- `h.qbClient.AddTorrent(torrentURL, downloadPath, category) (string, error)` — used in scheduler

<!-- Download model relevant fields: -->
```go
type Download struct {
    ID          uint
    TorrentHash string
    TorrentURL  string
    Status      string // pending, downloading, stalled, completed, failed
    RetryCount  int    // default 0
    MaxRetries  int    // default 5
    NextRetryAt *time.Time
    LastError   string
    RetryReason string
}
```
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Implement full Retry handler logic</name>
  <files>internal/api/handler/download.go</files>
  <read_first>
    - internal/api/handler/download.go (current state, lines 136-159)
    - internal/model/download.go (model fields)
  </read_first>
  <behavior>
    - Test 1: Retry handler fetches download by ID before attempting any action
    - Test 2: Retry handler returns 404 if download not found
    - Test 3: If TorrentHash != "" and qbClient != nil, DeleteTorrent is called
    - Test 4: RetryCount is reset to 0, Status set to "pending", RetryReason set to "user_retry"
    - Test 5: If qbClient != nil, AddTorrent is called with TorrentURL and download path
    - Test 6: On AddTorrent success, status updated to "downloading", TorrentHash updated
    - Test 7: On AddTorrent failure, status stays "failed", error logged
  </behavior>
  <action>
Replace the TODO Retry handler (lines 146-158) with the following complete implementation:

1. After parsing `id`, fetch the download record: `download, err := h.repo.GetByID(uint(id))`
2. Return 404 if download not found (same pattern as Delete handler)
3. If `download.TorrentHash != ""` and `h.qbClient != nil`:
   - Call `h.qbClient.DeleteTorrent(download.TorrentHash, true)` to remove old torrent
   - Log with `logger.Info("Deleting old torrent for retry", "download_id", id, "hash", download.TorrentHash)`
   - Ignore deletion error (old torrent may not exist) but log it
4. Reset retry-related fields on the download record:
   - `download.RetryCount = 0`
   - `download.RetryReason = "user_retry"`
   - `download.NextRetryAt = nil`
   - `download.LastError = ""`
   - `download.Status = "pending"`
   - `download.TorrentHash = ""` (clear old hash)
5. Call `h.repo.Update(download)` to persist the reset
6. If `h.qbClient != nil`:
   - Build download path: same pattern as `Delete` handler and `subscription.go` line 421 — get savePath from `h.downloadPath` field, generate with `utils.GenerateDownloadPath(savePath, download.Subscription.Name)` (but Note: download handler does not have `downloadPath` field — use a fixed path approach)
   
   Actually, check: DownloadHandler struct does NOT have downloadPath. The scheduler builds path via `utils.GenerateDownloadPath(basePath, sub.Name)`. We need to get basePath from config. Use the same pattern from scheduler: get from `s.configRepo.Get("download_path")`.
   
   Wait — DownloadHandler struct also does NOT have configRepo. The DownloadHandler has: repo, qbClient. We need to add configRepo to the struct.

   CORRECTION: Per D-01 decision "完整重试流程：先从 qBittorrent 删除旧种子 -> 重置计数 -> 调用 scheduler 重新添加下载任务" and D-02 "用户主动点击重试时应立即执行，不等待定时任务" — the simplest approach: after resetting the download record to pending, the existing scheduler will pick it up on its next scan. But D-02 says immediate, not wait for scheduler.
   
   The approach: Directly call AddTorrent like scheduler does. For the download path, we need access to the config. We must inject configRepo into DownloadHandler.

   ACTION: Add `configRepo repository.ConfigRepository` field to DownloadHandler struct, update NewDownloadHandler constructor to accept it. Update all callers.

   Then in Retry handler:
   - Get basePath: check h.configRepo.Get("download_path"), fallback to constants.DefaultDownloadPath
   - Get subscription name: `download.Subscription.Name` (Subscription is preloaded via repo.GetByID)
   - `downloadPath := utils.GenerateDownloadPath(basePath, download.Subscription.Name)`
   - `torrentHash, err := h.qbClient.AddTorrent(download.TorrentURL, downloadPath, "")`
   - On success: `download.Status = "downloading"`, `download.TorrentHash = torrentHash`
   - On failure: `download.Status = "failed"`, `download.LastError = err.Error()`
7. Final `h.repo.Update(download)` to persist the final state
8. Return 200 with success message

IMPORTANT: Add `configRepo` to DownloadHandler and update its constructor. Search for all `NewDownloadHandler` calls and update them.
  </action>
  <verify>
    <automated>go build ./...</automated>
    <automated>grep -n "configRepo" internal/api/handler/download.go | head -5</automated>
    <automated>grep -n "DeleteTorrent" internal/api/handler/download.go | grep -i retry</automated>
    <automated>grep -n "AddTorrent" internal/api/handler/download.go | grep -i retry</automated>
  </verify>
  <acceptance_criteria>
    - `internal/api/handler/download.go` contains `configRepo repository.ConfigRepository` in DownloadHandler struct
    - `NewDownloadHandler` accepts `configRepo repository.ConfigRepository` as parameter
    - Retry handler calls `h.repo.GetByID(uint(id))` before any modification
    - Retry handler calls `h.qbClient.DeleteTorrent(download.TorrentHash, true)` when TorrentHash is non-empty
    - Retry handler sets `download.RetryCount = 0`
    - Retry handler sets `download.Status = "pending"` initially, then `"downloading"` after AddTorrent success
    - Retry handler calls `h.qbClient.AddTorrent(download.TorrentURL, downloadPath, "")`
    - All `NewDownloadHandler` callers updated with new parameter
  </acceptance_criteria>
  <done>
    - Retry handler performs complete retry: delete old torrent, reset counters, add new torrent
    - Project builds successfully
    - All NewDownloadHandler callers compile
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| API -> qBittorrent | Untrusted user input (download ID) crosses to external service |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-01-01 | Denial of Service | Retry handler | mitigate | ID validated via ParseUint before use; no unbounded loops |
| T-01-02 | Information Disclosure | Error messages | accept | Error messages are generic ("Failed to retry download"); no sensitive data in responses |
</threat_model>

<verification>
- `go test ./internal/api/handler/...` passes
- `go build ./...` succeeds
- Retry handler contains complete flow: GetByID -> DeleteTorrent -> reset fields -> Update -> AddTorrent -> Update
</verification>

<success_criteria>
- BUG-01 resolved: Retry interface now triggers complete retry flow
- Old torrent deleted from qBittorrent
- Retry count reset to 0
- New torrent added to qBittorrent
- Download status transitions: pending -> downloading on success
</success_criteria>

<output>
After completion, create `.planning/phases/01-bug/01-01-fix-retry-logic-SUMMARY.md`
</output>
