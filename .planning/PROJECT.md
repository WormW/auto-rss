# Auto-RSS

## Current State

**Shipped:** v1.1 基础设施增强 (2026-04-06)

Auto-RSS 是一个自动化动漫下载系统，已完成基础设施增强，具备 JWT 认证、API 限流和 WebSocket 自动重连能力。

**Stats:**
- 代码库：~27,000 行 Go 代码
- 测试覆盖：Handler 层、Service 层、文件操作、并发安全、限流、WebSocket
- 架构：服务化设计，10+ 独立服务组件

---

## What This Is

基于 v1.0 技术债务清理成果，进一步增强基础设施：实现 JWT 认证系统保护 API 安全，添加 Token Bucket 限流防止 API 滥用，WebSocket 自动重连确保实时通知可靠性。系统现在更安全、更稳定、更可靠。

<details>
<summary>v1.0 技术债务清理 (Archived)</summary>

对 Auto-RSS 项目进行系统性的技术债务清理，修复了已知 Bug、安全漏洞，重构了臃肿代码。

**Key Deliverables:**
- 7 个关键 Bug 修复
- 10 个服务组件重构
- 性能优化 (N+1 查询、分页、RSS 超时)
- 测试覆盖提升

</details>

## Core Value

让 Auto-RSS 成为一个安全、稳定、可靠的自动化下载系统，具备良好的可维护性和扩展性。

---

## Requirements

### Validated (v1.1)

- ✓ 基础 RSS 订阅和下载功能 — 现有
- ✓ qBittorrent 集成 — 现有
- ✓ Bangumi 元数据获取 — 现有
- ✓ Web UI 订阅管理 — 现有
- ✓ 文件自动整理 — 现有
- ✓ BUG-01~BUG-05: 关键 Bug 修复 — v1.0
- ✓ SEC-01~SEC-02: 安全防护 — v1.0
- ✓ REF-01~REF-10: 代码重构 — v1.0
- ✓ PERF-01~PERF-03: 性能优化 — v1.0
- ✓ TEST-01~TEST-06: 测试覆盖 — v1.0
- ✓ SEC-03: JWT 认证系统 — v1.1
- ✓ SEC-04: API 限流保护 — v1.1
- ✓ INF-02: WebSocket 自动重连机制 — v1.1

### Active (Next Milestone)

- [ ] INF-01: Plex/Jellyfin 完整集成实现
- [ ] INF-03: 任务队列支持（多并发任务）
- [ ] INF-04: 数据库迁移至 PostgreSQL
- [ ] FEAT-01: 更多下载器支持 (Transmission, Aria2)
- [ ] FEAT-02: 通知渠道扩展 (Telegram, Discord)
- [ ] FEAT-03: 高级过滤规则引擎

### Out of Scope

| Feature | Reason |
|---------|--------|
| UI 界面改版 | 专注功能开发 |
| 多用户支持 | 架构变动过大，需单独规划 |
| 容器编排优化 | 运维层面，非代码债务 |

---

## Context

### v1.1 基础设施增强成果

1. **认证系统**：JWT access/refresh token 双令牌机制，支持登录/刷新/登出
2. **API 限流**：Token bucket 算法，IP-based 限流，防止 API 滥用
3. **WebSocket 增强**：自动重连、指数退避、消息缓冲、连接状态管理
4. **配置扩展**：环境变量支持所有限流参数，灵活可调

### v1.0 技术债务清理成果 (Archived)

1. **功能缺陷修复**：所有关键 TODO 已实现（磁盘暂停、重试机制等）
2. **代码结构优化**：单个文件从 2000+ 行降至 < 800 行
3. **安全隐患消除**：路径遍历风险已防护、SQL 注入已审计
4. **性能问题解决**：N+1 查询已修复、同步阻塞已优化
5. **测试覆盖提升**：Handler 层、Service 集成、文件操作、并发测试已完善

### 技术栈

- Go + Gin 框架
- SQLite 数据库
- Vue.js 前端
- qBittorrent 集成
- Bangumi API 集成

---

## Constraints

- **技术栈**：保持现有 Go + Vue 栈，不引入新技术
- **兼容性**：重构不能破坏现有 API 和数据库结构
- **质量**：新代码需有单元测试覆盖

---

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| JWT via query param for WebSocket | Header unavailable during upgrade | ✓ 成功 — Working well |
| Exponential backoff with ±50% jitter | Avoid thundering herd on server restart | ✓ 成功 — 有效分散重连请求 |
| IP-based rate limiting | Simple and effective | ✓ 成功 — Per-IP isolation |
| LRU eviction at 10k entries | Memory bound for safety | ✓ 成功 — Safe memory usage |
| Token bucket algorithm | Industry standard, well-tested library | ✓ 成功 — golang.org/x/time/rate |
| 分阶段清理 | 降低风险，每阶段可验证 | ✓ 成功 — 3 阶段顺利完成 |
| 先修复后重构 | 避免在不稳定基础上改动 | ✓ 成功 — Bug 修复为基础 |
| 保持向后兼容 | 不影响现有用户使用 | ✓ 成功 — API 行为不变 |

---

## Next Milestone Goals

**v1.2 功能扩展**

1. Plex/Jellyfin 完整集成实现（INF-01）
2. 任务队列支持多并发任务（INF-03）
3. 支持更多下载器（Transmission、Aria2）
4. 通知渠道扩展（Telegram、Discord）
5. 高级过滤规则引擎

**v2.0 架构升级（Future）**

1. 数据库迁移至 PostgreSQL（INF-04）
2. 多用户支持
3. 插件系统架构

---

*Last updated: 2026-04-06 after v1.1 milestone completion*
