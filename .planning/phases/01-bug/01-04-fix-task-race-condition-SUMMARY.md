---
plan: 01-04-fix-task-race-condition
phase: 01-bug
status: completed
completed_at: "2025-04-05"
---

# Summary: Fix Task Manager Race Condition

## What Was Built

Fixed race condition in Task Manager's CancelTask method:

1. **Explicit nil checks**: Check currentTask is not nil before accessing fields
2. **Separate status checks**: Split combined condition into separate checks
3. **CancelFunc validation**: Ensure cancelFunc is not nil before calling
4. **Helper method**: Added IsTaskRunning for safe concurrent task status checks

## Key Changes

- `internal/service/task/manager.go`: Rewrote CancelTask with explicit nil guards

## Self-Check: PASSED

- All tasks completed
- Code committed
