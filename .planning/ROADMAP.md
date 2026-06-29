# Roadmap: Auto-RSS

## Milestones

- **v1.0 技术债务清理** — Phases 1-3 (shipped 2026-04-05) — [详情](milestones/v1.0-ROADMAP.md)
- **v1.1 基础设施增强** — Phases 5-6 (shipped 2026-04-06) — [详情](milestones/v1.1-ROADMAP.md)
- **v1.2 API层功能增强** — Phase 7 API, handler tests, and router-level validation published — PR #38/#39/#40/#43/#44/#45/#46

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

<details>
<summary>✅ v1.1 基础设施增强 (Phases 5-6) — SHIPPED 2026-04-06</summary>

### Phase 5: API 限流 (3 plans)
- [x] 05-01: Token bucket rate limiter — completed 2026-04-06
- [x] 05-02: Middleware integration — completed 2026-04-06
- [x] 05-03: Configuration support — completed 2026-04-06

### Phase 6: WebSocket 自动重连 (3 plans)
- [x] 06-01: JWT authentication for WebSocket — completed 2026-04-06
- [x] 06-02: WebSocket service with reconnection — completed 2026-04-06
- [x] 06-03: Vue.js integration — completed 2026-04-06

</details>

---

## Current Phase

### Phase 7: API层功能增强 (v1.2) — API, HANDLER TESTS, AND ROUTER VALIDATION PUBLISHED

实现标签系统、下载历史/统计、RSS健康检查的完整API层。

- [x] 07-01: 标签系统 Handler 和路由 — completed 2026-04-12
- [x] 07-02: 下载历史和统计 Handler 和路由 — completed 2026-04-12
- [x] 07-03: RSS 健康检查 Handler 和路由 — completed 2026-04-12
- [x] 07-04: 标签系统 Handler 测试 — published 2026-06-22 via WOR-139 / PR #38
- [x] 07-05: 下载历史和统计 Handler 测试 — published 2026-06-22 via WOR-140 / PR #39
- [x] 07-06: RSS 健康检查 Handler 测试 — published 2026-06-22 via WOR-141 / PR #40
- [x] 07-07: 路由级集成测试和 API 验证 — published through WOR-159/WOR-164/WOR-169/WOR-174 and PR #43/#44/#45/#46

---

## Progress

| Phase             | Milestone | Plans Complete | Status      | Completed  |
|-------------------|-----------|----------------|-------------|------------|
| 1. Bug 修复与安全 | v1.0      | 7/7            | Complete    | 2025-04-05 |
| 2. 代码重构       | v1.0      | 5/5            | Complete    | 2025-04-05 |
| 3. 性能与测试     | v1.0      | 5/5            | Complete    | 2025-04-05 |
| 5. API 限流       | v1.1      | 3/3            | Complete    | 2026-04-06 |
| 6. WebSocket 自动重连 | v1.1  | 3/3            | Complete    | 2026-04-06 |
| 7. API层功能增强  | v1.2      | 7/7            | Complete    | 2026-06-28 |

---
*Last updated: 2026-06-29 after PR #46 completed the final Phase 7 router-level API validation slice. Recovery scan work remains dry-run/spec/test-only unless a future human gate approves mutation-oriented apply work.*
