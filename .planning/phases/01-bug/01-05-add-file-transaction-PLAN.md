---
phase: 01-bug
plan: 05
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/service/organizer/organizer.go
  - internal/model/download.go
autonomous: true
requirements:
  - BUG-05
must_haves:
  truths:
    - 文件移动和数据库状态在同一逻辑单元内保持一致
    - 崩溃后可检测不一致状态（organizing）并恢复
    - 文件操作失败时数据库状态回滚
  artifacts:
    - path: internal/service/organizer/organizer.go
      provides: "事务保护的文件移动"
      contains: "organizing"
      contains2: "transaction"
      contains3: "recover"
    - path: internal/model/download.go
      provides: "新的数据库状态"
      contains: '"organizing"'
  key_links:
    - from: organizer.organizeFile
      to: downloadRepo.UpdateInTx
      pattern: "UpdateInTx"
    - from: download.Status
      to: organizer state machine
      pattern: 'status.*"organizing"'
---

<objective>
为文件移动操作添加数据库事务保护。实现状态机（pending -> organizing -> completed/failed），崩溃后可根据数据库状态检测不一致并恢复。

Purpose: 当前 organizer.go 先移动文件再更新数据库，中间崩溃会导致文件已移动但数据库状态未更新，产生孤儿文件。
Output: 文件移动和数据库状态保持一致，崩溃后可自动恢复。
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/01-bug/01-CONTEXT.md
@internal/service/organizer/organizer.go
@internal/model/download.go
@internal/repository/download.go

<!-- Current file move flow (organizer.go lines 536-568): -->
```go
func (f *FileOrganizer) moveFile(src, dest string) error {
    // Handle duplicate dest
    // Try os.Rename
    // Fallback to copy + delete
    return nil
}
```

<!-- Current organizeFile flow (lines 250-332): -->
1. Parse filename
2. Match subscription
3. Generate new path
4. Create target directory
5. Move file via moveFile()
6. Log success

<!-- Download model status field: -->
```go
Status string // pending, downloading, stalled, completed, failed
```

<!-- DownloadRepository has transaction support: -->
```go
CreateInTx(tx *gorm.DB, download *model.Download) error
UpdateInTx(tx *gorm.DB, download *model.Download) error
```

<!-- Decisions D-10, D-11, D-12: -->
- D-10: 采用"先数据库，后文件"策略
- D-11: 数据库状态机：pending -> organizing -> completed/failed
- D-12: 崩溃后可根据数据库状态检测不一致（organizing 状态超时可重试）

<!-- Note: organizer.go does NOT currently have a reference to DownloadRepository. -->
<!-- The organizer creates new files from watch directory — it doesn't track downloads. -->
<!-- We need to add downloadRepo to FileOrganizer. -->
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add database transaction protection to file organizer</name>
  <files>internal/service/organizer/organizer.go, internal/model/download.go</files>
  <read_first>
    - internal/service/organizer/organizer.go (full file)
    - internal/model/download.go (status field and download model)
    - internal/repository/download.go (UpdateInTx signature)
  </read_first>
  <action>
Modify two files:

**File 1: internal/model/download.go**

Add a comment documenting the "organizing" status as a valid state (no schema change needed since Status is a string field):
No code change needed — the Status field is already a flexible string. Just ensure "organizing" is a recognized status.

Actually, let's document it properly. Add a constant for the organizing status near the top of the file or in the model package. But model/download.go doesn't have status constants. Let's add them:

```go
// Download status constants
const (
    DownloadStatusPending    = "pending"
    DownloadStatusDownloading = "downloading"
    DownloadStatusStalled    = "stalled"
    DownloadStatusCompleted  = "completed"
    DownloadStatusFailed     = "failed"
    DownloadStatusOrganizing = "organizing"
)
```

Wait, this might conflict with existing string literals. Better to just add the constant and use it in organizer.go.

**File 2: internal/service/organizer/organizer.go**

This is the main change. The organizer currently:
1. Watches directory for new files
2. Parses filename to extract title/episode
3. Matches against subscriptions
4. Moves file to organized location

We need to:
1. Add downloadRepo to FileOrganizer struct
2. After matching a subscription, look up or create a download record
3. Update download status through the state machine
4. Use transaction to ensure DB and filesystem consistency

But wait — the organizer watches the download directory and processes completed downloads. The files it processes don't necessarily have download records (e.g., manual downloads). We need to handle both cases.

Per D-10 "先数据库，后文件" strategy:
1. Find or create a download record for this file
2. Update status to "organizing" in a transaction
3. Move the file
4. Update status to "completed" with the new path
5. If file move fails, status stays "organizing" or rolls back to previous state

Implementation plan:

1. Add fields to FileOrganizer:
   ```go
   downloadRepo     repository.DownloadRepository
   db               *gorm.DB
   ```

2. Update constructor:
   ```go
   func NewFileOrganizer(
       watchDir string,
       destDir string,
       subscriptionRepo repository.SubscriptionRepository,
       downloadRepo repository.DownloadRepository,
       db *gorm.DB,
       bangumiService *bangumi.BangumiService,
       renameTemplate string,
   ) (*FileOrganizer, error)
   ```

3. In organizeFile, after matching subscription, find the download record:
   ```go
   var download *model.Download
   if subscription != nil {
       downloads, err := f.downloadRepo.GetBySubscriptionAndEpisodeWithLang(subscription.ID, info.Episode)
       if err == nil && len(downloads) > 0 {
           download = &downloads[0] // Use first match
       }
   }
   ```

