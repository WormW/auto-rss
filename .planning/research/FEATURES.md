# Feature Landscape: v1.1 Infrastructure Features

**Domain:** Auto-RSS Go/Gin Application Infrastructure  
**Researched:** 2026-04-05  
**Overall confidence:** HIGH

## Overview

This document analyzes four infrastructure features for Auto-RSS v1.1: JWT authentication, API rate limiting, WebSocket auto-reconnection, and task queue support. Each feature is categorized as table stakes (must-have), differentiator (nice-to-have), or anti-feature (what to avoid), with complexity assessments and dependencies on the existing system.

---

## 1. JWT Authentication (SEC-03)

### Table Stakes

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Token-based authentication | Standard for modern APIs | Medium | Replace current no-auth state |
| Access/Refresh token pair | Security best practice | Medium | 15-30 min access, 7-30 days refresh |
| Bearer token extraction | HTTP standard | Low | From Authorization header |
| Token validation middleware | Required for protected routes | Medium | Integrate with Gin middleware chain |
| Secure secret management | Production requirement | Low | Environment variable based |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Refresh token rotation | Enhanced security | Medium | Issue new refresh token on each use |
| Token blacklist (jti) | Immediate logout capability | Medium | Redis-backed for revocation |
| Multi-algorithm support | Future-proofing | Low | HS256 now, EdDSA ready |
| Token binding to IP | Extra security layer | Low | Optional, prevents token theft use |
| Audit logging | Security monitoring | Low | Log token issuance/validation events |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Long-lived access tokens (>1 hour) | Security risk | 15-30 minutes max, use refresh tokens |
| Storing tokens in localStorage | XSS vulnerability | HttpOnly cookies or secure memory |
| `none` algorithm support | Critical vulnerability | Explicit algorithm verification only |
| Hardcoded secrets | Security breach risk | Environment variables, secret managers |
| Stateless refresh tokens | Cannot revoke on breach | Server-side refresh token storage |

### Complexity Assessment

| Aspect | Level | Rationale |
|--------|-------|-----------|
| Implementation | Medium | Well-established patterns, libraries available |
| Testing | Medium | Token lifecycle, edge cases, expiration |
| Migration | Low | Currently no auth, additive feature |
| Maintenance | Low | Standard JWT, no custom crypto |

### Dependencies on Existing System

- **Router**: Add auth middleware to `/api/v1/*` routes
- **Middleware chain**: Insert after CORS, before handlers
- **Config system**: Store JWT secret, token lifetimes
- **Database**: Add user table (or use single admin user for v1.1)
- **WebSocket**: May need token validation on upgrade

### Recommended Implementation

```go
// Use appleboy/gin-jwt for comprehensive features
import jwt "github.com/appleboy/gin-jwt/v2"

// Key settings:
// - Timeout: 30 minutes (access token)
// - MaxRefresh: 7 days (refresh token)
// - SecureCookie: true (HTTPS only)
// - CookieHTTPOnly: true
// - TokenLookup: "header:Authorization, cookie:jwt"
```

---

## 2. API Rate Limiting (SEC-04)

### Table Stakes

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Per-IP rate limiting | Basic DDoS protection | Low | Standard for any public API |
| Rate limit headers | Client expectation | Low | X-RateLimit-Limit, Remaining, Reset |
| 429 Too Many Requests | HTTP standard | Low | Proper status code return |
| Configurable limits | Deployment flexibility | Low | Via config system |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Per-endpoint limits | Granular control | Medium | Heavy endpoints (RSS refresh) stricter |
| Authenticated user limits | Higher limits for logged-in users | Medium | Different tiers |
| Sliding window algorithm | Fairer than fixed window | Medium | Prevents edge-of-window bursts |
| Burst allowance | Better UX for legitimate users | Low | Token bucket capacity > 1 |
| Rate limit events | Monitoring/alerting | Low | Log when limits hit |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Global rate limit only | Affects all users equally | Per-IP + per-user differentiation |
| No headers | Poor client experience | Always return rate limit headers |
| Fixed window only | Allows 2x burst at edges | Use sliding window or token bucket |
| Memory-only storage | Lost on restart | Persistent storage (Redis or in-memory with grace) |
| Blocking rate limiter | Degrades under load | Non-blocking with channel buffer |

### Complexity Assessment

| Aspect | Level | Rationale |
|--------|-------|-----------|
| Implementation | Low-Medium | `golang.org/x/time/rate` provides token bucket |
| Testing | Low | Deterministic with controlled time |
| Migration | Low | Additive middleware |
| Maintenance | Low | Standard library, well-tested |

### Dependencies on Existing System

- **Middleware chain**: Insert early, before auth to protect login endpoint
- **Config system**: Store rate limits per endpoint/category
- **Redis** (optional): For distributed rate limiting (future)
- **Logger**: Rate limit hit events

### Recommended Implementation

