# Architecture Research: v1.1 Infrastructure Features

**Domain:** Go/Gin Service-Oriented Architecture with Infrastructure Enhancements
**Researched:** 2026-04-05
**Confidence:** HIGH

## Executive Summary

This research analyzes how four new infrastructure features (JWT authentication, API rate limiting, WebSocket auto-reconnection, and task queue) integrate with Auto-RSS's existing service-oriented architecture. The existing system uses a Handler-Service-Repository pattern with 10 independent services, SQLite database, and Gin framework.

**Key Finding:** All four features can be integrated as **middleware and services** without disrupting the existing architecture. The recommended approach adds 3 new middleware components, 2 new services, and extends the existing WebSocket and task infrastructure.

---

## Current Architecture Overview

### Existing System Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                         HTTP Layer                               │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────────────────┐ │
│  │  CORS   │  │ Logger  │  │Recovery │  │    Router Setup     │ │
│  │Middleware│  │Middleware│  │Middleware│  │   (router.go)      │ │
│  └─────────┘  └─────────┘  └─────────┘  └─────────────────────┘ │
├─────────────────────────────────────────────────────────────────┤
│                         Handler Layer                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│  │Subscription│ │ Download │ │  Config  │ │ Notification│          │
│  │ Handler   │ │ Handler  │ │ Handler  │ │  Handler   │          │
│  └─────┬────┘ └─────┬────┘ └─────┬────┘ └─────┬────┘           │
├────────┴────────────┴────────────┴────────────┴─────────────────┤
│                         Service Layer                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐ │
│  │Bangumi   │ │Downloader│ │  Mikan   │ │Notification│ │Calendar│ │
│  │ Service  │ │ Service  │ │ Service  │ │  Service   │ │ Service│ │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘ └────────┘ │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│  │  Disk    │ │   RSS    │ │Scheduler │ │  Task    │           │
│  │ Monitor  │ │  Parser  │ │ Service  │ │ Manager  │           │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘           │
├─────────────────────────────────────────────────────────────────┤
│                       Repository Layer                           │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│  │Subscription│ │ Download │ │  Config  │ │   Log    │           │
│  │    Repo   │ │   Repo   │ │   Repo   │ │   Repo   │           │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘           │
├─────────────────────────────────────────────────────────────────┤
│                        Storage Layer                             │
│                    ┌─────────────────┐                          │
│                    │     SQLite      │                          │
│                    │   (GORM ORM)    │                          │
│                    └─────────────────┘                          │
└─────────────────────────────────────────────────────────────────┘
```

### Current Middleware Chain (router.go)
```go
r.Use(middleware.Logger())
r.Use(middleware.Recovery())
r.Use(middleware.CORS())
```

### Current WebSocket Implementation
- **Location:** `internal/service/notification/websocket.go`
- **Pattern:** Hub-based with goroutine-per-client
- **Features:** Ping/pong, broadcast, client tracking
- **Missing:** Server-side reconnection state management

### Current Task Management
- **Location:** `internal/service/task/manager.go`
- **Pattern:** Singleton with single concurrent task
- **Features:** Progress tracking, cancellation, history
- **Limitation:** Only one task at a time, no queue

---

## New Components Required

### 1. JWT Authentication (SEC-03)

#### New Middleware
```
internal/api/middleware/
├── auth.go          # JWT validation middleware
└── auth_test.go     # Middleware tests
```

**Responsibility:** Extract, validate, and inject JWT claims into Gin context

**Integration Points:**
- Applied to protected route groups in `router.go`
- Injects `userID`, `claims` into Gin context for handlers
- Excludes public endpoints (health, login)

#### New Service
```
internal/service/auth/
├── service.go       # Token generation/validation
├── service_test.go  # Service tests
└── claims.go        # JWT claims structure
```

**Responsibility:** Token lifecycle management (issue, validate, refresh)

#### New Repository
```
internal/repository/
├── user.go          # User repository (if not exists)
└── token.go         # Refresh token storage
```

**Responsibility:** User lookup, refresh token persistence

#### Database Schema Additions
```sql
-- users table (if not exists)
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,  -- bcrypt hashed
    role TEXT DEFAULT 'user',     -- admin, user
    created_at DATETIME,
    updated_at DATETIME
);

