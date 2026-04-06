# Phase 5: API Rate Limiting - Context

**Gathered:** 2026-04-06
**Status:** Ready for planning

<domain>
## Phase Boundary

API 端点受到限流保护，防止滥用和 DoS 攻击。每个 IP 地址的请求被限制在 10 req/s（burst 20），登录端点使用更严格的限流策略（5 req/min）。系统返回标准的 X-RateLimit-* 响应头，超限时返回 429 状态码和 Retry-After 头。

</domain>

<decisions>
## Implementation Decisions

### Rate Limit Scope
- **D-01:** 除 `/health` 健康检查端点外，所有 API 端点启用限流
- **D-02:** 登录/刷新接口（`/api/v1/auth/login`, `/api/v1/auth/refresh`）使用独立限流策略：5 req/min，burst 5
- **D-03:** 已认证用户和未认证用户使用相同的 IP 级限流（后续多用户阶段可扩展为用户级限流）

### Storage Backend
- **D-04:** 使用内存存储（in-memory），基于令牌桶算法实现
- **D-05:** 不活跃客户端的限流数据 1 小时后自动清理（TTL）
- **D-06:** 最大缓存条目数 10,000，超出时 LRU 淘汰

### Rate Limit Headers
- **D-07:** 每个响应都包含标准限流头（不只是快超限时）
- **D-08:** 429 错误响应中同样包含 X-RateLimit-* 头
- **D-09:** 响应头包含：X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset

### Configuration
- **D-10:** 限流参数通过 `.env` 文件配置，有合理默认值
- **D-11:** 配置项：RATE_LIMIT_RPS (默认10), RATE_LIMIT_BURST (默认20), RATE_LIMIT_AUTH_RPM (默认5)

### Claude's Discretion
- 令牌桶算法的具体实现细节（可使用开源库如 `golang.org/x/time/rate` 或自实现）
- LRU 淘汰策略的具体实现
- 中间件的挂载顺序（限流中间件应在日志之后、认证之前）
- 限流数据的内部存储结构

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Rate Limiting Requirements
- `.planning/REQUIREMENTS.md` § Rate Limiting (RATE-01~04) — 限流需求规格

### Codebase Patterns
- `internal/api/middleware/auth.go` — 中间件实现模式（参考）
- `internal/api/middleware/cors.go` — 简单中间件示例
- `internal/api/router/router.go` — 路由注册和中间件挂载
- `internal/config/config.go` — 配置加载模式

### External Libraries (Candidate)
- `golang.org/x/time/rate` — Go 官方令牌桶实现（推荐）
- `github.com/gin-gonic/gin` — Web 框架中间件模式

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Gin Middleware 模式**：`internal/api/middleware/` 已有 Logger、Recovery、CORS、Auth 中间件，Rate Limit 中间件可直接参考
- **配置系统**：`internal/config/config.go` 使用 viper 加载环境变量，可扩展限流相关配置
- **IP 获取**：`c.ClientIP()` 已用于日志记录，限流可直接复用

### Established Patterns
- **中间件挂载**：`router.go` 第 29-32 行挂载中间件，限流中间件应放在 Recovery 之后、CORS 之前或之后
- **错误响应**：统一返回 JSON 格式 `{"code": 429, "message": "..."}`
- **结构化日志**：使用 `internal/pkg/logger` 记录限流触发事件

### Integration Points
- **中间件挂载位置**：`router.go` 第 29 行之后添加限流中间件
- **认证中间件配合**：限流应在认证之前执行（防止认证计算资源被耗尽）
- **公开路由排除**：健康检查 `/health` 和认证路由 `/api/v1/auth/*` 需要特殊处理

</code_context>

<specifics>
## Specific Ideas

- 登录端点限流更严格，防止暴力破解攻击
- 内存存储足够应对单机部署场景，后续如需多实例可迁移到 Redis
- 429 错误响应体可包含友好的中文提示："请求过于频繁，请稍后再试"

</specifics>

<deferred>
## Deferred Ideas

- 用户级限流（RATE-05）— 多用户支持后再实现
- Redis 分布式限流（RATE-06）— 多实例部署后再实现
- 动态限流调整 API — 管理功能，当前不需要
- 限流统计面板 — 监控功能，可选

</deferred>

---

*Phase: 05-api-rate-limiting*
*Context gathered: 2026-04-06*
