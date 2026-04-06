# Phase 4: JWT Authentication Foundation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-06
**Phase:** 04-jwt-authentication-foundation
**Areas discussed:** User credential storage, Login security, Token refresh, API protection, Token transport

---

## User Credential Storage

| Option | Description | Selected |
|--------|-------------|----------|
| 环境变量存储 | 用户名密码存储在 .env 文件，简单但不支持修改 | ✓ |
| 数据库存储 | 用户名哈希存储在 configs 表，支持运行时修改密码 | |
| 专用用户表 | 创建 users 表，为后续多用户做准备 | 初始选择，后撤回 |

**User's choice:** 环境变量存储（.env 文件）
**Notes:** 用户初始选择专用用户表，后撤回改为环境变量存储。单用户模式暂时只有一个账号。

---

## Login Failure Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| 固定延迟 | 失败返回 401，无延迟（简单但易被暴力破解） | ✓ |
| 指数退避 | 连续失败增加延迟（内置 memory 存储，重启清零） | |
| 锁定账户 | 5次失败锁定 15 分钟（需要额外状态存储） | |

**User's choice:** 固定延迟
**Notes:** 选择最简单的实现方式

---

## Token Refresh Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| 检测重用即失效 | refresh token 被第二次使用时，认为被盗，吊销该用户所有 token | ✓ |
| 仅标记旧 token | 正常轮换，不检测重用 | |
| Claude 决定 | 按最佳实践实现 | |

**User's choice:** Claude 决定
**Notes:** 委托 Claude 选择最佳实践（推荐检测重用即失效）

---

## API Protection Scope

| Option | Description | Selected |
|--------|-------------|----------|
| 全部保护 | 除登录/刷新外所有端点都需要 JWT | |
| 仅写操作保护 | GET 请求公开，POST/PUT/DELETE 需要认证 | |
| 你决定 | 根据最佳实践判断 | ✓ |

**User's choice:** Claude 决定
**Notes:** 委托 Claude 决定保护范围（推荐除登录/刷新/健康检查外全部保护）

---

## Token Transport Method

| Option | Description | Selected |
|--------|-------------|----------|
| Authorization Header | 标准 Bearer token，前端需要手动管理 | ✓ |
| HttpOnly Cookie | 防止 XSS，但需要处理 CSRF | |
| 两者都支持 | 优先 Header，fallback Cookie | |

**User's choice:** Authorization Header
**Notes:** 标准 Bearer token 方案

---

## Claude's Discretion

- Token 刷新策略具体实现（检测重用即失效）
- API 保护范围的具体端点列表
- JWT secret 轮换机制

## Deferred Ideas

- 多用户支持 — Phase 5+ 规划
- RBAC 权限系统 — 超出当前范围
- OAuth/SSO 集成 — 未来可选功能
