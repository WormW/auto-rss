# Phase 4: JWT Authentication Foundation - Context

**Gathered:** 2026-04-06
**Status:** Ready for planning

<domain>
## Phase Boundary

实现 JWT 认证系统，保护 API 端点。用户可以通过用户名/密码登录获取 access token 和 refresh token，所有敏感 API 端点需要认证后才能访问。

</domain>

<decisions>
## Implementation Decisions

### User Credential Storage
- **D-01:** 用户名密码存储在 `.env` 环境变量文件
- **D-02:** 单用户模式（当前阶段），后续多用户再迁移到数据库
- **D-03:** 配置项：`JWT_USERNAME`, `JWT_PASSWORD`（明文或 bcrypt 哈希均可）

### Token Configuration
- **D-04:** Access token 有效期 30 分钟
- **D-05:** Refresh token 有效期 7 天
- **D-06:** Token 使用 HS256 签名算法
- **D-07:** JWT secret 存储在环境变量 `JWT_SECRET`

### Login & Security
- **D-08:** 登录失败返回 401，无延迟（固定延迟策略）
- **D-09:** 登录端点：`POST /api/v1/auth/login`
- **D-10:** Refresh 端点：`POST /api/v1/auth/refresh`

### Token Transport
- **D-11:** 使用 `Authorization: Bearer <token>` Header 传输

### Token Refresh Strategy
- **D-12:** Claude 决定最佳实践（推荐：检测重用即失效机制）

### API Protection Scope
- **D-13:** Claude 决定保护范围（推荐：除登录/刷新/健康检查外全部保护）

### Claude's Discretion
- Token 刷新策略具体实现（重用检测的严格程度）
- API 保护范围的具体端点列表
- JWT secret 轮换机制（如需要）
- Token 黑名单/吊销实现方式

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Authentication Requirements
- `.planning/REQUIREMENTS.md` § Authentication (AUTH-01~04) — JWT 认证需求规格

### Codebase Patterns
- `internal/api/middleware/cors.go` — 现有中间件实现模式
- `internal/api/router/router.go` — 路由注册方式
- `internal/config/config.go` — 配置加载模式

### External Libraries
- `github.com/golang-jwt/jwt/v5` — JWT 库（Go 标准选择）
- `github.com/gin-gonic/gin` — Web 框架中间件模式

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Gin Middleware 模式**：`internal/api/middleware/` 已有 Logger、Recovery、CORS 中间件，可直接参考实现 Auth 中间件
- **配置系统**：`internal/config/config.go` 使用 viper 加载环境变量，可扩展 JWT 相关配置
- **数据库连接**：GORM + SQLite 已初始化，refresh token 存储可直接使用

### Established Patterns
- **Handler 结构**：所有 handler 在 `internal/api/handler/` 目录，按功能分文件
- **路由注册**：在 `internal/api/router/router.go` 中统一注册
- **错误处理**：统一返回 JSON 格式 `{"code": xxx, "message": "..."}`

### Integration Points
- **Middleware 挂载**：`router.go` 第 29-31 行已有中间件挂载点
- **API v1 路由组**：`router.go` 第 83 行的 `v1` Group 需要添加 auth 中间件
- **数据库迁移**：`internal/pkg/database/database.go` 需要添加 refresh_tokens 表（如需服务端存储）

</code_context>

<specifics>
## Specific Ideas

- 单用户模式简化实现，但预留多用户扩展接口
- 参考 requirements 中的 AUTH-03/04 要求实现 token 轮换和竞态条件保护
- 前端需要处理 401 响应，跳转到登录页

</specifics>

<deferred>
## Deferred Ideas

- 多用户支持（Phase 5+）— 需要数据库 schema 变更
- RBAC 权限系统 — 超出当前范围
- OAuth/SSO 集成 — 未来可选功能
- 密码强度策略 — 单用户模式暂不紧急

</deferred>

---

*Phase: 04-jwt-authentication-foundation*
*Context gathered: 2026-04-06*