4. Wrap file move in a transaction:
   ```go
   err := f.db.Transaction(func(tx *gorm.DB) error {
       if download != nil {
           download.Status = model.DownloadStatusOrganizing
           if err := f.downloadRepo.UpdateInTx(tx, download); err != nil {
               return fmt.Errorf("failed to set organizing status: %w", err)
           }
       }
       
       // Now move the file (outside transaction but after DB update)
       // Actually "先数据库，后文件" means update DB first, then move file
       return nil
   })
   ```

   Wait, we can't put the file move inside the DB transaction because the file operation is not transactional. The correct approach per D-10:
   
   a. Update DB to "organizing" state
   b. Move file
   c. Update DB to "completed" with new path
   d. If (b) fails, update DB to "failed" or keep "organizing" for retry

   For crash recovery (D-12): if we find downloads with "organizing" status on startup, we can attempt to recover them.

   Let's implement this:

   ```go
   // Step 1: Set organizing status in DB
   if download != nil {
       download.Status = model.DownloadStatusOrganizing
       if err := f.downloadRepo.Update(download); err != nil {
           return fmt.Errorf("failed to set organizing status: %w", err)
       }
   }

   // Step 2: Move file
   if err := f.moveFile(filePath, newPath); err != nil {
       // Update DB to failed status
       if download != nil {
           download.Status = model.DownloadStatusFailed
           download.LastError = err.Error()
           f.downloadRepo.Update(download)
       }
       return fmt.Errorf("failed to move file: %w", err)
   }

   // Step 3: Update DB to completed
   if download != nil {
       download.Status = model.DownloadStatusCompleted
       download.FilePath = newPath
       if err := f.downloadRepo.Update(download); err != nil {
           // File moved but DB update failed — log for manual recovery
           logger.Error("File moved but DB update failed",
               "download_id", download.ID,
               "new_path", newPath,
               "error", err)
           return fmt.Errorf("file moved but failed to update database: %w", err)
       }
   }
   ```

5. Add a recovery method for startup:
   ```go
   // RecoverOrganizingDownloads 恢复处于 organizing 状态的下载
   func (f *FileOrganizer) RecoverOrganizingDownloads() error {
       // This would need a method to query by status
       // For now, document that this should be called on startup
       logger.Info("Checking for downloads in organizing state that need recovery")
       // Implementation deferred — requires new repository method
       return nil
   }
   ```

Actually, for D-12 crash recovery, we don't need to implement it fully now. We just need to ensure the "organizing" state EXISTS in the database so that a future recovery mechanism can detect it.

Let me simplify the implementation:

1. Add downloadRepo and db to FileOrganizer
2. In organizeFile, after successful subscription match and before moving:
   - Find the download record
   - Update status to "organizing"
3. After successful file move:
   - Update status to "completed"
   - Set FilePath to new path
4. On file move failure:
   - Update status to "failed"
   - Set LastError

This gives us the state machine: pending/downloading -> organizing -> completed/failed

For recovery on startup: downloads stuck in "organizing" can be detected and retried. We'll add a comment/doc about this.

Also need to update all callers of NewFileOrganizer.

And add the status constant to model/download.go.
  </action>
  <verify>
    <automated>go build ./...</automated>
    <automated>grep -n "downloadRepo" internal/service/organizer/organizer.go | head -5</automated>
    <automated>grep -n "DownloadStatusOrganizing\|\"organizing\"" internal/service/organizer/organizer.go</automated>
    <automated>grep -n "DownloadStatusOrganizing" internal/model/download.go</automated>
  </verify>
  <acceptance_criteria>
    - `internal/model/download.go` contains `DownloadStatusOrganizing = "organizing"` constant
    - `internal/service/organizer/organizer.go` has `downloadRepo repository.DownloadRepository` field
    - `internal/service/organizer/organizer.go` has `db *gorm.DB` field
    - `NewFileOrganizer` accepts `downloadRepo` and `db` parameters
    - `organizeFile` sets download status to `model.DownloadStatusOrganizing` before moving file
    - `organizeFile` sets download status to `model.DownloadStatusCompleted` and `FilePath` after successful move
    - On move failure, status is set to `model.DownloadStatusFailed` with `LastError`
    - All `NewFileOrganizer` callers updated with new parameters
  </acceptance_criteria>
  <done>
    - File organizer uses database state machine for file moves
    - "organizing" status used during file move
    - "completed" status with FilePath set after success
    - "failed" status on move failure
    - Project builds successfully
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| File system -> Database | File operations and DB updates must remain consistent |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-05-01 | Denial of Service | Crash during file move | mitigate | "organizing" state allows recovery detection on restart |
| T-05-02 | Information Disclosure | File paths in DB | accept | File paths are internal, no sensitive data |
</threat_model>

<verification>
- `go build ./...` succeeds
- `grep "DownloadStatusOrganizing" internal/model/download.go` returns a match
- `grep "downloadRepo" internal/service/organizer/organizer.go` returns matches
</verification>

<success_criteria>
- BUG-05 resolved: File moves are protected by database state machine
- Downloads transition through "organizing" state during file move
- Crash recovery possible via "organizing" status detection
- File path recorded in database on successful move
</success_criteria>

<output>
After completion, create `.planning/phases/01-bug/01-05-add-file-transaction-SUMMARY.md`
</output>
