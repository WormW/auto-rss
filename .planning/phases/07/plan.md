# Phase 7 Plan: API层功能增强

## Goal

为 Auto-RSS 添加标签系统、下载历史/统计、RSS健康检查的完整API层，使前端能够使用这些功能。

## Context

### 当前状态

Auto-RSS 后端已具备以下基础设施：
1. **标签系统模型和仓储层** - `SubscriptionTag`, `SubscriptionTagRelation` 模型和完整的 Repository 方法
2. **下载历史和统计仓储** - `GetDownloadHistory`, `GetDownloadStatistics` 方法
3. **RSS健康检查服务** - `RSSHealthChecker` 服务实现

### 目标状态

暴露以下 REST API 端点：
- 标签 CRUD 和订阅关联管理
- 下载历史查询和统计分析
- RSS 健康状态检查和报告

## Success Criteria

- [x] 标签系统 API 完整可用（创建/更新/删除/列表/订阅关联）
- [x] 下载历史 API 支持筛选和分页
- [x] 下载统计 API 返回各状态数量和趋势数据
- [x] RSS 健康检查 API 可检查单个/全部订阅
- [x] 新增 handler 已有对应的 handler 级测试覆盖
- [ ] API 文档（通过代码注释）完整

## Plans

### Plan 07-01: 标签系统 Handler 和路由

**Files to modify/create:**
- `internal/api/handler/tag.go` (create)
- `internal/api/router/router.go` (modify)

**Requirements:**
- 实现 TagHandler 结构体
- 实现标签 CRUD 方法：List, Create, Update, Delete
- 实现订阅标签关联：GetSubscriptionTags, AddTagsToSubscription, RemoveTagsFromSubscription
- 在 router 中注册标签相关路由
- 添加必要的请求/响应结构体

**Validation:**
- Handler 能正确处理请求
- 路由注册正确
- 错误处理完善

---

### Plan 07-02: 下载历史和统计 Handler 和路由

**Files to modify/create:**
- `internal/api/handler/download_history.go` (create)
- `internal/api/handler/download.go` (modify - 添加历史/统计端点)
- `internal/api/router/router.go` (modify)

**Requirements:**
- 实现下载历史查询 API（支持订阅ID、状态、日期筛选）
- 实现下载统计 API（总览、每日统计、订阅排行）
- 添加时间线/趋势数据 API
- 正确解析查询参数

**Validation:**
- 历史查询支持所有筛选条件
- 统计数据准确
- 分页正常工作

---

### Plan 07-03: RSS 健康检查 Handler 和路由

**Files to modify/create:**
- `internal/api/handler/rss_health.go` (create)
- `internal/api/router/router.go` (modify)

**Requirements:**
- 创建 RSSHealthHandler，复用现有 RSSHealthChecker 服务
- 实现检查单个订阅健康状态
- 实现检查所有订阅健康状态
- 实现获取失效订阅列表
- 实现触发健康检查任务（异步）

**Validation:**
- 健康检查返回正确状态
- 异步任务正确触发
- 错误处理完善

---

### Plan 07-04: 标签系统 Handler 测试

**Files to modify/create:**
- `internal/api/handler/tag_test.go` (create)

**Requirements:**
- 测试标签 CRUD 所有端点
- 测试订阅标签关联端点
- 测试错误场景（无效ID、重复名称等）
- 使用 mock repository

**Validation:**
- 测试覆盖率 > 80%
- 所有测试通过

---

### Plan 07-05: 下载历史和统计 Handler 测试

**Files to modify/create:**
- `internal/api/handler/download_history_test.go` (create)

**Requirements:**
- 测试下载历史查询端点
- 测试下载统计端点
- 测试筛选参数解析
- 测试分页功能

**Validation:**
- 测试覆盖率 > 80%
- 所有测试通过

---

### Plan 07-06: RSS 健康检查 Handler 测试

**Files to modify/create:**
- `internal/api/handler/rss_health_test.go` (create)

**Requirements:**
- 测试单个订阅健康检查
- 测试批量健康检查
- 测试获取失效订阅
- 测试异步任务触发

**Validation:**
- 测试覆盖率 > 80%
- 所有测试通过

---

### Plan 07-07: 集成测试和 API 验证

**Files to modify/create:**
- `test/api/integration_test.go` (modify/add cases)

**Requirements:**
- 端到端测试新增 API
- 验证路由正确注册
- 验证响应格式统一

**Validation:**
- 所有集成测试通过
- API 文档与实现一致

## Plan Checklist

- [x] Plan 07-01: 标签系统 Handler 和路由 — completed 2026-04-12
- [x] Plan 07-02: 下载历史和统计 Handler 和路由 — completed 2026-04-12
- [x] Plan 07-03: RSS 健康检查 Handler 和路由 — completed 2026-04-12
- [x] Plan 07-04: 标签系统 Handler 测试 — published 2026-06-22 via WOR-139 / PR #38
- [x] Plan 07-05: 下载历史和统计 Handler 测试 — published 2026-06-22 via WOR-140 / PR #39
- [x] Plan 07-06: RSS 健康检查 Handler 测试 — published 2026-06-22 via WOR-141 / PR #40
- [ ] Plan 07-07: 路由级集成测试和 API 验证 — separate follow-up, not part of completed handler test work
