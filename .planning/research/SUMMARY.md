# Research Summary: Auto-RSS v1.1 Infrastructure Features

**Researched:** 2026-04-06
**Features:** JWT Authentication, API Rate Limiting, WebSocket Reconnection, Task Queue

---

## Key Findings

### Stack Additions

| Feature | Library | Version | Notes |
|---------|---------|---------|-------|
| JWT Auth | `github.com/golang-jwt/jwt/v5` | v5.2.2 | Security fix CVE-2025-30204 |
| Rate Limiting | `github.com/ulule/limiter/v3` | v3.11.2 | Native Gin middleware |
| WebSocket | `github.com/gorilla/websocket` | v1.5.3 (existing) | Add reconnection wrapper |
| Task Queue | `github.com/hibiken/asynq` | v0.25.1* | Go 1.23 compatible |

*Note: v0.26.0 requires Go 1.24, use v0.25.1 for Go 1.23

### Critical Pitfalls to Avoid

1. **JWT Algorithm Confusion** — Always whitelist `HS256` only, reject `none` algorithm
2. **SQLite Write Contention** — Enable WAL mode, serialize writes through single goroutine
3. **WebSocket Thundering Herd** — Exponential backoff with jitter (not fixed intervals)
4. **Rate Limiter Memory Exhaustion** — Implement TTL cleanup for inactive clients

### Recommended Build Order

1. **SEC-03: JWT Authentication** — Foundation, no dependencies
2. **SEC-04: API Rate Limiting** — Protects auth endpoints, simple addition
3. **INF-02: WebSocket Reconnection** — Client-side only, independent
4. **INF-03: Task Queue** — Most complex, requires careful SQLite handling

### Architecture Changes

- **New Middleware:** auth, rate limiting
- **New Services:** auth service, queue service
- **Schema Additions:** users, refresh_tokens, rate_limit_counters, job_queue
- **Integration:** Apply middleware to router, replace task.Manager with queue

---

*See full research in STACK.md, FEATURES.md, ARCHITECTURE.md, PITFALLS.md*
