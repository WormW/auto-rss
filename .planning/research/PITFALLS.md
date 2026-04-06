# Domain Pitfalls: v1.1 Infrastructure Features

**Domain:** Auto-RSS Go/Gin application adding JWT auth, rate limiting, WebSocket reconnection, and task queue
**Researched:** 2026-04-05
**Confidence:** HIGH

## Critical Pitfalls

### Pitfall 1: JWT Algorithm Confusion Attack

**What goes wrong:**
Attackers manipulate the `alg` header in JWT tokens, switching from RS256 (asymmetric) to HS256 (symmetric). The application then uses the RS256 public key as an HMAC secret, allowing attackers to forge valid tokens with any payload.

**Why it happens:**
- Libraries naively trust the client-specified algorithm in the JWT header
- Developers don't explicitly whitelist allowed algorithms
- Brownfield systems often retrofit JWT without understanding algorithm implications

**How to avoid:**
```go
// WRONG: Trusts the algorithm from token header
token, err := jwt.Parse(tokenString, func(token *jwt.Token) {
    return secret, nil
})

// CORRECT: Explicitly whitelist allowed algorithms
token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
    if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
        return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
    }
    return secret, nil
}, jwt.WithValidMethods([]string{"HS256"}))
```
- Always use `jwt.WithValidMethods()` to whitelist algorithms
- Never use `alg: none` — explicitly reject it
- Use asymmetric keys (RS256/ES256) for production, never share secrets

**Warning signs:**
- JWT validation succeeds with unexpected algorithm in header
- Tokens validate despite being signed with different keys
- Security scanners flag "algorithm confusion" vulnerability

**Phase to address:** SEC-03 (JWT Authentication System)

---

### Pitfall 2: SQLite Write Contention with Concurrent Task Queue

**What goes wrong:**
When implementing multi-concurrent downloads with SQLite, multiple goroutines attempt simultaneous writes, causing `SQLITE_BUSY` errors, connection timeouts, or in worst cases, database corruption.

**Why it happens:**
- SQLite allows unlimited concurrent readers but only ONE writer at a time
- Task queue workers compete for write access when updating task status
- Default SQLite journal mode blocks readers during writes
- No retry logic causes cascading failures under load

**How to avoid:**
```go
// WRONG: Multiple goroutines writing directly
go func() { db.Exec("UPDATE tasks SET status=?", done) }() // Worker 1
go func() { db.Exec("UPDATE tasks SET status=?", done) }() // Worker 2 - CONFLICT!

// CORRECT: Single writer goroutine with channel
type DBQueue struct {
    tasks chan DBTask
    db    *sql.DB
}

func (q *DBQueue) Start() {
    go func() {
        for task := range q.tasks {
            // Single goroutine = serialized writes, no contention
            q.execute(task)
        }
    }()
}
```
- Enable WAL mode: `PRAGMA journal_mode=WAL` (essential for concurrency)
- Separate concurrent I/O (downloads) from serialized DB writes
- Implement exponential backoff with jitter for transient `SQLITE_BUSY` errors
- Use a single writer goroutine with channels to serialize all writes
- Keep transactions short — don't hold locks during network operations

**Warning signs:**
- `database is locked (5) (SQLITE_BUSY)` errors in logs
- Task updates intermittently failing under concurrent load
- Gradual performance degradation as concurrency increases
- Database corruption warnings or checksum failures

**Phase to address:** INF-03 (Task Queue Support)

---

### Pitfall 3: WebSocket Thundering Herd on Reconnection

**What goes wrong:**
When the server restarts or network issues resolve, all disconnected clients reconnect simultaneously using fixed or synchronized intervals, overwhelming the server and causing cascading failures.

**Why it happens:**
- Fixed retry intervals cause synchronized reconnection attempts
- No jitter means all clients calculate the same next retry time
- Missing exponential backoff leads to immediate aggressive retries
- No circuit breaker allows infinite reconnection attempts

