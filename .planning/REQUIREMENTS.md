# Requirements: Auto-RSS 技术债务清理

**Defined:** 2025-04-05
**Core Value:** 修复关键 Bug 和安全问题，提升代码可维护性

## v1 Requirements

### 阶段 1：Bug 修复与安全（Phase 1）

**核心目标：修复已知 Bug，消除安全隐患，确保系统稳定运行**

#### BUG-01 下载重试逻辑修复
- [ ] **BUG-01**: 修复 Retry 接口只改状态未真正重试的问题
  - 当前：`Retry` handler 仅将状态改为 pending
  - 期望：调用 scheduler 重新添加下载任务
  - 文件：`internal/api/handler/download.go:146`

#### BUG-02 日历下载状态修复
- [ ] **BUG-02**: 修复 IsDownloaded 字段硬编码为 false 的问题
  - 当前：`calendar.go:123` 总是返回 false
  - 期望：查询 download repository 判断真实状态
  - 文件：`internal/service/calendar/calendar.go:123`

#### BUG-03 磁盘监控暂停功能实现
- [ ] **BUG-03**: 实现 pauseDownloads() 和 resumeDownloads()
  - 当前：空实现，只发送通知
  - 期望：设置全局标志阻止 scheduler 添加新下载
  - 文件：`internal/service/disk/monitor.go:520-527`

#### BUG-04 Task Manager 竞态条件修复
- [ ] **BUG-04**: 修复 cancelFunc 在任务完成后被调用的竞态条件
  - 当前：`manager.go:115-160` 可能在任务完成后调用 cancel
  - 期望：添加 nil 检查或同步机制
  - 文件：`internal/service/task/manager.go`

#### BUG-05 文件移动事务保护
- [ ] **BUG-05**: 将文件移动和数据库更新纳入同一事务
  - 当前：`organizer.go:536-567` 先移动文件再更新数据库
  - 期望：使用数据库事务，失败时回滚文件操作
  - 文件：`internal/service/organizer/organizer.go`

#### SEC-01 路径遍历防护
- [ ] **SEC-01**: 防止路径遍历攻击
  - 当前：用户输入构造路径未充分校验
  - 期望：验证路径在允许目录内，阻止 `../` 序列
  - 文件：`subscription.go`, `organizer.go`

#### SEC-02 SQL 注入审计
- [ ] **SEC-02**: 审计 repository 层 Where 子句
  - 当前：使用 GORM，但需确认所有用户输入都参数化
  - 期望：确保无动态 SQL 拼接
  - 文件：`internal/repository/*.go`

---

### 阶段 2：代码重构（Phase 2）

**核心目标：拆分臃肿代码，提升可维护性和测试性**

#### REF-01 订阅处理器拆分
- [ ] **REF-01**: 提取 Bangumi 富化服务
  - 从 `subscription.go` 提取 Bangumi 元数据获取逻辑
  - 创建 `internal/service/bangumi/enrich.go`

- [ ] **REF-02**: 提取重命名服务
  - 从 `subscription.go` 提取文件重命名逻辑
  - 创建 `internal/service/renamer/service.go`

- [ ] **REF-03**: 提取批量导入服务
  - 从 `subscription.go` 提取批量导入逻辑
  - 创建 `internal/service/subscription/batch.go`

- [ ] **REF-04**: 提取集合下载服务
  - 从 `subscription.go` 提取批量创建下载任务逻辑
  - 创建 `internal/service/subscription/collection.go`

#### REF-02 下载监控器拆分
- [ ] **REF-05**: 提取状态更新组件
  - 从 `monitor.go` 提取 qBittorrent 状态同步逻辑
  - 创建 `internal/service/downloader/status_sync.go`

- [ ] **REF-06**: 提取重命名触发器
  - 从 `monitor.go` 提取完成后的重命名触发逻辑
  - 创建 `internal/service/downloader/completion_handler.go`

- [ ] **REF-07**: 提取重试服务
  - 从 `monitor.go` 提取自动重试逻辑
  - 创建 `internal/service/downloader/retry_service.go`

