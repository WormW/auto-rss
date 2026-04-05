# Milestones

## v1.0 技术债务清理 (Shipped: 2026-04-05)

**Phases completed:** 3 phases, 17 plans, ~534 tasks

**Key accomplishments:**

1. **修复了 5 个关键 Bug 和 2 个安全问题**
   - 下载重试逻辑：真正实现重试功能（调用 scheduler 重新添加任务）
   - 日历下载状态：修复硬编码 false，查询真实下载状态
   - 磁盘监控暂停：实现全局原子暂停标志，自动保护磁盘空间
   - Task Manager 竞态条件：修复 cancelFunc 在任务完成后被调用的问题
   - 文件事务保护：使用数据库状态机保护文件移动操作
   - 路径遍历防护：验证路径在允许目录内，阻止 `../` 序列
   - SQL 注入审计：确认所有用户输入都参数化

2. **重构了 3 个臃肿模块**
   - 订阅处理器：从 2345 行降至 < 800 行
   - 下载监控器：从 959 行降至 < 500 行
   - 文件整理器：从 683 行降至 < 400 行
   - 提取出 10 个独立服务：Bangumi Enricher、Renamer、Batch Import、Collection Download、Status Sync、Completion Handler、Retry Service、Parser、Matcher、Mover

3. **修复了性能瓶颈**
   - N+1 查询：使用 JOIN + COUNT 一次性查询订阅列表和下载计数
   - 分页限制：所有 List 接口添加最大页数保护（1000条）
   - RSS 超时：支持按源配置超时时间

4. **大幅提升测试覆盖**
   - DownloadHandler 和 SubscriptionHandler 单元测试
   - Bangumi 和 Mikan 服务集成测试
   - Organizer 文件操作测试
   - Task Manager 并发安全测试

5. **建立了服务化架构**
   - 通过接口设计模式实现高内聚低耦合
   - 所有新服务都有独立单元测试
   - 100% 向后兼容（API 行为不变）

**Stats:**
- Files modified: 31
- Lines changed: +3,642 / -77
- Total Go LOC: ~25,585

---