```go
// Use golang.org/x/time/rate for token bucket
import "golang.org/x/time/rate"

// Per-client limiter map with cleanup
// - Refill rate: 10 requests/second
// - Burst: 20 requests
// - Cleanup idle limiters every 10 minutes
```

---

## 3. WebSocket Auto-Reconnection (INF-02)

### Table Stakes

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Exponential backoff | Standard reconnection pattern | Low | Prevents thundering herd |
| Max retry attempts | Prevent infinite loops | Low | Configurable, e.g., 10 attempts |
| Manual close detection | Don't reconnect on intentional close | Low | Flag to distinguish from error |
| Connection state tracking | UI feedback | Low | connected/disconnecting/reconnecting |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Jitter in backoff | Better distribution | Low | Random component to delay |
| Message queue during disconnect | No lost notifications | Medium | Buffer messages, flush on reconnect |
| Heartbeat/ping-pong | Detect dead connections | Low | 30s interval, 10s timeout |
| Session resumption | Replay missed events | Medium | Server-side event buffer |
| Reconnection state restore | Re-subscribe automatically | Low | Restore subscriptions on reconnect |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Immediate reconnection | Server overload | Exponential backoff minimum 1s |
| Unlimited reconnection | Resource exhaustion | Max attempts or max total time |
| Reconnect on 1000 (normal close) | Wastes resources | Check close code, only reconnect on abnormal |
| No user feedback | Poor UX | Visual indicator of connection state |
| Blocking reconnection | UI freeze | Async reconnection with state updates |

### Complexity Assessment

| Aspect | Level | Rationale |
|--------|-------|-----------|
| Client-side | Medium | Vue.js reconnection logic, state management |
| Server-side | Low | Current WebSocket hub supports reconnections |
| Testing | Medium | Network failure simulation |
| Migration | Low | Enhancement to existing WebSocket |
| Maintenance | Low | Standard patterns |

### Dependencies on Existing System

- **Existing WebSocket hub** (`internal/service/notification/websocket.go`): Already supports multiple clients
- **Vue.js frontend**: Reconnection logic in notification service
- **Current ping/pong**: Already implemented in `readPump()`/`writePump()`

### Current State Analysis

The existing WebSocket implementation already has:
- Ping/pong heartbeat (60s pong wait, 54s ping period)
- Proper close handling
- Client registration/unregistration

**What's missing:**
- Client-side reconnection logic
- Message buffering during disconnect
- Visual connection state indicator

### Recommended Implementation

```javascript
// Frontend (Vue.js) - Exponential backoff with jitter
const backoff = {
  baseDelay: 1000,
  maxDelay: 30000,
  attempt: 0,
  nextDelay() {
    const exp = Math.min(this.baseDelay * Math.pow(2, this.attempt), this.maxDelay);
    this.attempt++;
    return exp * (0.5 + Math.random() * 0.5); // Jitter
  },
  reset() { this.attempt = 0; }
};
```

---

## 4. Task Queue for Multi-Concurrent Downloads (INF-03)

### Table Stakes

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Concurrent download processing | Better throughput | Medium | Current sequential processing |
| Worker pool pattern | Standard concurrency control | Medium | Fixed number of workers |
| Queue with backpressure | Prevent memory exhaustion | Medium | Bounded queue, reject when full |
| Task status tracking | User visibility | Low | Pending/Running/Completed/Failed |

### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Priority queue | Important downloads first | Medium | High priority for manual triggers |
| Dynamic worker scaling | Adapt to load | Medium | Scale up/down based on queue depth |
| Retry with exponential backoff | Resilience | Low | Already have retry service |
| Progress streaming | Real-time updates | Medium | WebSocket integration |
| Task cancellation | User control | Medium | Context cancellation |
| Batch operations | Efficiency | Medium | Batch import from RSS |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Unlimited goroutines | Resource exhaustion | Worker pool with fixed size |
| Unbounded queue | Memory exhaustion | Bounded channel with backpressure |
| No visibility | Poor UX | Task status API, WebSocket updates |
| Fire-and-forget | Lost tasks | Persistent queue (DB-backed) |
| Blocking task submission | API timeouts | Non-blocking with immediate response |

### Complexity Assessment

| Aspect | Level | Rationale |
|--------|-------|-----------|
| Implementation | Medium | Worker pool + queue integration |
| Testing | Medium | Concurrency, race conditions |
| Migration | Medium | Refactor existing task manager |
| Maintenance | Low | Standard Go concurrency patterns |

### Dependencies on Existing System

- **Current Task Manager** (`internal/service/task/manager.go`): Single-task only, needs extension
- **Download Monitor** (`internal/service/downloader/monitor.go`): Processes pending downloads
- **Scheduler** (`internal/service/scheduler/scheduler.go`): Creates download tasks
- **WebSocket hub**: For progress notifications
- **Database**: Download table stores task state

### Current State Analysis

