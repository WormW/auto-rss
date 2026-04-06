# Roadmap: Auto-RSS

## Milestones

- **v1.0 技术债务清理** — Phases 1-3 (shipped 2026-04-05) — [详情](milestones/v1.0-ROADMAP.md)

---

## Phases

<details>
<summary>✅ v1.0 技术债务清理 (Phases 1-3) — SHIPPED 2026-04-05</summary>

### Phase 1: Bug 修复与安全 (7 plans)
- [x] 01-01: 修复下载重试逻辑 — completed 2025-04-05
- [x] 01-02: 修复日历下载状态 — completed 2025-04-05
- [x] 01-03: 实现磁盘监控暂停功能 — completed 2025-04-05
- [x] 01-04: 修复 Task Manager 竞态条件 — completed 2025-04-05
- [x] 01-05: 添加文件事务保护 — completed 2025-04-05
- [x] 01-06: 防止路径遍历攻击 — completed 2025-04-05
- [x] 01-07: SQL 注入审计 — completed 2025-04-05

### Phase 2: 代码重构 (5 plans)
- [x] 02-01: Bangumi Enricher 和 Renamer 服务 — completed 2025-04-05
- [x] 02-02: 批量导入和集合下载服务 — completed 2025-04-05
- [x] 02-03: 状态同步、完成处理器、重试服务 — completed 2025-04-05
- [x] 02-04: Matcher 和 Mover 服务 — completed 2025-04-05
- [x] 02-05: 主文件重构使用新服务 — completed 2025-04-05

### Phase 3: 性能与测试 (5 plans)
- [x] 03-01: N+1 查询修复和分页限制 — completed 2025-04-05
- [x] 03-02: 可配置 RSS 超时 — completed 2025-04-05
- [x] 03-03: Handler 层测试 — completed 2025-04-05
- [x] 03-04: Bangumi 和 Mikan 服务测试 — completed 2025-04-05
- [x] 03-05: Organizer 和 Task Manager 测试 — completed 2025-04-05

</details>

---

## Progress

| Phase             | Milestone | Plans Complete | Status      | Completed  |
|-------------------|-----------|----------------|-------------|------------|
| 1. Bug 修复与安全 | v1.0      | 7/7            | Complete    | 2025-04-05 |
| 2. 代码重构       | v1.0      | 5/5            | Complete    | 2025-04-05 |
| 3. 性能与测试     | v1.0      | 5/5            | Complete    | 2025-04-05 |
| 5. API 限流       | v1.1      | 3/3            | Complete    | 2026-04-06 |
| 6. WebSocket 自动重连 | v1.1  | 3/3            | Complete    | 2026-04-06 |

---
*Last updated: 2026-04-06 after Phase 5 completion*
