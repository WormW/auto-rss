# Roadmap: Auto-RSS 技术债务清理

**Project:** Auto-RSS 技术债务清理  
**Created:** 2025-04-05  
**Phases:** 3  
**Requirements:** 22

---

## Phase 1: Bug 修复与安全

**Goal:** 修复已知 Bug，消除安全隐患，确保系统稳定运行

**Requirements:** BUG-01 ~ BUG-05, SEC-01 ~ SEC-02 (7 requirements)

**Success Criteria:**
1. 下载重试功能正常工作（从 API 到实际添加任务）
2. 日历正确显示已下载/未下载状态
3. 磁盘空间不足时自动暂停新下载
4. Task Manager 无竞态条件（通过并发测试）
5. 文件移动和数据库状态保持一致（崩溃后无孤儿文件）
6. 路径输入无法跳出指定目录
7. 所有 SQL 查询通过注入审计

**Key Files:**
- `internal/api/handler/download.go`
- `internal/service/calendar/calendar.go`
- `internal/service/disk/monitor.go`
- `internal/service/task/manager.go`
- `internal/service/organizer/organizer.go`
- `internal/api/handler/subscription.go`
- `internal/repository/*.go`

---

## Phase 2: 代码重构

**Goal:** 拆分臃肿代码，提升可维护性和测试性

**Requirements:** REF-01 ~ REF-10 (10 requirements)

**Success Criteria:**
1. `subscription.go` 从 2345 行降至 < 800 行
2. `monitor.go` 从 959 行降至 < 500 行
3. `organizer.go` 从 683 行降至 < 400 行
4. 所有新提取的服务都有独立单元测试
5. 原有功能 100% 向后兼容（API 行为不变）

**New Structure:**
```
internal/
├── service/
│   ├── bangumi/
│   │   └── enrich.go           # REF-01
│   ├── renamer/
│   │   └── service.go          # REF-02
│   ├── subscription/
│   │   ├── batch.go            # REF-03
│   │   └── collection.go       # REF-04
│   ├── downloader/
│   │   ├── status_sync.go      # REF-05
│   │   ├── completion_handler.go # REF-06
│   │   └── retry_service.go    # REF-07
│   └── organizer/
│       ├── parser.go           # REF-08
│       ├── matcher.go          # REF-09
│       └── mover.go            # REF-10
```

---

## Phase 3: 性能与测试

**Goal:** 修复性能瓶颈，补充测试覆盖

**Requirements:** PERF-01 ~ PERF-03, TEST-01 ~ TEST-06 (5 requirements)

**Success Criteria:**
1. 订阅列表加载时间减少 50%（无 N+1 查询）
2. 所有 List 接口有分页上限保护
3. RSS 超时支持按源配置
4. Handler 层测试覆盖率达 60%+
5. Organizer 测试覆盖率达 70%+
6. Task Manager 通过并发安全测试

**Test Targets:**
- `internal/api/handler/download_test.go`
- `internal/api/handler/subscription_test.go`
- `internal/service/bangumi/bangumi_test.go`
- `internal/service/mikan/mikan_test.go`
- `internal/service/organizer/*_test.go`
- `internal/service/task/manager_test.go`

---

## Traceability

| Requirement | Phase | Plan File |
|-------------|-------|-----------|
| BUG-01 | Phase 1 | 01-01-fix-retry-logic.md |
| BUG-02 | Phase 1 | 01-02-fix-calendar-status.md |
| BUG-03 | Phase 1 | 01-03-implement-disk-pause.md |
| BUG-04 | Phase 1 | 01-04-fix-task-race-condition.md |
| BUG-05 | Phase 1 | 01-05-add-file-transaction.md |
| SEC-01 | Phase 1 | 01-06-prevent-path-traversal.md |
| SEC-02 | Phase 1 | 01-07-audit-sql-injection.md |
| REF-01~04 | Phase 2 | 02-01-refactor-subscription-handler.md |
| REF-05~07 | Phase 2 | 02-02-refactor-download-monitor.md |
| REF-08~10 | Phase 2 | 02-03-refactor-file-organizer.md |
| PERF-01~03 | Phase 3 | 03-01-fix-performance-issues.md |
| TEST-01~06 | Phase 3 | 03-02-add-test-coverage.md |

---

## Completion Criteria

- [ ] Phase 1: 0 个已知 Bug，无高危安全漏洞
- [ ] Phase 2: 所有目标文件行数达标，新服务有测试
- [ ] Phase 3: 性能指标达标，测试覆盖率达标

---

## Next Action

**Phase 1 Ready for Planning**

Run: `/gsd-plan-phase 1`

---
*Roadmap created: 2025-04-05*
*Last updated: 2025-04-05 after creation*
