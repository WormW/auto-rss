# Technology Stack Research: v1.1 Infrastructure Features

**Project:** Auto-RSS v1.1 Infrastructure Enhancement
**Domain:** Go/Gin backend with JWT auth, rate limiting, WebSocket, task queue
**Researched:** 2026-04-05
**Confidence:** HIGH

## Executive Summary

For Auto-RSS v1.1 infrastructure features, the recommended stack additions are:

1. **JWT Authentication**: `github.com/golang-jwt/jwt/v5` (v5.2.2) — Community-maintained successor to deprecated dgrijalva/jwt-go
2. **API Rate Limiting**: `github.com/ulule/limiter/v3` (v3.11.2) — Native Gin middleware with Redis support
3. **WebSocket**: Keep existing `github.com/gorilla/websocket` (v1.5.3) — Add custom reconnection wrapper
4. **Task Queue**: `github.com/hibiken/asynq` (v0.26.0) — Redis-based with excellent observability

All libraries are actively maintained, production-proven, and integrate seamlessly with the existing Go 1.23 + Gin + SQLite stack.

---

## Recommended Stack Additions

### Core Libraries for v1.1 Features

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| golang-jwt/jwt | v5.2.2 | JWT authentication | Community-maintained successor to vulnerable dgrijalva/jwt-go; v5 has improved validation API; security patch (CVE-2025-30204) fixed in v5.2.2 |
| ulule/limiter | v3.11.2 | API rate limiting | Native Gin middleware; supports memory and Redis stores; per-route and per-key limiting; production-proven |
| gorilla/websocket | v1.5.3 (existing) | WebSocket connections | Already in use; stable and widely adopted; no need to change |
| hibiken/asynq | v0.26.0 | Task queue for downloads | Redis-based; excellent Web UI (asynqmon); retries, priorities, scheduling; most popular Go task queue (~13K stars) |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| golang.org/x/time/rate | latest | Alternative rate limiter | When you need minimal dependencies and custom implementation |
| github.com/redis/go-redis/v9 | v9.5.0 | Redis client | Required by asynq; use for rate limiting Redis store if needed |
| github.com/hibiken/asynqmon | latest | Asynq Web UI | For monitoring task queues in production |

---

## Detailed Library Analysis

### 1. JWT Authentication: golang-jwt/jwt/v5

**Current Version:** v5.2.2 (March 2025) — **Security patch release**

**Why This Choice:**
- **Security**: Original `dgrijalva/jwt-go` has CVE-2020-26160 (authorization bypass) and is unmaintained
- **Community**: Official community-maintained fork with active development
- **API**: v5 introduces cleaner validation API with `RegisteredClaims` replacing deprecated `StandardClaims`
- **Compatibility**: Maintains backward compatibility with v3.x tags for easier migration

**Critical Security Note:**
- **CVE-2025-30204** (CVSS 7.5/10) fixed in v5.2.2 — DoS via excessive memory allocation when parsing malicious JWT tokens
- **Action**: Must use v5.2.2 or later

**Integration with Gin:**
```go
import "github.com/golang-jwt/jwt/v5"

// Middleware pattern
func JWTMiddleware(secret string) gin.HandlerFunc {
    return func(c *gin.Context) {
        tokenString := extractBearerToken(c)
        token, err := jwt.Parse(tokenString, func(token *jwt.Token) {
            return []byte(secret), nil
        }, jwt.WithValidMethods([]string{"HS256"}))
        // ... validation logic
    }
}
```

**Alternatives Considered:**
| Library | Why Not |
|---------|---------|
| dgrijalva/jwt-go | Deprecated, has CVE-2020-26160, unmaintained |
| lestrrat-go/jwx | More complex, overkill for simple JWT needs |
| auth0/go-jwt-middleware | Tied to Auth0 ecosystem |

---

### 2. API Rate Limiting: ulule/limiter/v3

**Current Version:** v3.11.2 (May 2023) — Stable, maintained

**Why This Choice:**
- **Gin Native**: Built-in middleware specifically for Gin (`drivers/middleware/gin`)
- **Flexible Storage**: In-memory (single instance) or Redis (distributed)
- **Rich Features**: Per-route limiting, custom key extractors (IP, user ID, API key), configurable headers
- **Production Ready**: Used in production APIs, Debian/Ubuntu packaged

**Key Features:**
- Token bucket algorithm
- Custom `KeyGetter` for complex scenarios (e.g., authenticated user ID instead of IP)
- Automatic `X-RateLimit-*` headers
- 429 status code with customizable response

