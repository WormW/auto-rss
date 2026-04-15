import type { NotificationPayload } from '../stores/websocket'
import type { WebSocketStore } from '../stores/websocket'

// Configuration constants (per D-07, D-08, D-09)
const BASE_DELAY = 1000 // 1 second
const MAX_DELAY = 30000 // 30 seconds
const MAX_RETRIES = 10
const JITTER_FACTOR = 0.5 // ±50%

// Buffer configuration (per D-14, D-15, D-16, D-17)
const MAX_BUFFER_SIZE = 100
const BUFFER_TTL_MS = 300000 // 5 minutes

export type ConnectionStatus =
  | 'connecting'
  | 'connected'
  | 'disconnected'
  | 'error'
  | 'max_retries_exceeded'

export interface BufferedMessage {
  payload: NotificationPayload
  timestamp: number
}

export class WebSocketService {
  private ws: WebSocket | null = null
  private url: string
  private reconnectAttempt: number = 0
  private reconnectTimer: number | null = null
  private messageBuffer: BufferedMessage[] = []
  private store: WebSocketStore
  private isManualClose: boolean = false
  private currentToken: string | null = null

  constructor(store: WebSocketStore) {
    this.store = store
    this.url = this.buildWebSocketUrl()

    // Page visibility handling (per D-13)
    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', this.handleVisibilityChange.bind(this))
    }
  }

  /**
   * Build WebSocket URL based on current window location
   */
  private buildWebSocketUrl(): string {
    if (typeof window === 'undefined') {
      return ''
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${protocol}//${window.location.host}/ws/notifications`
  }

  /**
   * Calculate reconnection delay with exponential backoff and jitter (per D-07, D-08)
   */
  private calculateDelay(): number {
    const exponential = BASE_DELAY * Math.pow(2, this.reconnectAttempt)
    const capped = Math.min(exponential, MAX_DELAY)
    const jitter = capped * JITTER_FACTOR * (Math.random() * 2 - 1)
    return Math.max(0, Math.floor(capped + jitter))
  }

  /**
   * Connect to WebSocket server with optional JWT token
   */
  connect(token?: string): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      console.log('[WebSocket] Already connected')
      return
    }

    this.currentToken = token || null
    this.isManualClose = false
    this.store.setStatus('connecting')

    try {
      // Build URL with optional token
      const urlWithToken = token 
        ? `${this.url}?token=${encodeURIComponent(token)}`
        : this.url
      this.ws = new WebSocket(urlWithToken)

      this.ws.onopen = this.onOpen.bind(this)
      this.ws.onclose = this.onClose.bind(this)
      this.ws.onerror = this.onError.bind(this)
      this.ws.onmessage = this.onMessage.bind(this)
    } catch (error) {
      console.error('[WebSocket] Connection error:', error)
      this.store.setStatus('error', 'Failed to create WebSocket connection')
      this.scheduleReconnect()
    }
  }

  /**
   * Disconnect WebSocket connection cleanly
   */
  disconnect(): void {
    this.isManualClose = true
    this.clearReconnectTimer()

    if (this.ws) {
      if (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING) {
        this.ws.close(1000, 'Manual disconnect')
      }
      this.ws = null
    }

    this.store.setStatus('disconnected')
    this.reconnectAttempt = 0
    this.store.setReconnectAttempt(0)
    this.store.setNextReconnectDelay(0)
  }

  /**
   * Force immediate reconnection attempt
   */
  reconnect(): void {
    this.clearReconnectTimer()
    this.isManualClose = false

    if (this.ws) {
      this.ws.close(1000, 'Reconnecting')
      this.ws = null
    }

    // Reconnect with or without token
    this.connect(this.currentToken || undefined)
  }

  /**
   * Send message or buffer it if disconnected
   */
  send(payload: NotificationPayload): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      try {
        this.ws.send(JSON.stringify(payload))
      } catch (error) {
        console.error('[WebSocket] Send error:', error)
        this.bufferMessage(payload)
      }
    } else {
      this.bufferMessage(payload)
    }
  }

  /**
   * Get current connection status
   */
  getStatus(): ConnectionStatus {
    return this.store.status
  }

  /**
   * Check if WebSocket is connected
   */
  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  /**
   * Handle visibility change - reconnect if visible and disconnected (per D-13)
   */
  private handleVisibilityChange(): void {
    if (document.visibilityState === 'visible' && this.store.status === 'disconnected') {
      console.log('[WebSocket] Page visible, triggering reconnect')
      this.reconnect()
    }
  }

  /**
   * WebSocket open handler
   */
  private onOpen(): void {
    console.log('[WebSocket] Connected')
    this.store.setStatus('connected')
    this.reconnectAttempt = 0
    this.store.setReconnectAttempt(0)
    this.store.setNextReconnectDelay(0)
    this.flushBuffer()
  }

  /**
   * WebSocket close handler
   */
  private onClose(event: CloseEvent): void {
    console.log(`[WebSocket] Closed with code: ${event.code}`)

    // Normal close (code 1000) does not trigger reconnection (per D-11)
    if (event.code === 1000 || this.isManualClose) {
      this.store.setStatus('disconnected')
      return
    }

    // Abnormal close triggers reconnection (per D-12)
    this.store.setStatus('disconnected')
    this.scheduleReconnect()
  }

  /**
   * WebSocket error handler
   */
  private onError(event: Event): void {
    console.error('[WebSocket] Error:', event)
    this.store.setStatus('error', 'WebSocket connection error')
  }

  /**
   * WebSocket message handler
   */
  private onMessage(event: MessageEvent): void {
    try {
      const payload: NotificationPayload = JSON.parse(event.data)
      this.store.onMessageReceived(payload)
    } catch (error) {
      console.error('[WebSocket] Failed to parse message:', error)
    }
  }

  /**
   * Schedule reconnection with exponential backoff
   */
  private scheduleReconnect(): void {
    if (this.reconnectAttempt >= MAX_RETRIES) {
      console.log('[WebSocket] Max retries exceeded')
      this.store.setStatus('max_retries_exceeded')
      return
    }

    const delay = this.calculateDelay()
    this.reconnectAttempt++
    this.store.setReconnectAttempt(this.reconnectAttempt)
    this.store.setNextReconnectDelay(delay)

    console.log(`[WebSocket] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempt}/${MAX_RETRIES})`)

    this.reconnectTimer = window.setTimeout(() => {
      // Reconnect with or without token
      this.connect(this.currentToken || undefined)
    }, delay)
  }

  /**
   * Clear reconnect timer
   */
  private clearReconnectTimer(): void {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }

  /**
   * Buffer message when disconnected (per D-14, D-15, D-16, D-17)
   */
  private bufferMessage(payload: NotificationPayload): void {
    // Drop oldest messages when buffer is full (per T-06-06 threat mitigation)
    if (this.messageBuffer.length >= MAX_BUFFER_SIZE) {
      this.messageBuffer.shift()
    }

    this.messageBuffer.push({
      payload,
      timestamp: Date.now()
    })

    this.store.setBufferedMessageCount(this.messageBuffer.length)
  }

  /**
   * Flush buffered messages after reconnection
   */
  private flushBuffer(): void {
    if (this.messageBuffer.length === 0) {
      return
    }

    const now = Date.now()
    const validMessages: BufferedMessage[] = []

    // Filter out expired messages (TTL check)
    for (const item of this.messageBuffer) {
      if (now - item.timestamp <= BUFFER_TTL_MS) {
        validMessages.push(item)
      }
    }

    // Send valid messages in order
    for (const item of validMessages) {
      try {
        this.ws?.send(JSON.stringify(item.payload))
      } catch (error) {
        console.error('[WebSocket] Failed to send buffered message:', error)
      }
    }

    this.messageBuffer = []
    this.store.setBufferedMessageCount(0)

    console.log(`[WebSocket] Flushed ${validMessages.length} buffered messages`)
  }

  /**
   * Clean up resources
   */
  dispose(): void {
    this.disconnect()

    if (typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', this.handleVisibilityChange.bind(this))
    }
  }
}

/**
 * Factory function to create WebSocket service instance
 */
export function createWebSocketService(store: WebSocketStore): WebSocketService {
  return new WebSocketService(store)
}

export default WebSocketService
