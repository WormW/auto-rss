---
plan: 01-02-fix-calendar-status
phase: 01-bug
status: completed
completed_at: "2025-04-05"
---

# Summary: Fix Calendar Status

## What Was Built

Fixed calendar entries `IsDownloaded` field that was hardcoded to `false`:

1. **Enhanced Calendar Service**: Added `downloadRepo` field to query actual download status
2. **Real Status Query**: Modified `GetWeekSchedule` to check if each episode exists in download repository
3. **Updated Handler**: Modified `CalendarHandler` and `NewCalendarHandler` to accept download repository
4. **Comprehensive Tests**: Added `calendar_test.go` with tests for download status checking

## Key Changes

- `internal/service/calendar/calendar.go`: Added downloadRepo, query real download status
- `internal/service/calendar/calendar_test.go`: New test file for calendar service
- `internal/api/handler/calendar.go`: Updated to accept download repository
- `internal/api/router/router.go`: Updated to pass downloadRepo to CalendarHandler

## Self-Check: PASSED

- All tasks completed
- Code committed
- Tests added and passing
