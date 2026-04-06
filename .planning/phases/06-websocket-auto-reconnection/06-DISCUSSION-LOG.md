# Phase 6: WebSocket Auto-Reconnection - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-06
**Phase:** 06-websocket-auto-reconnection
**Areas discussed:** Connection scope, Authentication, Reconnection strategy, Message buffering

---

## Connection Scope

| Option | Description | Selected |
|--------|-------------|----------|
| Global (all pages) | 在 App.vue 初始化，所有页面接收实时通知 | ✓ |
| Page-specific | 仅在通知页面连接 | |

**User's choice:** "本身设计不太重要，你按照推荐即可"
**Selected:** 全局连接，更好的用户体验

---

## Authentication Approach

| Option | Description | Selected |
|--------|-------------|----------|
| Query parameter | `ws://host/ws?token=xxx`，实现最简单 | ✓ |
| Subprotocol | 通过 Sec-WebSocket-Protocol 头传递 | |
| First message | 连接后第一条消息发送 token | |

**User's choice:** 按推荐方案（查询参数）
**Notes:** 简单实现优先，token 出现在日志在可控范围内

---

## Reconnection Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Exponential backoff + limit | 指数退避，最大10次后停止 | ✓ |
| Infinite retry | 一直重连（用户明确要求不要无限重试）| |
| Manual only | 断开后显示按钮，用户手动重连 | |

**User's choice:** "注意不要无限重试即可"
**Selected:** 最大10次重试，之后停止并显示手动重连按钮

---

## Message Buffering

| Option | Description | Selected |
|--------|-------------|----------|
| Client-side only | 仅在浏览器内存缓冲，标签页关闭丢消息 | ✓ |
| Server-side | 后端 hub 缓冲，需要改 Go 代码 | |

**User's choice:** 按推荐方案
**Selected:** 仅客户端缓冲，实现简单，符合 WS-03 需求

---

## Claude's Discretion

- 连接状态 UI 样式（标签颜色、提示文案）
- 心跳间隔具体数值
- 重连按钮位置
- 消息缓冲数据结构

## Deferred Ideas

- 服务端消息重放（WS-05）— v1.2+ 规划
- 多标签页同步 — 可选增强