-- refresh_tokens table
CREATE TABLE refresh_tokens (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL,
    token_hash TEXT UNIQUE NOT NULL,  -- SHA-256 of token
    expires_at DATETIME NOT NULL,
    created_at DATETIME,
    revoked_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
```

---

### 2. API Rate Limiting (SEC-04)

#### New Middleware
```
internal/api/middleware/
├── ratelimit.go          # Rate limiting middleware
└── ratelimit_test.go     # Tests
```

**Responsibility:** Enforce request rate limits per client

**Integration Points:**
- Applied globally or per-route in `router.go`
- Uses client IP + optional user ID as key
- Returns 429 with `Retry-After` header

#### Storage Options

| Option | Pros | Cons | Recommendation |
|--------|------|------|----------------|
| **In-Memory** | Fast, simple | Lost on restart, not shared | Development only |
| **SQLite** | Persistent, existing | Write contention | Single-instance |
| **Redis** | Fast, distributed | New dependency | Multi-instance |

**Recommended for Auto-RSS:** SQLite-based with in-memory cache layer

#### Database Schema Additions
```sql
-- rate_limit_counters table
CREATE TABLE rate_limit_counters (
    id INTEGER PRIMARY KEY,
    key TEXT UNIQUE NOT NULL,        -- ip:path or user:path
    window_start DATETIME NOT NULL,
    request_count INTEGER DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME
);

-- Indexes for cleanup
CREATE INDEX idx_rate_limit_window ON rate_limit_counters(window_start);
```

---

### 3. WebSocket Auto-Reconnection (INF-02)

#### Modified Components

**Existing:** `internal/service/notification/websocket.go`

**Additions:**
```go
// Client state tracking for reconnection
type ClientSession struct {
    ClientID    string
    UserID      uint          // For authenticated sessions
    ConnectedAt time.Time
    LastPing    time.Time
    MessageOffset uint64      // For message replay
}

// Message buffer for replay during reconnection
type MessageBuffer struct {
    messages []BufferedMessage
    maxSize  int
}
```

#### New Repository
```
internal/repository/
└── websocket_session.go    # Session persistence
```

**Responsibility:** Store session state for reconnection validation

#### Database Schema Additions
```sql
-- websocket_sessions table
CREATE TABLE websocket_sessions (
    id INTEGER PRIMARY KEY,
    client_id TEXT UNIQUE NOT NULL,
    user_id INTEGER,                 -- NULL for anonymous
    connected_at DATETIME,
    last_ping DATETIME,
    message_offset INTEGER DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME
);

-- websocket_message_buffer table (optional, for message replay)
CREATE TABLE websocket_message_buffer (
    id INTEGER PRIMARY KEY,
    client_id TEXT NOT NULL,
    message TEXT NOT NULL,
    offset INTEGER NOT NULL,
    created_at DATETIME,
    FOREIGN KEY (client_id) REFERENCES websocket_sessions(client_id)
);
```

**Integration Points:**
- WebSocket handler validates `client_id` query parameter
- Server maintains message buffer per session
- On reconnect: validate session, replay missed messages

---

### 4. Task Queue (INF-03)

#### New Service
```
internal/service/queue/
├── worker_pool.go       # Worker pool implementation
├── queue.go             # Job queue management
├── job.go               # Job definitions
├── scheduler.go         # Job scheduling
└── handlers.go          # Job type handlers
```

**Responsibility:** Manage concurrent background job execution

#### Architecture Pattern

```
┌─────────────────────────────────────────────────────────────┐
│                      Task Queue Service                      │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │  Job Queue   │───→│  Dispatcher  │───→│ Worker Pool  │  │
│  │   (chan)     │    │   (goroutine)│    │ (N workers)  │  │
│  └──────────────┘    └──────────────┘    └──────┬───────┘  │
│         ↑                                        │          │
│         └────────────┬───────────────────────────┘          │
│                      ↓                                       │
│              ┌──────────────┐                               │
│              │ SQLite Store │  (Persistence)                │
│              └──────────────┘                               │
└─────────────────────────────────────────────────────────────┘
```

#### Database Schema Additions
```sql
-- job_queue table
CREATE TABLE job_queue (
    id INTEGER PRIMARY KEY,
    job_id TEXT UNIQUE NOT NULL,
    job_type TEXT NOT NULL,          -- 'rss_refresh', 'download_check', etc.
    payload TEXT,                    -- JSON payload
    priority INTEGER DEFAULT 0,      -- Higher = sooner
    status TEXT DEFAULT 'pending',   -- pending, running, completed, failed
    attempts INTEGER DEFAULT 0,
    max_attempts INTEGER DEFAULT 3,
    error_message TEXT,
    scheduled_at DATETIME,           -- For delayed jobs
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME,
    updated_at DATETIME
);

-- job_results table (optional, for result storage)
CREATE TABLE job_results (
    id INTEGER PRIMARY KEY,
    job_id TEXT UNIQUE NOT NULL,
    result TEXT,                     -- JSON result
    created_at DATETIME,
    FOREIGN KEY (job_id) REFERENCES job_queue(job_id)
);

-- Indexes
CREATE INDEX idx_job_queue_status ON job_queue(status, priority, created_at);
CREATE INDEX idx_job_queue_scheduled ON job_queue(scheduled_at) WHERE status = 'pending';
```

**Integration Points:**
- Replaces singleton `task.Manager` with queue-based system
- Existing scheduler (`rssScheduler`) enqueues jobs instead of direct execution
- Handlers register job types with the queue service

---

## Integration Points with Existing Architecture

### 1. Router Integration

**Modified:** `internal/api/router/router.go`

```go
func Setup(db *gorm.DB, cfg *config.Config, qbClient downloader.QBittorrentClient, appCtx *app.Context) *gin.Engine {
    r := gin.New()

    // Global middleware (order matters)
    r.Use(middleware.Recovery())
    r.Use(middleware.Logger())
    r.Use(middleware.CORS())
    r.Use(middleware.RateLimit(db))        // NEW: Rate limiting

    // Initialize services
    authService := auth.NewService(db, cfg.JWTSecret)
    queueService := queue.NewService(db, 5) // 5 workers

    // Public routes (no auth)
    r.GET("/health", healthHandler)
    r.POST("/api/v1/auth/login", authHandler.Login)
    r.POST("/api/v1/auth/refresh", authHandler.Refresh)

    // Protected routes
    v1 := r.Group("/api/v1")
    v1.Use(middleware.Auth(authService))   // NEW: JWT auth
    {
        // ... existing handlers
    }

    // WebSocket with session support
    r.GET("/ws/notifications", notificationHandler.WebSocketHandler)
    // Client connects with: /ws/notifications?client_id=xxx&offset=yyy
}
```

### 2. Service Layer Integration

**Modified:** `internal/service/scheduler/scheduler.go`

```go
// Before: Direct execution
func (s *Scheduler) refreshRSS() {
    s.taskManager.StartTask(...) // Single task only
}

// After: Queue-based execution
func (s *Scheduler) refreshRSS() {
    s.queueService.Enqueue(queue.Job{
        Type:     "rss_refresh",
        Payload:  map[string]any{"source": "scheduled"},
        Priority: queue.PriorityNormal,
    })
}
```

### 3. Handler Layer Integration

**Modified:** Handlers access authenticated user via context

```go
func (h *SubscriptionHandler) Create(c *gin.Context) {
    // NEW: Get authenticated user from context
    userID, exists := c.Get("userID")
    if !exists {
        c.JSON(401, gin.H{"error": "unauthorized"})
        return
    }

    // Existing logic...
    var req CreateSubscriptionRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // Create subscription with user context
    sub, err := h.service.Create(userID.(uint), req)
    // ...
}
```

---

## Data Flow Changes

### Authentication Flow

```
┌─────────┐     ┌──────────────┐     ┌─────────────┐     ┌──────────────┐
│  Client │────→│ Login Handler │────→│ Auth Service │────→│  User Repo   │
└─────────┘     └──────────────┘     └─────────────┘     └──────────────┘
     ↑                                                          │
     │                                                          ↓
     │     ┌──────────────┐     ┌─────────────┐          ┌──────────────┐
     └─────│ JWT Response │←────│ Token Gen   │←─────────│ Password     │
           │(access/refresh)│    │(HS256)      │          │ Verify       │
           └──────────────┘     └─────────────┘          └──────────────┘

Subsequent Requests:
┌─────────┐     ┌──────────────┐     ┌─────────────┐     ┌──────────────┐
│  Client │────→│ Auth Middleware│───→│ Token Verify │────→│ Set Context  │
│(Bearer) │     │(validate JWT) │     │(HS256)       │     │(userID,claims)│
└─────────┘     └──────────────┘     └─────────────┘     └──────────────┘
```

### Rate Limiting Flow

```
Request → RateLimit Middleware → Key Generation (IP+User+Path)
                                      ↓
                              ┌───────────────┐
                              │ Check Counter │
                              │ (SQLite/Mem)  │
                              └───────┬───────┘
                                      ↓
                    ┌─────────────────┴─────────────────┐
                    ↓                                   ↓
              Within Limit                           Exceeded
                    ↓                                   ↓
              Allow Request                      Return 429
              Increment Counter              Retry-After header
```

### WebSocket Reconnection Flow

```
┌──────────────┐         ┌──────────────┐         ┌──────────────┐
│ Client Disconnect│────→│ Server Detect │────→│ Mark Session  │
│ (network issue)  │     │ (ping timeout)│     │ Disconnected  │
└──────────────┘         └──────────────┘         └──────────────┘
                                                          │
┌──────────────┐         ┌──────────────┐                │
│ Client Reconnect│────→│ Validate      │←───────────────┘
│ (same client_id)│     │ Session ID    │
└──────────────┘         └───────┬──────┘
                                 ↓
                    ┌──────────────────────┐
                    ↓                      ↓
              Valid Session           Invalid/Expired
                    ↓                      ↓
         ┌──────────────┐           ┌──────────────┐
         │ Replay Missed │           │ New Session  │
         │ Messages      │           │ Created      │
         └──────────────┘           └──────────────┘
```

### Task Queue Flow

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Trigger    │────→│ Enqueue Job  │────→│ Persist to   │────→│ Return JobID │
│(API/Schedule)│     │(queue.Service)│     │ SQLite       │     │ to Caller    │
└──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘
                                                  │
                                                  ↓
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Worker N   │←────│  Dispatcher  │←────│ Poll Pending │
│(Execute Job) │     │(assign job)  │     │ Jobs         │
└──────┬───────┘     └──────────────┘     └──────────────┘
       ↓
┌──────────────┐     ┌──────────────┐
│ Update Status│────→│ Notify Result│
│(completed/  │     │(WebSocket/  │
│  failed)     │     │  Callback)   │
└──────────────┘     └──────────────┘
```

---

## Suggested Build Order

Based on dependency analysis:

### Phase 1: Foundation (No Dependencies)
1. **JWT Authentication Service** (`internal/service/auth/`)
   - Token generation/validation logic
   - Claims structure
   - Unit tests

2. **Rate Limiting Storage** (`internal/repository/ratelimit.go`)
   - Counter storage interface
   - SQLite implementation
   - Cleanup logic

### Phase 2: Middleware Layer (Depends on Phase 1)
3. **Auth Middleware** (`internal/api/middleware/auth.go`)
   - JWT extraction and validation
   - Context injection
   - Integration with router

4. **Rate Limit Middleware** (`internal/api/middleware/ratelimit.go`)
   - Token bucket algorithm
   - Header injection
   - 429 response handling

### Phase 3: WebSocket Enhancement (Depends on Auth)
5. **WebSocket Session Management**
   - Session repository
   - Client state tracking
   - Message buffer (optional)

6. **WebSocket Auto-Reconnection**
   - Modify existing WebSocket handler
   - Session validation on connect
   - Message replay logic

### Phase 4: Task Queue (Independent, Complex)
7. **Queue Service Core** (`internal/service/queue/`)
   - Job struct and storage
   - Worker pool implementation
   - Dispatcher logic

8. **Queue Integration**
   - Replace task.Manager usages
   - Migrate scheduler to queue
   - Handler registration

### Phase 5: Integration & Testing
9. **Router Integration**
   - Apply middleware to routes
   - Protected route configuration
   - Handler updates for auth context

10. **End-to-End Testing**
    - Authentication flows
    - Rate limit enforcement
    - WebSocket reconnection
    - Concurrent job processing

---

## Component Dependency Graph

```
                    ┌─────────────────┐
                    │   Database      │
                    │   (SQLite)      │
                    └────────┬────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ↓                    ↓                    ↓
┌───────────────┐    ┌───────────────┐    ┌───────────────┐
│  Auth Service │    │ RateLimit Repo│    │  Queue Store  │
└───────┬───────┘    └───────┬───────┘    └───────┬───────┘
        │                    │                    │
        ↓                    ↓                    │
┌───────────────┐    ┌───────────────┐           │
│ Auth Middleware│   │ RateLimit     │           │
└───────┬───────┘    │ Middleware    │           │
        │            └───────┬───────┘           │
        │                    │                    │
        └────────────────────┼────────────────────┘
                             ↓
                    ┌─────────────────┐
                    │  Router Setup   │
                    │  (Integration)  │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              ↓              ↓              ↓
       ┌────────────┐ ┌────────────┐ ┌────────────┐
       │  WebSocket │ │  Handlers  │ │  Scheduler │
       │  Handler   │ │            │ │            │
       └────────────┘ └────────────┘ └────────────┘
              ↑                           │
              │                           ↓
              │                  ┌────────────┐
              │                  │   Queue    │
              │                  │  Service   │
              │                  └────────────┘
              │                           │
              └───────────────────────────┘
                    (Notifications)
```

---

## Architecture Decisions

### Decision 1: SQLite for Rate Limiting (Not Redis)
**Rationale:**
- Auto-RSS is single-instance deployment
- SQLite is already in use, no new dependency
- Write contention is acceptable for expected load (< 100 req/sec)
- Simpler operational model

**Trade-off:** Cannot scale to multi-instance without migration

### Decision 2: Token Bucket for Rate Limiting
**Rationale:**
- Allows burst traffic (user actions) while maintaining average rate
- Standard algorithm with well-understood properties
- `golang.org/x/time/rate` provides battle-tested implementation

### Decision 3: Queue Service Replaces Task Manager
**Rationale:**
- Existing `task.Manager` only supports single concurrent task
- Queue service provides same API with added concurrency
- Backward compatible: single task = queue with 1 worker

### Decision 4: WebSocket Sessions in SQLite
**Rationale:**
- Session data needs to survive server restarts
- Enables cross-instance sessions (future scaling)
- Small data volume (connections << 1000)

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| JWT secret management | Medium | High | Use env var, document rotation process |
| Rate limit memory leak | Low | Medium | Implement cleanup job, add metrics |
| Queue worker panic | Low | High | Panic recovery in workers, retry logic |
| WebSocket buffer overflow | Low | Medium | Limit buffer size, drop old messages |
| Migration conflicts | Medium | Medium | Versioned migrations, test on copy |

---

## Sources

- [appleboy/gin-jwt: JWT Middleware for Gin framework](https://github.com/appleboy/gin-jwt)
- [How to Handle JWT Authentication Securely in Go](https://oneuptime.com/blog/post/2026-01-07-go-jwt-authentication/view)
- [Building Production-Ready Rate Limiting Middleware in Go](https://ubogdan.com/2023/09/building-production-ready-rate-limiting-middleware-in-go/)
- [10 Best Practices for API Rate Limiting in 2025](https://zuplo.com/learning-center/10-best-practices-for-api-rate-limiting-in-2025/)
- [How to Implement Reconnection Logic for WebSockets](https://oneuptime.com/blog/post/2026-01-27-websocket-reconnection/view)
- [How to Implement Background Job Processing in Go](https://oneuptime.com/blog/post/2026-01-30-go-background-job-processing/view)
- [GitHub - saravanasai/goqueue: GoQueue](https://github.com/saravanasai/goqueue)
- [Go by Example: Worker Pools](https://gobyexample.com/worker-pools)

---

*Architecture research for: Auto-RSS v1.1 Infrastructure Features*
*Researched: 2026-04-05*
