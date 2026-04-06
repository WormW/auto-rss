# Roadmap: Auto-RSS

## Milestones

- **v1.0 技术债务清理** — Phases 1-3 (shipped 2026-04-05) — [详情](milestones/v1.0-ROADMAP.md)
- **v1.1 基础设施增强** — Phases 4-7 (in progress) — JWT认证、API限流、WebSocket重连、任务队列

---

## Phases

### v1.1 基础设施增强 (Phases 4-7)

- [ ] **Phase 4: JWT Authentication Foundation** — 实现JWT认证系统，保护API端点
- [ ] **Phase 5: API Rate Limiting** — 实现API限流保护，防止滥用
- [ ] **Phase 6: WebSocket Auto-Reconnection** — 实现WebSocket自动重连机制
- [ ] **Phase 7: Task Queue** — 实现任务队列支持多并发下载

---

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

## Phase Details

### Phase 4: JWT Authentication Foundation
**Goal**: 用户可以通过安全的JWT认证系统访问受保护的API端点
**Depends on**: Phase 3 (v1.0 completion)
**Requirements**: AUTH-01, AUTH-02, AUTH-03, AUTH-04
**Success Criteria** (what must be TRUE):
  1. 用户可以通过用户名/密码登录并获取access token和refresh token
  2. Access token有效期30分钟，refresh token有效期7天
  3. 使用refresh token可以获取新的token对，旧token被标记为已使用
  4. 并发刷新请求不会导致竞态条件或token重用问题
  5. 受保护的API端点会验证JWT token并拒绝无效请求
**Plans**: 3 plans

Plans:
- [ ] 04-01-PLAN.md — Core JWT Infrastructure (config, models, service)
- [ ] 04-02-PLAN.md — Auth Handlers (login, refresh endpoints)
- [ ] 04-03-PLAN.md — Auth Middleware (API protection)

### Phase 5: API Rate Limiting
**Goal**: API端点受到限流保护，防止滥用和DoS攻击
**Depends on**: Phase 4
**Requirements**: RATE-01, RATE-02, RATE-03, RATE-04
**Success Criteria** (what must be TRUE):
  1. 每个IP地址的请求被限制在10 req/s，burst 20
  2. 响应包含标准的X-RateLimit-*头信息（Limit, Remaining, Reset）
  3. 超限时返回429状态码和Retry-After头
  4. 不活跃客户端的限流数据1小时后自动清理
  5. 限流器内存使用有上限，不会无限增长
**Plans**: TBD

### Phase 6: WebSocket Auto-Reconnection
**Goal**: WebSocket连接断开后能够自动重连，保证实时通知的可靠性
**Depends on**: Phase 5
**Requirements**: WS-01, WS-02, WS-03, WS-04
**Success Criteria** (what must be TRUE):
  1. 网络异常断开后，客户端使用指数退避策略自动重连（1s -> 30s上限）
  2. 重连延迟包含随机抖动（±50%），防止惊群效应
  3. 断线期间的消息被缓冲（最多100条，TTL 5分钟），重连后按序发送
  4. 正常关闭（code 1000）和用户登出不触发重连
  5. 异常断开（网络错误）触发重连，最多重试10次
**Plans**: TBD
**UI hint**: yes

### Phase 7: Task Queue
**Goal**: 系统使用任务队列处理下载任务，支持多并发和背压保护
**Depends on**: Phase 6
**Requirements**: QUEUE-01, QUEUE-02, QUEUE-03, QUEUE-04
**Success Criteria** (what must be TRUE):
  1. Worker pool支持4个并发下载任务，可配置worker数量
  2. 任务队列有背压保护，队列满（100条）时拒绝新任务并返回错误
  3. SQLite使用WAL模式，单goroutine序列化写入避免冲突
  4. 任务状态持久化到数据库（pending, processing, completed, failed）
  5. 支持任务优先级（高/中/低），高优先级任务优先执行
**Plans**: TBD

---

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Bug 修复与安全 | v1.0 | 7/7 | Complete | 2025-04-05 |
| 2. 代码重构 | v1.0 | 5/5 | Complete | 2025-04-05 |
| 3. 性能与测试 | v1.0 | 5/5 | Complete | 2025-04-05 |
| 4. JWT Authentication Foundation | v1.1 | 0/3 | Not started | - |
| 5. API Rate Limiting | v1.1 | 0/TBD | Not started | - |
| 6. WebSocket Auto-Reconnection | v1.1 | 0/TBD | Not started | - |
| 7. Task Queue | v1.1 | 0/TBD | Not started | - |

---
*Last updated: 2026-04-06 after Phase 4 planning*