#### REF-03 文件整理器拆分
- [ ] **REF-08**: 提取解析器组件
  - 从 `organizer.go` 提取文件名解析逻辑
  - 创建 `internal/service/organizer/parser.go`

- [ ] **REF-09**: 提取匹配器组件
  - 从 `organizer.go` 提取剧集匹配逻辑
  - 创建 `internal/service/organizer/matcher.go`

- [ ] **REF-10**: 提取移动器组件
  - 从 `organizer.go` 提取文件移动逻辑
  - 创建 `internal/service/organizer/mover.go`

---

### 阶段 3：性能与测试（Phase 3）

**核心目标：修复性能瓶颈，补充测试覆盖**

#### PERF-01 N+1 查询修复
- [ ] **PERF-01**: 修复订阅列表的下载计数 N+1 问题
  - 当前：`subscription.go:1041-1063` 循环查询
  - 期望：使用 JOIN 或预加载一次性查询
  - 文件：`internal/api/handler/subscription.go`

#### PERF-02 分页限制
- [ ] **PERF-02**: 为 List 查询添加上限
  - 当前：`repository/download.go` 无最大页数限制
  - 期望：限制单次查询最大记录数（如 1000）

#### PERF-03 可配置 RSS 超时
- [ ] **PERF-03**: RSS 解析超时改为可配置
  - 当前：`rss/parser.go:97` 硬编码 30 秒
  - 期望：从配置读取，支持按源配置

#### TEST-01 Handler 层测试
- [ ] **TEST-01**: 为 download handler 添加测试
  - 覆盖：List, Retry, Delete 接口
  - 使用 mock repository

- [ ] **TEST-02**: 为 subscription handler 添加测试（重构后）
  - 覆盖：CRUD, 批量操作接口

#### TEST-02 Service 集成测试
- [ ] **TEST-03**: 为 bangumi 服务添加测试
  - 使用 httpmock 模拟 Bangumi API

- [ ] **TEST-04**: 为 mikan 服务添加测试
  - 使用 httpmock 模拟 Mikan 页面

#### TEST-03 文件操作测试
- [ ] **TEST-05**: 为 organizer 添加测试
  - 使用临时目录测试文件移动
  - 测试解析和匹配逻辑

#### TEST-04 并发测试
- [ ] **TEST-06**: 为 task manager 添加并发测试
  - 测试竞态条件、取消机制

---

## v2 Requirements（后续里程碑）

### 基础设施改进
- **INF-01**: Plex/Jellyfin 完整集成实现
- **INF-02**: WebSocket 自动重连机制
- **INF-03**: 任务队列支持（多并发任务）
- **INF-04**: 数据库迁移至 PostgreSQL

### 安全增强
- **SEC-03**: 添加 JWT 认证系统
- **SEC-04**: API 限流保护

---

## Out of Scope

| Feature | Reason |
|---------|--------|
| 新功能开发（如通知系统增强） | 专注技术债务清理 |
| UI 界面改版 | 超出当前范围 |
| 多用户支持 | 架构变动过大 |
| SQLite → PostgreSQL 迁移 | 作为独立里程碑处理 |
| 容器编排优化 | 运维层面，非代码债务 |

---

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| BUG-01 | Phase 1 | Pending |
| BUG-02 | Phase 1 | Pending |
| BUG-03 | Phase 1 | Pending |
| BUG-04 | Phase 1 | Pending |
| BUG-05 | Phase 1 | Pending |
| SEC-01 | Phase 1 | Pending |
| SEC-02 | Phase 1 | Pending |
| REF-01~REF-04 | Phase 2 | Pending |
| REF-05~REF-07 | Phase 2 | Pending |
| REF-08~REF-10 | Phase 2 | Pending |
| PERF-01~PERF-03 | Phase 3 | Pending |
| TEST-01~TEST-06 | Phase 3 | Pending |

**Coverage:**
- v1 requirements: 22 total
- Mapped to phases: 22
- Unmapped: 0 ✓

---
*Requirements defined: 2025-04-05*