**Integration with Gin:**
```go
import (
    "github.com/ulule/limiter/v3"
    mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
    "github.com/ulule/limiter/v3/drivers/store/memory"
)

// Rate: 100 requests per minute
rate := limiter.Rate{
    Period: 1 * time.Minute,
    Limit:  100,
}
store := memory.NewStore()
instance := limiter.New(store, rate)
router.Use(mgin.NewMiddleware(instance))
```

**Redis Store for Distributed Rate Limiting:**
```go
import "github.com/ulule/limiter/v3/drivers/store/redis"

store, err := redis.NewStore(redisClient)
```

**Alternatives Considered:**
| Library | Why Not |
|---------|---------|
| golang.org/x/time/rate | No HTTP/Gin integration; requires custom middleware |
| go-chi/httprate | Chi-specific, not Gin |
| sethvargo/go-limiter | Less Gin-specific support |

---

### 3. WebSocket: gorilla/websocket (Existing)

**Current Version:** v1.5.3 (already in project)

**Decision:** Keep existing library. No change needed.

**Why This Choice:**
- **Already Integrated**: Project already uses gorilla/websocket v1.5.3
- **Standard**: Most widely used Go WebSocket library
- **Stable**: Mature, well-documented, battle-tested

**For Auto-Reconnection (INF-02):**
Gorilla/websocket does not provide built-in reconnection. Implement a wrapper with:

**Recommended Pattern:**
```go
type ReconnectingClient struct {
    URL              string
    Conn             *websocket.Conn
    ReconnectInterval time.Duration
    MaxReconnects    int
    OnMessage        func(messageType int, data []byte)
    OnConnect        func()
    OnDisconnect     func()
    
    mu         sync.RWMutex
    connected  bool
    reconnects int
    stopCh     chan struct{}
}

func (c *ReconnectingClient) Connect() error {
    // Initial connection with exponential backoff
}

func (c *ReconnectingClient) readLoop() {
    for {
        messageType, data, err := c.Conn.ReadMessage()
        if err != nil {
            // Connection lost, trigger reconnect
            c.handleReconnect()
            return
        }
        c.OnMessage(messageType, data)
    }
}
```

**Key Implementation Details:**
- Exponential backoff with jitter (prevent thundering herd)
- Configurable max reconnection attempts (0 = unlimited)
- Ping/pong heartbeat for connection health detection
- Thread-safe connection state management
- Message buffering during disconnection

**Alternatives Considered:**
| Library | Why Not |
|---------|---------|
| nhooyr/websocket | Less mature, smaller community |
| coder/websocket | Good but no compelling reason to migrate from gorilla |

---

### 4. Task Queue: hibiken/asynq

**Current Version:** v0.26.0 (December 2024)

**Why This Choice:**
- **Observability**: Built-in Web UI (asynqmon), CLI tools, Prometheus metrics
- **Features**: Scheduling, retries, priority queues, task aggregation, dead letter queues
- **Performance**: Redis-backed, low latency for task enqueue
- **Ecosystem**: Most popular Go task queue (~13K stars), active community

**Key Features for Auto-RSS:**
- **Multi-concurrent downloads**: Configure worker concurrency per queue
- **Retry with backoff**: Failed downloads automatically retried
- **Priority queues**: High priority for user-initiated, low for background RSS
- **Task timeout/deadline**: Prevent stuck downloads
- **Queue pause/resume**: Useful for disk space management

**Integration Pattern:**
```go
import "github.com/hibiken/asynq"

// Task definition
type DownloadTask struct {
    URL      string
    Priority int
    RSSID    uint
}

// Client (enqueue)
client := asynq.NewClient(asynq.RedisClientOpt{Addr: "localhost:6379"})
task := asynq.NewTask("download", payload)
info, err := client.Enqueue(task, asynq.Queue("downloads"), asynq.MaxRetry(3))

// Server (worker)
srv := asynq.NewServer(
    asynq.RedisClientOpt{Addr: "localhost:6379"},
    asynq.Config{
        Concurrency: 5,  // 5 concurrent downloads
        Queues: map[string]int{
            "downloads": 10,
            "metadata":  5,
        },
    },
)
```

**Version Notes:**
- v0.26.0 requires **Go 1.24.0** minimum (project uses Go 1.23 — need to verify compatibility)
- v0.25.1 supports Go 1.22+ if upgrade needed
- Headers support added in v0.26.0
- TLS and Redis ACL auth improvements

