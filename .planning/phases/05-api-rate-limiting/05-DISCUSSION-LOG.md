# Phase 5: API Rate Limiting - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-06
**Phase:** 05-api-rate-limiting
**Areas discussed:** Scope & Strategy, Storage Backend, Rate Limit Headers, Configuration Approach

---

## Scope & Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| 全部限流 | 所有端点统一限流 | |
| 排除健康检查 | 除 `/health` 外全部限流 | ✓ |
| 登录更严格 | 登录接口单独更严格（5 req/min）| ✓ |
| 区分认证状态 | 已认证用户不同限流策略 | |

**User's choice:** 排除健康检查，登录接口单独限流
**Notes:** 防止暴力破解，健康检查需要保持可用性

---

## Storage Backend

| Option | Description | Selected |
|--------|-------------|----------|
| 内存存储 | 令牌桶算法，TTL清理 | ✓ |
| SQLite | 持久化存储，可统计历史 | |
| Redis | 分布式限流，多实例支持 | |

**User's choice:** 内存存储
**Notes:** 单机部署够用，简单高效

---

## Rate Limit Headers

| Option | Description | Selected |
|--------|-------------|----------|
| 所有响应 | 每个响应都包含限流头 | ✓ |
| 接近限流时 | 只在快超限时返回 | |
| 仅错误响应 | 只在429时返回 | |

**User's choice:** 所有响应都包含
**Notes:** 客户端可以预判剩余额度

---

## Configuration Approach

| Option | Description | Selected |
|--------|-------------|----------|
| 硬编码 | 代码里固定值 | |
| `.env` 配置 | 环境变量可配置，有默认值 | ✓ |
| 数据库配置 | 运行时动态调整 | |

**User's choice:** `.env` 配置文件
**Notes:** 符合现有配置模式，简单够用

---

## Claude's Discretion

- 令牌桶算法实现细节
- LRU 淘汰策略实现
- 中间件挂载顺序
- 限流数据内部存储结构

## Deferred Ideas

- 用户级限流 — 多用户后实现
- Redis 分布式限流 — 多实例后实现
- 动态限流调整 API — 管理功能
