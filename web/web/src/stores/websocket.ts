import { defineStore } from 'pinia'
import { useMessage } from 'naive-ui'

// Connection status type
export type ConnectionStatus =
  | 'connecting'
  | 'connected'
  | 'disconnected'
  | 'error'
  | 'max_retries_exceeded'

// Notification payload interface (from model.NotificationPayload)
export interface NotificationPayload {
  type: string
  title: string
  message: string
  data?: any
  timestamp: number
}

// WebSocket state interface
interface WebSocketState {
  status: ConnectionStatus
  lastError: string | null
  reconnectAttempt: number
  nextReconnectDelay: number
  bufferedMessageCount: number
}

export const useWebSocketStore = defineStore('websocket', {
  state: (): WebSocketState => ({
    status: 'disconnected',
    lastError: null,
    reconnectAttempt: 0,
    nextReconnectDelay: 0,
    bufferedMessageCount: 0
  }),

  getters: {
    /**
     * Check if WebSocket is connected
     */
    isConnected(): boolean {
      return this.status === 'connected'
    },

    /**
     * Check if reconnection is possible
     */
    canReconnect(): boolean {
      return this.status !== 'connecting' && this.status !== 'connected'
    },

    /**
     * Human-readable status text in Chinese
     */
    statusText(): string {
      switch (this.status) {
        case 'connecting':
          return '连接中...'
        case 'connected':
          return '已连接'
        case 'disconnected':
          return '已断开'
        case 'error':
          return '连接错误'
        case 'max_retries_exceeded':
          return '连接失败，请手动重试'
        default:
          return '未知状态'
      }
    }
  },

  actions: {
    /**
     * Set connection status
     */
    setStatus(status: ConnectionStatus, error?: string): void {
      this.status = status
      if (error !== undefined) {
        this.lastError = error
      }
    },

    /**
     * Set current reconnect attempt count
     */
    setReconnectAttempt(attempt: number): void {
      this.reconnectAttempt = attempt
    },

    /**
     * Set next reconnect delay in milliseconds
     */
    setNextReconnectDelay(delay: number): void {
      this.nextReconnectDelay = delay
    },

    /**
     * Set buffered message count
     */
    setBufferedMessageCount(count: number): void {
      this.bufferedMessageCount = count
    },

    /**
     * Handle incoming notification message
     */
    onMessageReceived(payload: NotificationPayload): void {
      // Use naive-ui's useMessage for toasts
      const message = useMessage()

      switch (payload.type) {
        case 'download_complete':
          message.success(`${payload.title}: ${payload.message}`, {
            duration: 5000,
            closable: true
          })
          break

        case 'download_failed':
          message.error(`${payload.title}: ${payload.message}`, {
            duration: 8000,
            closable: true
          })
          break

        case 'disk_warning':
          message.warning(`${payload.title}: ${payload.message}`, {
            duration: 10000,
            closable: true
          })
          break

        case 'disk_critical':
          message.error(`${payload.title}: ${payload.message}`, {
            duration: 0, // Persistent until closed
            closable: true
          })
          break

        default:
          // Generic notification
          message.info(`${payload.title}: ${payload.message}`, {
            duration: 5000,
            closable: true
          })
      }
    }
  }
})

// Export store type for use in service
export type WebSocketStore = ReturnType<typeof useWebSocketStore>