**How to avoid:**
```go
// WRONG: Fixed retry interval causes thundering herd
time.Sleep(5 * time.Second) // All clients retry at same time!

// CORRECT: Exponential backoff with jitter
func calculateBackoff(attempt int) time.Duration {
    baseDelay := 1 * time.Second
    maxDelay := 30 * time.Second
    multiplier := 2.0
    jitter := 0.5
    
    delay := float64(baseDelay) * math.Pow(multiplier, float64(attempt))
    if delay > float64(maxDelay) {
        delay = float64(maxDelay)
    }
    
    // Add jitter to prevent synchronized storms
    jitterRange := delay * jitter
    jitteredDelay := delay + (rand.Float64()*2-1)*jitterRange
    
    return time.Duration(math.Max(0, jitteredDelay))
}
```
- Use exponential backoff (1s → 2s → 4s → 8s... capped at 30s)
- Add jitter (±50% randomization) to desynchronize clients
- Implement max retry limits (10-20 attempts) or circuit breaker
- Distinguish intentional closes from errors (don't reconnect on logout)

**Warning signs:**
- Server CPU/memory spikes immediately after restart
- Connection accept queue overflows
- Log shows burst of connections at regular intervals
- Clients getting connection refused despite server being up

**Phase to address:** INF-02 (WebSocket Auto-Reconnection)

---

### Pitfall 4: Rate Limiting Memory Exhaustion

**What goes wrong:**
In-memory rate limiters (token bucket/sliding window) consume unbounded memory as the number of unique clients grows, eventually causing OOM crashes.

**Why it happens:**
- Each unique client ID creates a new rate limiter entry
- No TTL or cleanup for inactive client entries
- DDoS attacks or crawlers generate millions of unique identifiers
- Memory grows linearly with client count, not request rate

**How to avoid:**
```go
// WRONG: Unbounded map growth
var limiters = make(map[string]*rate.Limiter) // Never cleaned!

// CORRECT: TTL-based cleanup with size limits
type RateLimiterCache struct {
    limiters map[string]*LimiterEntry
    maxSize  int
    ttl      time.Duration
    mu       sync.RWMutex
}

type LimiterEntry struct {
    limiter    *rate.Limiter
    lastAccess time.Time
}

func (c *RateLimiterCache) cleanup() {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    for key, entry := range c.limiters {
        if time.Since(entry.lastAccess) > c.ttl {
            delete(c.limiters, key)
        }
    }
}
```
- Implement TTL for rate limiter entries (e.g., 1 hour inactivity)
- Set maximum cache size with LRU eviction
- Use Redis for distributed rate limiting (handles cleanup automatically)
- Consider fixed window algorithm for memory-constrained scenarios

**Warning signs:**
- Memory usage grows steadily without corresponding traffic increase
- OOM kills during traffic spikes
- GC pause times increasing
- Rate limiter response times degrading

**Phase to address:** SEC-04 (API Rate Limiting)

---

### Pitfall 5: JWT Secret Key Exposure in Configuration

**What goes wrong:**
Hardcoded or environment-variable JWT secrets are committed to version control, exposed in logs, or leaked through error messages, allowing attackers to forge tokens.

**Why it happens:**
- Quick implementation uses hardcoded secrets for testing, never changed
- Secrets stored in plain text environment files (.env) committed to git
- Error messages include sensitive configuration details
- No key rotation mechanism exists

**How to avoid:**
```go
// WRONG: Hardcoded secret
var jwtSecret = []byte("my-super-secret-key-123")

// CORRECT: Load from secure vault with validation
func loadJWTSecret() ([]byte, error) {
    secret := os.Getenv("JWT_SECRET")
    if len(secret) < 32 {
        return nil, errors.New("JWT_SECRET must be at least 32 bytes")
    }
    return []byte(secret), nil
}

// Add to .gitignore
// .env
// *.key
// secrets/
```
- Minimum 32-byte secrets (256 bits for HS256)
- Use secret management (HashiCorp Vault, AWS Secrets Manager)
- Implement automated key rotation
- Never log tokens or secrets
- Add `.env` and key files to `.gitignore`

**Warning signs:**
- Secrets found in git history (use `git-secrets` or `truffleHog`)
- Error messages containing configuration values
- No process for rotating compromised keys
- Same secret used across environments

**Phase to address:** SEC-03 (JWT Authentication System)

---

### Pitfall 6: WebSocket Message Loss During Disconnection

**What goes wrong:**
Messages sent while the WebSocket is disconnected are silently dropped, causing users to miss critical updates (download completions, errors).

**Why it happens:**
- No message queuing mechanism exists
- Application doesn't track connection state before sending
- Failed writes aren't retried or buffered
- No acknowledgment mechanism for important messages

**How to avoid:**
```go
type MessageQueue struct {
    queue      []QueuedMessage
    maxSize    int           // Cap at 1000
    messageTTL time.Duration // 5 minutes
    mu         sync.Mutex
}

func (r *ReconnectingWebSocket) Send(data interface{}) error {
    if r.isConnected && r.conn.ReadyState() == websocket.Open {
        return r.conn.WriteJSON(data)
    }
    // Queue for later delivery
    id := r.messageQueue.Enqueue(data, 0)
    log.Printf("Message %s queued (queue size: %d)", id, r.messageQueue.Size())
    return nil
}

func (r *ReconnectingWebSocket) flushQueue() {
    messages := r.messageQueue.DequeueAll()
    for _, msg := range messages {
        if err := r.conn.WriteJSON(msg); err != nil {
            r.messageQueue.Requeue(msg) // Re-queue on failure
            break
        }
    }
}
```
- Implement in-memory message queue with size limits
- Add TTL to prevent stale message accumulation
- Flush queue on successful reconnection
- Prioritize critical messages (errors > progress updates)
- Consider persistent queue (localStorage) for critical data

**Warning signs:**
- Users report missing notifications
- Download completions not reflected in UI
- Inconsistent state between server and client
- Message sequence gaps in logs

**Phase to address:** INF-02 (WebSocket Auto-Reconnection)

---

### Pitfall 7: Rate Limiting Breaking Existing API Clients

**What goes wrong:**
Adding rate limiting to an existing API suddenly breaks legitimate clients (frontend polling, mobile apps, scripts) that were making requests at higher frequencies.

**Why it happens:**
- No baseline measurement of existing traffic patterns
- Aggressive limits applied globally without gradual rollout
- Frontend polling intervals exceed new rate limits
- No grace period or client notification

**How to avoid:**
```go
// Phase 1: Monitor-only mode (log violations, don't block)
func rateLimitMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        key := c.ClientIP()
        if !limiter.Allow(key) {
            log.Printf("[RATE_LIMIT_WOULD_BLOCK] client=%s path=%s", key, c.Request.URL.Path)
            // Still allow request in monitoring mode
        }
        c.Next()
    }
}

// Phase 2: Soft enforcement (warn headers, allow with delay)
// Phase 3: Hard enforcement (429 responses)
```
- Measure existing traffic for 1-2 weeks before enforcement
- Implement monitoring-only mode first (log but don't block)
- Add `X-RateLimit-*` headers before enforcement
- Use different limits for different client types
- Provide migration guide for API consumers

**Warning signs:**
- Sudden spike in 429 responses after deployment
- User complaints about broken integrations
- Frontend errors or timeouts
- Support tickets about API instability

**Phase to address:** SEC-04 (API Rate Limiting)

---

### Pitfall 8: Refresh Token Race Conditions

**What goes wrong:**
Multiple concurrent requests using the same refresh token cause race conditions where one request invalidates the token before others can use it, forcing users to re-authenticate.

**Why it happens:**
- Refresh token rotation invalidates old token immediately
- Concurrent API calls from same client all attempt refresh
- No synchronization between token refresh attempts
- Client doesn't serialize requests during token refresh

**How to avoid:**
```go
// Server-side: Mark as used atomically
func (s *AuthService) RefreshToken(refreshToken string) (*TokenPair, error) {
    // Use database transaction for atomic check-and-update
    tx, err := s.db.Begin()
    
    // Check if token exists AND is unused
    var token RefreshToken
    err = tx.QueryRow(
        "SELECT * FROM refresh_tokens WHERE token = ? AND used = false FOR UPDATE",
        refreshToken,
    ).Scan(&token)
    
    if err != nil {
        tx.Rollback()
        return nil, ErrInvalidToken
    }
    
    // Mark as used
    tx.Exec("UPDATE refresh_tokens SET used = true WHERE token = ?", refreshToken)
    
    // Generate new pair
    newPair := s.generateTokenPair()
    tx.Commit()
    
    return newPair, nil
}
```
- Use database transactions with `SELECT FOR UPDATE` for atomic operations
- Implement refresh token reuse detection (security feature)
- Client-side: Queue requests during token refresh, retry with new token
- Consider short grace period where old refresh token still works (seconds)

**Warning signs:**
- Users randomly logged out during normal usage
- Multiple refresh requests in quick succession in logs
- "Invalid refresh token" errors for legitimate users
- Pattern correlates with pages making multiple concurrent API calls

**Phase to address:** SEC-03 (JWT Authentication System)

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| In-memory rate limiter | Simple, no external deps | Memory exhaustion, no distributed support | Single-instance deployment only, < 1000 unique clients |
| Skip refresh token rotation | Simpler implementation, stateless | Compromised refresh tokens grant indefinite access | Never in production — use rotation with reuse detection |
| Fixed retry intervals | Easy to implement | Thundering herd, server overload | Never — always use exponential backoff with jitter |
| Direct DB access from workers | Less code complexity | SQLite contention, corruption risk | Never with SQLite — always serialize writes |
| Long-lived access tokens (24h+) | Fewer refresh operations | Larger window for token theft exploitation | Never — keep access tokens under 30 minutes |
| Skip message queue for WebSocket | Simpler code | Silent message loss during disconnections | Only for non-critical updates (heartbeats) |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| JWT with existing routes | Modifying every handler for auth check | Use Gin route groups with middleware: `protected.Use(authMiddleware)` |
| Rate limiting with Gin | Applying middleware globally | Use route-specific groups: `public` vs `protected` with different limits |
| WebSocket with existing auth | Sharing HTTP auth headers | Implement separate token validation for WebSocket upgrade, or use cookie-based auth |
| Task queue with existing scheduler | Replacing existing scheduler entirely | Gradual migration — run both in parallel, migrate tasks incrementally |
| SQLite with concurrent workers | One connection per goroutine | Use connection pool with `_busy_timeout=5000` and single writer pattern |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Unbounded JWT blacklist | Memory growth, slower validation | Use TTL-based cache, Redis with expiration | 1000+ logged-out tokens |
| Per-request DB rate limiter | High latency, DB overload | Use in-memory cache with periodic sync | > 100 req/s |
| WebSocket without heartbeat | Silent dead connections, missed updates | Application-level ping/pong every 30s | Network with NAT timeouts (5 min) |
| Synchronous download + DB write | Slow downloads, poor throughput | Decouple download completion from DB update | > 5 concurrent downloads |
| No connection pooling for SQLite | File descriptor exhaustion, slow opens | Use `sql.DB` with `SetMaxOpenConns(1)` for writes | > 50 concurrent operations |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Storing JWT in localStorage | XSS vulnerability, token theft | Use `HttpOnly`, `Secure`, `SameSite=Strict` cookies |
| Missing `exp` claim validation | Stolen tokens valid forever | Always verify `exp`, use short TTLs (15-30 min) |
| No rate limit on login endpoint | Brute force attacks | Implement strict rate limiting (5 attempts/minute) |
| Logging JWT tokens | Credential exposure in logs | Redact tokens in logging middleware |
| Missing CORS configuration | CSRF attacks from malicious sites | Explicit allowlist for origins, credentials handling |
| WebSocket without origin check | Cross-site WebSocket hijacking | Validate `Origin` header on upgrade request |
| Hardcoded JWT secrets in tests | Secrets committed to git | Use test-specific secrets, different from production |

## "Looks Done But Isn't" Checklist

- [ ] **JWT Authentication:** Token refresh rotation implemented with reuse detection — verify concurrent refresh doesn't log users out
- [ ] **JWT Authentication:** Algorithm explicitly whitelisted — verify `none` algorithm is rejected
- [ ] **JWT Authentication:** Secret is 32+ bytes, loaded from environment — verify not hardcoded
- [ ] **Rate Limiting:** Memory usage bounded with TTL/cleanup — verify no unbounded growth
- [ ] **Rate Limiting:** Different limits for authenticated vs anonymous — verify public endpoints protected
- [ ] **WebSocket Reconnection:** Exponential backoff with jitter implemented — verify no thundering herd on reconnect
- [ ] **WebSocket Reconnection:** Message queue with size limits — verify messages not lost during disconnect
- [ ] **WebSocket Reconnection:** Manual close detection — verify logout doesn't trigger reconnection
- [ ] **Task Queue:** SQLite WAL mode enabled — verify `PRAGMA journal_mode` returns `wal`
- [ ] **Task Queue:** Single writer goroutine for DB — verify no `SQLITE_BUSY` errors under load
- [ ] **Task Queue:** Retry logic with exponential backoff — verify transient failures recover automatically
- [ ] **Integration:** Existing API routes still work without auth (if intended) — verify backward compatibility

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Algorithm confusion exploited | HIGH | 1. Rotate all signing keys immediately<br>2. Invalidate all existing tokens<br>3. Force re-authentication for all users<br>4. Audit logs for forged tokens |
| SQLite corruption | HIGH | 1. Restore from backup<br>2. Run `PRAGMA integrity_check`<br>3. Export/import to new database if damaged<br>4. Implement WAL mode before restart |
| Rate limiter OOM | MEDIUM | 1. Restart service with emergency config<br>2. Implement TTL cleanup<br>3. Switch to Redis-based limiting<br>4. Add memory monitoring alerts |
| WebSocket thundering herd | LOW | 1. Scale up server capacity temporarily<br>2. Deploy fix with jitter<br>3. Gradually restart clients if possible<br>4. Implement circuit breaker |
| JWT secret leaked | HIGH | 1. Rotate secret immediately<br>2. Invalidate all tokens<br>3. Force logout all users<br>4. Audit for unauthorized access<br>5. Review secret management practices |
| Message queue overflow | LOW | 1. Increase queue size temporarily<br>2. Add queue size metrics/alerting<br>3. Implement message prioritization<br>4. Consider persistent queue for critical messages |

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|-------------|---------------|------------|
| SEC-03: JWT Implementation | Algorithm confusion, secret exposure | Code review checklist, security scanning with `gosec` |
| SEC-03: Token Refresh | Race conditions on concurrent refresh | Database transaction with `FOR UPDATE`, client-side request queuing |
| SEC-04: Rate Limiting | Breaking existing clients | Monitoring-only mode first, gradual enforcement, client communication |
| SEC-04: Rate Limiting | Memory exhaustion | Implement TTL, size limits, or use Redis |
| INF-02: WebSocket Reconnection | Thundering herd on server restart | Exponential backoff with jitter, max retry limits |
| INF-02: WebSocket Reconnection | Message loss during disconnect | Implement message queue with size limits and TTL |
| INF-03: Task Queue | SQLite contention and corruption | WAL mode, single writer goroutine, retry logic |
| INF-03: Task Queue | Goroutine leaks on task cancellation | Context cancellation propagation, proper cleanup |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| JWT Algorithm Confusion | SEC-03 | Security scan with JWT manipulation tests |
| SQLite Write Contention | INF-03 | Load test with 50+ concurrent tasks, verify no `SQLITE_BUSY` |
| WebSocket Thundering Herd | INF-02 | Simulate server restart, verify staggered reconnections |
| Rate Limiter Memory Exhaustion | SEC-04 | Memory profiling under load, verify bounded growth |
| JWT Secret Exposure | SEC-03 | Git history scan, secret detection in CI |
| WebSocket Message Loss | INF-02 | Disconnect test, verify message delivery after reconnect |
| Rate Limit Breaking Clients | SEC-04 | Traffic analysis, gradual rollout with monitoring |
| Refresh Token Race Conditions | SEC-03 | Concurrent refresh test, verify single token issuance |

## Sources

- [JWT Implementation Pitfalls, Security Threats - SlashID](https://www.slashid.dev/blog/jwt-risks/) — HIGH confidence
- [JWT Security Pitfalls and Best Practices 2025](https://www.usefulfunctions.co.uk/2025/11/05/jwt-security-pitfalls-and-best-practices/) — HIGH confidence
- [JWT Security Pitfalls - MojoAuth](https://mojoauth.com/ciam-qna/jwt-security-pitfalls-implementation) — HIGH confidence
- [Go JWT Authentication Best Practices - OneUptime](https://oneuptime.com/blog/post/2026-01-07-go-jwt-authentication/view) — HIGH confidence
- [Refresh Token Rotation Best Practices - Descope](https://www.descope.com/blog/post/refresh-token-rotation) — HIGH confidence
- [API Rate Limiting Best Practices 2025 - Zuplo](https://zuplo.com/blog/2025/01/06/10-best-practices-for-api-rate-limiting-in-2025) — HIGH confidence
- [WebSocket Reconnection Strategies - OneUptime](https://oneuptime.com/blog/post/2026-01-27-websocket-reconnection/view) — HIGH confidence
- [SQLite Concurrency Issues - go-sqlite3 GitHub](https://github.com/mattn/go-sqlite3/issues/1179) — HIGH confidence
- [SQLite Concurrency Best Practices - LinkedIn](https://www.linkedin.com/posts/inai-wiki_mastering-sqlite-concurrency-in-go-insights-activity-7437218856991031296-GChJ) — MEDIUM confidence
- [Homebox SQLite Concurrency Issue](https://github.com/hay-kot/homebox/issues/669) — HIGH confidence (real-world failure case)
- [Gin JWT Middleware - appleboy/gin-jwt](https://github.com/appleboy/gin-jwt) — HIGH confidence

---
*Pitfalls research for: Auto-RSS v1.1 Infrastructure Features*
*Researched: 2026-04-05*
