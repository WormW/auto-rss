---
phase: 02
slug: refactor
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-05
---

# Phase 02 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (built-in) |
| **Config file** | none — uses go.mod |
| **Quick run command** | `go test ./internal/service/... -short -count=1` |
| **Full suite command** | `go test ./... -count=1` |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/service/{service}/... -count=1`
- **After every plan wave:** Run `go test ./internal/service/... -count=1`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 10 seconds per service test

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 02-01-01 | 01 | 1 | REF-01 | — | Bangumi enrichment works correctly | unit | `go test ./internal/service/bangumi/...` | ❌ W0 | ⬜ pending |
| 02-02-01 | 02 | 1 | REF-02 | — | File renaming works correctly | unit | `go test ./internal/service/renamer/...` | ❌ W0 | ⬜ pending |
| 02-03-01 | 03 | 1 | REF-03 | — | Batch import works correctly | unit | `go test ./internal/service/subscription/... -run Batch` | ❌ W0 | ⬜ pending |
| 02-04-01 | 04 | 1 | REF-04 | — | Collection download works correctly | unit | `go test ./internal/service/subscription/... -run Collection` | ❌ W0 | ⬜ pending |
| 02-05-01 | 05 | 2 | REF-05 | — | Status sync works correctly | unit | `go test ./internal/service/downloader/... -run Sync` | ❌ W0 | ⬜ pending |
| 02-06-01 | 06 | 2 | REF-06 | — | Completion handler works correctly | unit | `go test ./internal/service/downloader/... -run Complete` | ❌ W0 | ⬜ pending |
| 02-07-01 | 07 | 2 | REF-07 | — | Retry service works correctly | unit | `go test ./internal/service/downloader/... -run Retry` | ❌ W0 | ⬜ pending |
| 02-08-01 | 08 | 3 | REF-08 | — | Parser works correctly | unit | `go test ./internal/service/organizer/... -run Parser` | ❌ W0 | ⬜ pending |
| 02-09-01 | 09 | 3 | REF-09 | — | Matcher works correctly | unit | `go test ./internal/service/organizer/... -run Matcher` | ❌ W0 | ⬜ pending |
| 02-10-01 | 10 | 3 | REF-10 | — | Mover works correctly | unit | `go test ./internal/service/organizer/... -run Mover` | ❌ W0 | ⬜ pending |
| 02-11-01 | 11 | 4 | REF-all | — | Handler line counts under limit | verify | `wc -l internal/api/handler/subscription.go` | ✅ | ⬜ pending |
| 02-11-02 | 11 | 4 | REF-all | — | Monitor line counts under limit | verify | `wc -l internal/service/downloader/monitor.go` | ✅ | ⬜ pending |
| 02-11-03 | 11 | 4 | REF-all | — | Organizer line counts under limit | verify | `wc -l internal/service/organizer/organizer.go` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/service/bangumi/enrich_test.go` — test stubs for REF-01
- [ ] `internal/service/renamer/service_test.go` — test stubs for REF-02
- [ ] `internal/service/subscription/batch_test.go` — test stubs for REF-03
- [ ] `internal/service/subscription/collection_test.go` — test stubs for REF-04
- [ ] `internal/service/downloader/status_sync_test.go` — test stubs for REF-05
- [ ] `internal/service/downloader/completion_handler_test.go` — test stubs for REF-06
- [ ] `internal/service/downloader/retry_service_test.go` — test stubs for REF-07
- [ ] `internal/service/organizer/parser_test.go` — test stubs for REF-08
- [ ] `internal/service/organizer/matcher_test.go` — test stubs for REF-09
- [ ] `internal/service/organizer/mover_test.go` — test stubs for REF-10

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Backward compatibility | REF-all | API behavior must be manually verified | Run integration tests and verify all endpoints return same responses |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
