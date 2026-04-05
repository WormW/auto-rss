# Auto-RSS

## Current State

**Shipped:** v1.0 技术债务清理 (2026-04-05)

Auto-RSS 是一个自动化动漫下载系统，已完成技术债务清理，系统稳定可靠运行。

**Stats:**
- 代码库：~25,585 行 Go 代码
- 测试覆盖：Handler 层、Service 层、文件操作、并发安全
- 架构：服务化设计，10个独立服务组件

---

## What This Is

对 Auto-RSS 项目进行系统性的技术债务清理后的稳定版本，修复了已知 Bug、安全漏洞，重构了臃肿代码，为后续功能开发奠定稳固基础。

## Core Value

修复关键 Bug 和安全问题，让系统稳定可靠运行，同时提升代码可维护性。

---

## Requirements

### Validated (v1.0)

- ✓ 基础 RSS 订阅和下载功能 — 现有
- ✓ qBittorrent 集成 — 现有
- ✓ Bangumi 元数据获取 — 现有
- ✓ Web UI 订阅管理 — 现有
- ✓ 文件自动整理 — 现有
- ✓ BUG-01: 下载重试逻辑修复 — v1.0
- ✓ BUG-02: 日历下载状态修复 — v1.0
- ✓ BUG-03: 磁盘监控暂停功能 — v1.0
- ✓ BUG-04: Task Manager 竞态条件修复 — v1.0
- ✓ BUG-05: 文件移动事务保护 — v1.0
- ✓ SEC-01: 路径遍历防护 — v1.0
- ✓ SEC-02: SQL 注入审计 — v1.0
- ✓ REF-01~REF-10: 代码重构，提取 10 个服务 — v1.0
- ✓ PERF-01~PERF-03: 性能优化（N+1、分页、RSS超时）— v1.0
- ✓ TEST-01~TEST-06: 测试覆盖提升 — v1.0

### Active (Next Milestone)

- [ ] INF-01: Plex/Jellyfin 完整集成实现
- [ ] INF-02: WebSocket 自动重连机制
- [ ] INF-03: 任务队列支持（多并发任务）
- [ ] INF-04: 数据库迁移至 PostgreSQL
- [ ] SEC-03: 添加 JWT 认证系统
- [ ] SEC-04: API 限流保护

### Out of Scope

| Feature | Reason |
|---------|--------|
| UI 界面改版 | 专注功能开发 |
| 多用户支持 | 架构变动过大，需单独规划 |
| 容器编排优化 | 运维层面，非代码债务 |

---

## Context

### v1.0 技术债务清理成果

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
| 分阶段清理 | 降低风险，每阶段可验证 | ✓ 成功 — 3 阶段顺利完成 |
| 先修复后重构 | 避免在不稳定基础上改动 | ✓ 成功 — Bug 修复为基础 |
| 保持向后兼容 | 不影响现有用户使用 | ✓ 成功 — API 行为不变 |
| StatusSync interface | Per D-04 interface design pattern | ✓ 成功 — 测试性提升 |
| CompletionHandler interface | Per D-04 interface design pattern | ✓ 成功 — 可测试性提升 |
| Nil DB handling | Enable testing without real database | ✓ 成功 — 测试覆盖率提升 |

---

## Next Milestone Goals

**v1.1 基础设施增强**

1. 添加 JWT 认证系统（SEC-03）
2. 实现 API 限流保护（SEC-04）
3. 添加 WebSocket 自动重连机制（INF-02）
4. 优化任务队列支持（INF-03）

---

*Last updated: 2026-04-05 after v1.0 milestone completion*
