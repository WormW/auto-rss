# Requirements: Auto-RSS v1.1 基础设施增强

**Defined:** 2026-04-06
**Core Value:** 添加核心基础设施功能，提升系统稳定性和安全性

---

## v1.1 Requirements

### Authentication (AUTH)

**Goal:** 实现 JWT 认证系统，保护 API 端点

- [ ] **AUTH-01**: 用户可以通过用户名/密码登录获取 JWT token
  - 实现 login 接口，验证用户名密码
  - 返回 access token 和 refresh token
  - 密码使用 bcrypt 哈希存储

- [ ] **AUTH-02**: 系统支持 access token (30分钟) 和 refresh token (7天)
  - Access token 有效期 30 分钟
  - Refresh token 有效期 7 天
  - Token 使用 HS256 签名

- [ ] **AUTH-03**: 实现 refresh token 轮换机制
  - 每次使用 refresh token 时发放新的 token 对
  - 旧的 refresh token 标记为已使用
  - 检测 refresh token 重用（安全特性）

- [ ] **AUTH-04**: 防止并发刷新导致的竞态条件
  - 使用数据库事务和 SELECT FOR UPDATE
  - 确保同一 refresh token 只能使用一次
  - 客户端队列化处理并发请求

### Rate Limiting (RATE)

**Goal:** 实现 API 限流保护，防止滥用

- [ ] **RATE-01**: 基于 IP 的限流（10 req/s，burst 20）
  - 使用令牌桶算法
  - 每 IP 独立计数
  - 可配置限流参数

- [ ] **RATE-02**: 返回标准的 X-RateLimit-* 响应头
  - X-RateLimit-Limit: 限流上限
  - X-RateLimit-Remaining: 剩余请求数
  - X-RateLimit-Reset: 重置时间

- [ ] **RATE-03**: 超限时返回 429 状态码和 Retry-After 头
  - HTTP 429 Too Many Requests
  - Retry-After 头指示何时可以重试
  - 可选的响应体说明限流详情

- [ ] **RATE-04**: 限流器内存使用有上限（TTL 清理）
  - 不活跃客户端的限流器 1 小时后清理
  - 最大缓存条目数限制（LRU 淘汰）
  - 可选：使用 SQLite 存储持久化计数

### WebSocket Reconnection (WS)

**Goal:** 实现 WebSocket 自动重连机制

- [ ] **WS-01**: 实现指数退避重连
  - 基础延迟 1 秒，最大 30 秒
  - 延迟 = min(1s × 2^attempt, 30s)
  - 最大重试次数 10 次（或无限）

- [ ] **WS-02**: 添加随机抖动防止惊群效应
  - 抖动范围 ±50%
  - 防止所有客户端同时重连
  - 使用随机数生成器

- [ ] **WS-03**: 断线期间的消息缓冲
  - 最多缓冲 100 条消息
  - 消息 TTL 5 分钟
  - 重连后按顺序发送缓冲的消息

- [ ] **WS-04**: 区分正常关闭和异常断开
  - 正常关闭（1000）不重连
  - 异常关闭（网络错误）触发重连
  - 用户登出时标记为正常关闭

### Task Queue (QUEUE)

**Goal:** 实现任务队列支持多并发下载

- [ ] **QUEUE-01**: 实现 worker pool 支持多并发任务
  - 4 个 worker 处理下载任务
  - 可配置的并发数
  - 使用 Go channel 进行任务分发

- [ ] **QUEUE-02**: 任务队列有背压保护
  - 队列最大长度 100
  - 队列满时拒绝新任务并返回错误
  - 提供队列长度监控接口

- [ ] **QUEUE-03**: SQLite WAL 模式避免写入冲突
  - 启用 WAL 模式（PRAGMA journal_mode=WAL）
  - 单 goroutine 序列化数据库写入
  - 忙等待重试机制（5秒超时）

- [ ] **QUEUE-04**: 任务状态持久化到数据库
  - 任务队列表：id, type, payload, status, priority, created_at
  - 支持任务优先级（高/中/低）
  - 任务完成/失败状态更新

---

## Future Requirements (v1.2+)

| ID | Requirement | Reason |
|----|-------------|--------|
| AUTH-05 | 多用户支持 | 需要数据库 schema 变更，单独规划 |
| AUTH-06 | OAuth 集成 | 超出当前范围 |
| RATE-05 | 基于用户的限流 | 多用户后实现 |
| RATE-06 | Redis 分布式限流 | 多实例部署后实现 |
| WS-05 | 服务器端消息重放 | 需要服务器端状态存储 |
| QUEUE-05 | 动态 worker 扩缩容 | 当前固定 worker 足够 |
| QUEUE-06 | 任务优先级队列 | FIFO 满足当前需求 |

---

## Out of Scope

| Feature | Reason |
|---------|--------|
| UI 界面改版 | 专注后端基础设施 |
| Plex/Jellyfin 集成 | v1.2 规划 |
| PostgreSQL 迁移 | v1.2 规划 |
| 容器编排优化 | 运维层面，非代码功能 |

---

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| AUTH-01 | Phase 4 | Pending |
| AUTH-02 | Phase 4 | Pending |
| AUTH-03 | Phase 4 | Pending |
| AUTH-04 | Phase 4 | Pending |
| RATE-01 | Phase 5 | Pending |
| RATE-02 | Phase 5 | Pending |
| RATE-03 | Phase 5 | Pending |
| RATE-04 | Phase 5 | Pending |
| WS-01 | Phase 6 | Pending |
| WS-02 | Phase 6 | Pending |
| WS-03 | Phase 6 | Pending |
| WS-04 | Phase 6 | Pending |
| QUEUE-01 | Phase 7 | Pending |
| QUEUE-02 | Phase 7 | Pending |
| QUEUE-03 | Phase 7 | Pending |
| QUEUE-04 | Phase 7 | Pending |

**Coverage:**
- v1.1 requirements: 16 total
- Mapped to phases: 16
- Unmapped: 0 ✓

---

*Requirements defined: 2026-04-06*
