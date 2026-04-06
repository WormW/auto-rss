# Phase 1: Bug 修复与安全 - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2025-04-05
**Phase:** 01-bug
**Areas discussed:** BUG-01, BUG-02, BUG-03, BUG-04, BUG-05, SEC-01, SEC-02

---

## BUG-01 重试逻辑

| 选项 | 描述 | 选择 |
|------|------|------|
| 完整重试流程 | 删除旧种子 → 重置计数 → 重新添加任务 | ✓ |
| 简化重试 | 只重置状态为 pending，等定时任务处理 | |
| 智能重试 | 检查状态后决定处理方式 | |

**决策：** 完整重试流程 — 用户主动点击"重试"时期望立即重新下载

---

## BUG-02 日历下载状态

| 选项 | 描述 | 选择 |
|------|------|------|
| 严格判断 | 只算状态为 `completed` 的记录 | ✓ |
| 宽松判断 | 包括 `completed` 和 `downloading` | |
| 分层显示 | 区分两种状态，UI 不同显示 | |

**决策：** 严格判断 — 只算真正完成下载的，避免误导

---

## BUG-03 磁盘暂停

| 选项 | 描述 | 选择 |
|------|------|------|
| 全局原子变量 | `atomic.Bool`，scheduler 检查 | ✓ |
| Channel 通知 | scheduler 监听 pause/resume channel | |
| 数据库配置 | config 表存储 `download_paused` | |

**决策：** 全局原子变量 — 简单高效，无性能开销

---

## BUG-04 竞态条件

| 选项 | 描述 | 选择 |
|------|------|------|
| nil 检查 + 原子操作 | `atomic.Value` 存储 cancelFunc | |
| 重排锁顺序 | 锁内检查 currentTask 状态 | ✓ |
| 双向验证 | 检查 currentTask 和 cancelFunc | |

**决策：** 重排锁顺序 — 保持现有锁机制

---

## BUG-05 文件事务

| 选项 | 描述 | 选择 |
|------|------|------|
| 先数据库，后文件 | 状态机模式，崩溃可检测 | ✓ |
| 两阶段提交 | 标记意图 → 执行 → 确认 | |
| 补偿机制 | 失败后尝试反向恢复 | |

**决策：** 先数据库后文件 — 最实用的做法

---

## SEC-01 路径遍历

| 选项 | 描述 | 选择 |
|------|------|------|
| 规范化 + 前缀检查 | `Clean()` 后验证前缀 | ✓ |
| 禁止 `..` 序列 | 直接拒绝包含 `..` 的路径 | |
| chroot 风格 | 限制在基础目录内 | |

**决策：** 规范化 + 前缀检查 — 安全且允许合理子目录

---

## SEC-02 SQL 注入

| 选项 | 描述 | 选择 |
|------|------|------|
| 全面审计 | 检查所有 repository 文件 | |
| 重点审计 | 只检查动态条件查询 | ✓ |
| 信任 GORM | 假设 GORM 正确使用 | |

**决策：** 重点审计动态条件查询 — 风险最高的部分

---

## Claude's Discretion

None — all decisions locked

---

*Discussion completed: 2025-04-05*