The existing task system:
- **Task Manager**: Single concurrent task, supports cancellation
- **Download Monitor**: Processes pending downloads sequentially (10 at a time in `processPendingDownloads`)
- **Scheduler**: Creates download records, adds to qBittorrent

**What's missing:**
- True concurrent download processing (qBittorrent handles actual downloads)
- Queue for download creation tasks
- Worker pool for RSS parsing (CPU-bound)

### Recommended Architecture

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│   Scheduler     │────▶│  Task Queue  │────▶│  Worker Pool    │
│ (RSS Check)     │     │  (Buffered)  │     │  (4 workers)    │
└─────────────────┘     └──────────────┘     └────────┬────────┘
                                                      │
                           ┌──────────────────────────┘
                           ▼
                    ┌──────────────┐
                    │  qBittorrent │
                    │   (Actual    │
                    │  downloads)  │
                    └──────────────┘
```

Worker sizing:
- **RSS parsing workers**: `runtime.NumCPU()` (CPU-bound)
- **Download creation workers**: `runtime.NumCPU() * 2` (I/O-bound, API calls)

---

## Feature Dependencies

```
JWT Auth
  └── Rate Limiting (protect login endpoint)

Task Queue
  ├── WebSocket (progress updates)
  └── Existing Task Manager (refactor/extend)

WebSocket Reconnection
  └── Existing WebSocket Hub (no server changes needed)

Rate Limiting
  └── JWT Auth (different limits for auth vs unauth)
```

---

## MVP Recommendation for v1.1

### Phase 1: JWT Authentication (SEC-03)
**Priority:** HIGH
- Single admin user (no multi-user yet)
- Access token: 30 minutes
- Refresh token: 7 days with rotation
- Protect all `/api/v1/*` routes

### Phase 2: API Rate Limiting (SEC-04)
**Priority:** HIGH
- Per-IP limiting using token bucket
- 10 req/s with burst of 20
- Apply before auth to protect login

### Phase 3: WebSocket Auto-Reconnection (INF-02)
**Priority:** MEDIUM
- Client-side only (Vue.js)
- Exponential backoff with jitter
- Max 10 reconnection attempts
- Visual connection indicator

### Phase 4: Task Queue (INF-03)
**Priority:** MEDIUM
- Worker pool for RSS parsing
- 4 workers for download creation
- Queue depth: 100 tasks
- Backpressure: reject when full

### Defer to v1.2+

| Feature | Reason |
|---------|--------|
| Multi-user support | Requires database schema changes, out of scope per PROJECT.md |
| Distributed rate limiting | Single instance deployment currently |
| Session resumption | Complex server-side state, limited value for notifications |
| Dynamic worker scaling | Fixed workers sufficient for current load |
| Priority queue | FIFO sufficient for current needs |

---

## Complexity Summary

| Feature | Implementation | Testing | Integration | Total |
|---------|---------------|---------|-------------|-------|
| JWT Auth | Medium | Medium | Low | **Medium** |
| Rate Limiting | Low | Low | Low | **Low** |
| WebSocket Reconnect | Low | Medium | Low | **Low-Medium** |
| Task Queue | Medium | Medium | Medium | **Medium** |

---

## Sources

- [JWT Security Best Practices 2025](https://jwtsecrets.com/blog/jwt-security-best-practices-2025)
- [JWT Security Best Practices for 2025 - JWT.app](https://jwt.app/blog/jwt-best-practices)
- [How to Handle JWT Authentication Securely in Go](https://oneuptime.com/blog/post/2026-01-07-go-jwt-authentication/view)
- [GitHub - appleboy/gin-jwt](https://github.com/appleboy/gin-jwt)
- [10 Best Practices for API Rate Limiting in 2025](https://zuplo.com/learning-center/10-best-practices-for-api-rate-limiting-in-2025/)
- [How to Implement Rate Limiting in Go Without External Services](https://oneuptime.com/blog/post/2026-01-07-go-rate-limiting/view)
- [Building Rate Limiter using the Token Bucket Algorithm in Go](https://blog.blockmagnates.com/building-rate-limiter-using-the-token-bucket-algorithm-in-go-3ce46b4541bf)
- [How to Implement Reconnection Logic for WebSockets](https://oneuptime.com/blog/post/2026-01-27-websocket-reconnection/view)
- [WebSocket Reliability Patterns for Multi-Agent Systems](https://zylos.ai/research/2026-02-23-websocket-reliability-multi-agent-systems)
- [Go Worker Pool Pattern: Production-Ready Concurrency Control](https://www.backendbytes.com/go/go-worker-pool-pattern-concurrency-control/)
- [7 Powerful Golang Concurrency Patterns That Will Transform Your Code in 2025](https://cristiancurteanu.com/7-powerful-golang-concurrency-patterns-that-will-transform-your-code-in-2025/)
- [Mastering the Worker Pool Pattern in Go](https://corentings.dev/blog/go-pattern-worker/)
