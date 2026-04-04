---
plan: 01-06-prevent-path-traversal
phase: 01-bug
status: completed
completed_at: "2025-04-05"
---

# Summary: Prevent Path Traversal

## What Was Built

Implemented path traversal protection for file operations:

1. **ValidatePath Utility**: Added to `internal/pkg/utils/path.go`
   - Uses `filepath.Clean()` for path normalization
   - Uses prefix checking with separator suffix for proper containment
   - Returns error if path escapes allowed root directory

2. **Organizer Validation**:
   - Validate source file path is within `watchDir`
   - Validate generated destination path is within `destDir`
   - Block `../` sequences that would escape allowed directories

3. **Subscription Handler Validation**:
   - Validate generated download paths in `downloadCollectionTorrent`
   - Validate generated download paths in RSS processing
   - Prevent subscription names with `../` from escaping download directory

## Key Changes

- `internal/pkg/utils/path.go`: Added `ValidatePath()` and `ValidatePathOrDefault()` functions
- `internal/service/organizer/organizer.go`: Added path validation in organizeFile()
- `internal/api/handler/subscription.go`: Added path validation before torrent downloads

## Self-Check: PASSED

- All tasks completed
- Code committed
- Builds successfully
