package notification

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/api/middleware"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Client WebSocket 客户端连接
type Client struct {
	hub      *WebSocketHub
	conn     *websocket.Conn
	send     chan []byte
	clientID string
}

// WebSocketHub WebSocket 连接管理器
type WebSocketHub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	mu         sync.RWMutex
}

// NewWebSocketHub 创建 WebSocket Hub 实例
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
	}
}

// Run 启动 Hub 事件循环
func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			logger.Info("WebSocket client registered", "client_id", client.clientID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				logger.Info("WebSocket client unregistered", "client_id", client.clientID)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.mu.RUnlock()

			for _, client := range clients {
				select {
				case client.send <- message:
				default:
					h.mu.Lock()
					delete(h.clients, client)
					close(client.send)
					client.conn.Close()
					h.mu.Unlock()
				}
			}
		}
	}
}

// Broadcast 广播消息给所有客户端
func (h *WebSocketHub) Broadcast(payload model.NotificationPayload) {
	if h == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("Failed to marshal notification payload", "error", err)
		return
	}
	select {
	case h.broadcast <- data:
	default:
		logger.Warn("WebSocket broadcast channel full, message dropped")
	}
}

// GetClientCount 获取当前连接数
func (h *WebSocketHub) GetClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Warn("WebSocket unexpected close", "error", err)
			}
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}
			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// HandleWebSocket 处理 WebSocket 连接升级
func HandleWebSocket(hub *WebSocketHub, jwtService middleware.JWTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract token from query parameter (optional for read-only access)
		token := c.Query("token")
		
		var clientID string
		
		if token != "" {
			// Validate the token if provided
			claims, err := jwtService.ValidateAccessToken(token)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"code":    401,
					"message": "Invalid or expired token",
				})
				return
			}
			clientID = fmt.Sprintf("user_%d_%s_%s", claims.UserID, c.ClientIP(), time.Now().Format("20060102150405"))
		} else {
			// Anonymous connection (no token)
			clientID = fmt.Sprintf("anon_%s_%s", c.ClientIP(), time.Now().Format("20060102150405"))
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logger.Error("Failed to upgrade WebSocket connection", "error", err)
			return
		}
		client := &Client{
			hub:      hub,
			conn:     conn,
			send:     make(chan []byte, 256),
			clientID: clientID,
		}
		client.hub.register <- client
		go client.writePump()
		go client.readPump()
	}
}
