---
phase: 06-websocket-auto-reconnection
plan: 03
subsystem: frontend
phase_name: WebSocket自动重连机制
plan_name: Vue应用集成
tags: [websocket, vue, integration, ui]
dependencies:
  requires:
    - 06-02
  provides:
    - global-websocket-ui
    - real-time-notifications
tech-stack:
  added: []
  patterns:
    - Pinia store integration in Vue components
    - Lifecycle-based service initialization
    - Reactive status indicators
key-files:
  created: []
  modified:
    - web/src/App.vue
decisions: []
metrics:
  duration: "15 minutes"
  completed_date: "2026-04-06"
  tasks_completed: 2
  files_modified: 1
---

# Phase 06 Plan 03: Vue应用集成 - 执行总结

## 概述
将WebSocket服务集成到Vue应用中，在全局App.vue组件中添加连接状态指示器和手动重连按钮，并在应用挂载时自动初始化WebSocket连接。

## 执行的任务

### Task 1: 添加连接状态指示器和重连按钮
- **文件**: `web/src/App.vue`
- **变更**:
  - 导入WebSocket store和服务
  - 导入WiFi图标（WifiOutline, WifiOffOutline, RefreshOutline）
  - 在桌面端头部添加连接状态标签（n-tag）
    - 连接中: 黄色警告样式，旋转刷新图标
    - 已断开/错误: 红色错误样式，WiFi关闭图标
    - 已连接: 不显示（保持界面简洁）
  - 添加手动重连按钮（当可以重连时显示）
  - 添加工具提示显示重连尝试次数和下次重试延迟
  - 添加旋转动画CSS用于连接中状态

### Task 2: 初始化WebSocket服务
- **文件**: `web/src/App.vue`
- **变更**:
  - 添加`initWebSocket()`函数从localStorage获取token并初始化连接
  - 在`onMounted`生命周期钩子中调用`initWebSocket()`
  - 在`onUnmounted`中清理WebSocket连接
  - 在`handleUserSelect`中处理登出时断开WebSocket
  - 添加`watch`监听器检测token过期错误

## 关键实现细节

### 状态指示器逻辑
```typescript
const connectionTagType = computed(() => {
  switch (wsStore.status) {
    case 'connected': return 'success'
    case 'connecting': return 'warning'
    case 'disconnected':
    case 'error':
    case 'max_retries_exceeded': return 'error'
    default: return 'default'
  }
})
```

### WebSocket初始化
```typescript
const initWebSocket = () => {
  const storedToken = localStorage.getItem('access_token')
  if (!storedToken) {
    console.log('[App] No token available, WebSocket not initialized')
    return
  }
  token.value = storedToken
  wsService.value = createWebSocketService(wsStore)
  wsService.value.connect(token.value)
}
```

### 清理处理
```typescript
onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
  if (wsService.value) {
    wsService.value.disconnect()
    wsService.value = null
  }
})
```

## 偏差记录

无偏差 - 计划按预期执行。

## 验证结果

- [x] TypeScript编译无错误
- [x] 所有验收标准通过:
  - `useWebSocketStore` 导入和使用
  - `createWebSocketService` 导入和使用
  - WiFi图标导入和使用
  - `handleReconnect` 函数实现
  - `store.status` 和 `store.canReconnect` 使用
  - `localStorage.getItem('access_token')` 使用
  - `initWebSocket` 函数和调用
  - `wsService.value?.disconnect()` 使用
  - `onMounted` 中调用 `initWebSocket`

## 提交记录

| 提交 | 消息 |
|------|------|
| d286d48 | feat(06-03): add WebSocket connection status indicator and reconnect button to App.vue |
| 87fb5b4 | feat(06-03): initialize WebSocket service on app mount with cleanup |

## 后续工作

- 当登录页面实现后，取消注释登录跳转逻辑
- 考虑在移动端头部也添加连接状态指示器
- 可添加声音通知选项用于重要消息

## 威胁标志

无新增威胁表面。所有安全考虑已在06-01和06-02中处理:
- Token从localStorage获取（标准SPA实践）
- 未在日志中记录敏感token值
- 使用计算属性避免UI重渲染循环（缓解T-06-08）