**Alternatives Considered:**
| Library | Why Not |
|---------|---------|
| gocraft/work | Less feature-rich, no built-in UI |
| machinery | Slower development, resource intensive at scale |
| River | PostgreSQL-based (good for transactions, but adds complexity) |
| Custom implementation | Reinventing the wheel; asynq provides proven patterns |

---

## Installation Commands

```bash
# JWT Authentication
go get github.com/golang-jwt/jwt/v5@v5.2.2

# Rate Limiting
go get github.com/ulule/limiter/v3@v3.11.2

# Task Queue (asynq)
go get github.com/hibiken/asynq@v0.26.0

# Redis client (if not already present)
go get github.com/redis/go-redis/v9@v9.5.0

# Asynq Web UI (optional, for monitoring)
go install github.com/hibiken/asynqmon@latest
```

---

## Version Compatibility Matrix

| Package | Go Version | Compatible With | Notes |
|---------|------------|-----------------|-------|
| golang-jwt/jwt v5.2.2 | Go 1.18+ | Gin v1.10.0 | No known issues |
| ulule/limiter v3.11.2 | Go 1.17+ | Gin v1.10.0 | Uses Gin v1.8.2 internally |
| gorilla/websocket v1.5.3 | Go 1.12+ | Existing codebase | Already integrated |
| hibiken/asynq v0.26.0 | Go 1.24.0+ | Redis 6.0+ | May need v0.25.1 for Go 1.23 |

**Go Version Compatibility Check:**
- Project uses Go 1.23.0
- asynq v0.26.0 requires Go 1.24.0
- **Action**: Use asynq v0.25.1 if not upgrading Go, or upgrade Go to 1.24

---

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| dgrijalva/jwt-go | CVE-2020-26160 (auth bypass), unmaintained | golang-jwt/jwt/v5 |
| golang.org/x/time/rate alone | No Gin integration, requires boilerplate | ulule/limiter for quick setup |
| Machinery (task queue) | Slower development, resource issues | asynq |
| Custom JWT implementation | Security risks, maintenance burden | golang-jwt/jwt |
| In-memory task queue | Lost on restart, no persistence | asynq with Redis |

---

## Integration Architecture

### JWT + Rate Limiting Middleware Stack

```
Request → Rate Limit (IP-based) → JWT Auth → Handler
              ↓                       ↓
         429 if exceeded       401 if invalid
```

Recommended order:
1. Rate limiting first (prevents auth flooding)
2. JWT validation second
3. Handler last

### Task Queue Architecture

```
RSS Fetcher ──┐
              ├──► [asynq:download queue] ──► Workers ──► qBittorrent
User Action ──┘         (concurrent=5)
                              │
                         [asynq:metadata queue]
                              │
                         Metadata Workers
```

### WebSocket Auto-Reconnection Flow

```
Client ──► Connect ──► Read Loop
              │              │
              │         Error/Disconnect
              │              │
              └────◄── Exponential Backoff Retry
```

---

## Phase Implementation Notes

| Feature | Library | Complexity | Notes |
|---------|---------|------------|-------|
| SEC-03: JWT Auth | golang-jwt/jwt | Medium | Use RegisteredClaims, set reasonable exp |
| SEC-04: Rate Limiting | ulule/limiter | Low | Start with memory store, Redis for scale |
| INF-02: WebSocket Reconnect | gorilla + custom | Medium | Implement reconnection wrapper |
| INF-03: Task Queue | asynq | Medium-High | Requires Redis, plan infrastructure |

---

## Sources

- [golang-jwt/jwt GitHub](https://github.com/golang-jwt/jwt) — v5.2.2 release notes, CVE-2025-30204 security fix
- [ulule/limiter GitHub](https://github.com/ulule/limiter) — v3.11.2 release, Gin middleware docs
- [hibiken/asynq GitHub](https://github.com/hibiken/asynq) — v0.26.0 release, feature documentation
- [gorilla/websocket GitHub](https://github.com/gorilla/websocket) — v1.5.3 documentation
- [WebSocket Reconnection Best Practices](https://oneuptime.com/blog/post/2026-01-27-websocket-reconnection/view) — 2026 reconnection patterns
- [Task Queues in Go: Asynq vs Machinery](https://www.linkedin.com/pulse/task-queues-go-asynq-vs-machinery-vs-work-powering-jobs-systems-flores-dgx4f) — Comparison analysis

---

*Stack research for: Auto-RSS v1.1 Infrastructure Features*
*Researched: 2026-04-05*
