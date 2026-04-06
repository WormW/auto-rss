# Phase 6: WebSocket Auto-Reconnection - Context

**Gathered:** 2026-04-06
**Status:** Ready for planning

<domain>
## Phase Boundary

实现 WebSocket 自动重连机制，保证实时通知的可靠性。WebSocket 断开后客户端使用指数退避策略自动重连，断线期间消息缓冲并在重连后发送。

**注意**：本阶段主要是前端实现，后端 WebSocket 基础设施已存在。

</domain>

<decisions>
## Implementation Decisions

### Connection Scope
- **D-01:** 全局连接，在 `App.vue` 中初始化 WebSocket 客户端
- **D-02:** 所有页面都能接收实时通知（下载完成、错误等toast提示）
- **D-03:** 连接状态全局共享（使用 Pinia store）

### Authentication Approach
- **D-04:** 使用 URL 查询参数传递 JWT token: `ws://host/ws?token=<jwt>`
- **D-05:** 后端 WebSocket 升级处理函数解析 token 并验证
- **D-06:** Token 过期时后端返回 401，前端触发重新登录

### Reconnection Strategy
- **D-07:** 指数退避重连：延迟 = min(1s × 2^attempt, 30s)
- **D-08:** 随机抖动 ±50%，防止惊群效应
- **D-09:** 最大重试次数 10 次，之后停止重连并显示"连接已断开"状态
- **D-10:** 用户可点击按钮手动触发重连

### Disconnection Handling
- **D-11:** 正常关闭（code 1000）和用户登出不触发重连
- **D-12:** 异常关闭（网络错误、code 1006等）触发重连逻辑
- **D-13:** 页面可见性变化（visibilitychange）时检测连接状态，断开则立即重连

### Message Buffering
- **D-14:** 仅客户端缓冲，断线期间消息保存在内存中
- **D-15:** 最多缓冲 100 条消息，超过时丢弃最旧的消息
- **D-16:** 消息 TTL 5 分钟，超时消息丢弃
- **D-17:** 重连成功后按序发送缓冲的消息

### Claude's Discretion
- 连接状态 UI 展示样式（标签颜色、提示文案）
- WebSocket 心跳间隔具体数值
- 重连按钮的位置和样式
- 消息缓冲的具体数据结构实现

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### WebSocket Requirements
- `.planning/REQUIREMENTS.md` § WebSocket Reconnection (WS-01~04) — 需求规格

### Existing WebSocket Code
- `internal/service/notification/websocket.go` — 后端 WebSocket hub 实现
- `internal/api/handler/notification.go` — WebSocket handler 和状态接口
- `web/src/views/Notifications.vue` — 前端通知页面（现有 WebSocket 状态显示）

### Frontend Patterns
- `web/src/main.ts` — Vue 应用入口，Pinia store 初始化
- `web/src/App.vue` — 根组件，适合全局 WebSocket 初始化
- `web/src/api/index.ts` — API 客户端模式参考

### Authentication (from Phase 4)
- `internal/api/middleware/auth.go` — JWT 验证中间件，参考 token 解析逻辑
- 路由 `/ws` 需要添加 token 验证（可能需要在 upgrader 中处理）

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets (Frontend)
- **API 客户端**: `web/src/api/index.ts` 中的 axios 实例模式，可用于创建 WebSocket 服务
- **状态管理**: Pinia 已配置 (`createPinia()`)，适合存储 WebSocket 连接状态和消息缓冲
- **通知 API**: `notificationApi.getWebSocketStatus()` 已存在，可用于初始状态检测

### Backend Assets
- **WebSocket Hub**: `websocket.go` 已有完整实现：
  - Client 结构体（send channel、conn、hub 引用）
  - Hub 管理（register/unregister/broadcast channels）
  - readPump/writePump goroutines
  - ping/pong 心跳机制（60s 间隔）
- **升级端点**: `GET /ws` 已注册（需在 `router.go` 确认）

### Integration Points
1. **Frontend entry**: `main.ts` 或 `App.vue` 中初始化 WebSocket 服务
2. **Backend auth**: `websocket.go` 的 `upgrader.CheckOrigin` 或升级处理中添加 JWT 验证
3. **Global state**: 创建 `web/src/stores/websocket.ts` Pinia store
4. **Toast notifications**: 在 `App.vue` 或全局组件中监听 WebSocket 消息并显示

### Required Changes
- **Backend**: `HandleWebSocket` 函数需要修改以解析 `token` 查询参数并验证 JWT
- **Frontend**: 新建 WebSocket 客户端服务，包含重连逻辑和消息缓冲

</code_context>

<specifics>
## Specific Ideas

- 参考现有的 `notificationApi.getWebSocketStatus()` 接口获取初始连接数
- WebSocket 消息格式应与现有的 `model.NotificationPayload` 一致
- Toast 通知使用 naive-ui 的 `useNotification()` 或 `useMessage()`
- 连接断开后在页面顶部显示警告条（类似 GitHub 的离线提示）

</specifics>

<deferred>
## Deferred Ideas

- 服务端消息重放（需要服务器端持久化，当前仅客户端缓冲）— WS-05，v1.2+ 规划
- 多标签页同步（BroadcastChannel API）— 可选增强
- WebSocket 压缩（permessage-deflate）— 性能优化，当前不需要

</deferred>

---

*Phase: 06-websocket-auto-reconnection*
*Context gathered: 2026-04-06*
