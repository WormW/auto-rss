package notification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"gorm.io/gorm"
)

// Service 通知服务接口
type Service interface {
	Send(payload model.NotificationPayload)
	SendSync(payload model.NotificationPayload) error
	RegisterChannel(channel Channel)
	UnregisterChannel(name string)
	GetChannels() []string
	GetWebSocketHub() *WebSocketHub
}

// Channel 通知渠道接口
type Channel interface {
	Name() string
	Send(title, message string) error
	IsEnabled() bool
}

type service struct {
	db          *gorm.DB
	channels    map[string]Channel
	channelsMux sync.RWMutex
	dedupeCache *dedupeCache
	wsHub       *WebSocketHub
}

type dedupeCache struct {
	items map[string]time.Time
	mu    sync.RWMutex
	ttl   time.Duration
}

func newDedupeCache(ttl time.Duration) *dedupeCache {
	c := &dedupeCache{
		items: make(map[string]time.Time),
		ttl:   ttl,
	}
	go c.cleanup()
	return c
}

func (c *dedupeCache) Add(eventID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[eventID]; exists {
		return false
	}
	c.items[eventID] = time.Now()
	return true
}

func (c *dedupeCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for id, t := range c.items {
			if now.Sub(t) > c.ttl {
				delete(c.items, id)
			}
		}
		c.mu.Unlock()
	}
}

// NewService 创建通知服务实例
func NewService(db *gorm.DB) Service {
	svc := &service{
		db:          db,
		channels:    make(map[string]Channel),
		dedupeCache: newDedupeCache(5 * time.Minute),
		wsHub:       NewWebSocketHub(),
	}
	go svc.wsHub.Run()
	svc.loadChannels()
	return svc
}

// GetWebSocketHub 获取 WebSocket Hub
func (s *service) GetWebSocketHub() *WebSocketHub {
	return s.wsHub
}

func (s *service) loadChannels() {
	var settings []model.NotificationSetting
	if err := s.db.Find(&settings).Error; err != nil {
		logger.Error("Failed to load notification settings", "error", err)
		return
	}
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}
		switch setting.Channel {
		case "telegram":
			config := &TelegramConfig{}
			if err := json.Unmarshal([]byte(setting.Config), config); err != nil {
				logger.Error("Failed to unmarshal telegram config", "error", err)
				continue
			}
			channel := NewTelegramChannel(config)
			s.RegisterChannel(channel)
			logger.Info("Telegram channel registered")
		case "webhook":
			logger.Info("Webhook channel not yet implemented")
		case "email":
			logger.Info("Email channel not yet implemented")
		}
	}
}

// RegisterChannel 注册通知渠道
func (s *service) RegisterChannel(channel Channel) {
	s.channelsMux.Lock()
	defer s.channelsMux.Unlock()
	s.channels[channel.Name()] = channel
}

// UnregisterChannel 注销通知渠道
func (s *service) UnregisterChannel(name string) {
	s.channelsMux.Lock()
	defer s.channelsMux.Unlock()
	delete(s.channels, name)
}

// GetChannels 获取所有已注册的渠道名称
func (s *service) GetChannels() []string {
	s.channelsMux.RLock()
	defer s.channelsMux.RUnlock()
	names := make([]string, 0, len(s.channels))
	for name := range s.channels {
		names = append(names, name)
	}
	return names
}

func generateEventID(payload model.NotificationPayload) string {
	data := fmt.Sprintf("%s:%s:%d", payload.Event, payload.Title, payload.Timestamp.Unix()/60)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8])
}

// Send 异步发送通知
func (s *service) Send(payload model.NotificationPayload) {
	if payload.EventID == "" {
		payload.EventID = generateEventID(payload)
	}
	if !s.dedupeCache.Add(payload.EventID) {
		logger.Debug("Notification deduplicated", "event_id", payload.EventID)
		return
	}
	go s.sendInternal(payload)
}

// SendSync 同步发送通知
func (s *service) SendSync(payload model.NotificationPayload) error {
	if payload.EventID == "" {
		payload.EventID = generateEventID(payload)
	}
	if !s.dedupeCache.Add(payload.EventID) {
		logger.Debug("Notification deduplicated", "event_id", payload.EventID)
		return nil
	}
	return s.sendSyncInternal(payload)
}

func (s *service) sendInternal(payload model.NotificationPayload) {
	if s.wsHub != nil {
		s.wsHub.Broadcast(payload)
	}
	s.channelsMux.RLock()
	channels := make(map[string]Channel, len(s.channels))
	for name, ch := range s.channels {
		channels[name] = ch
	}
	s.channelsMux.RUnlock()
	for name, channel := range channels {
		if !channel.IsEnabled() {
			continue
		}
		go func(channelName string, ch Channel) {
			if err := ch.Send(payload.Title, payload.Message); err != nil {
				logger.Error("Failed to send notification", "channel", channelName, "error", err)
				s.saveNotification(channelName, payload, "failed", err.Error())
			} else {
				s.saveNotification(channelName, payload, "sent", "")
			}
		}(name, channel)
	}
}

func (s *service) sendSyncInternal(payload model.NotificationPayload) error {
	if s.wsHub != nil {
		s.wsHub.Broadcast(payload)
	}
	s.channelsMux.RLock()
	channels := make(map[string]Channel, len(s.channels))
	for name, ch := range s.channels {
		channels[name] = ch
	}
	s.channelsMux.RUnlock()
	var lastErr error
	for name, channel := range channels {
		if !channel.IsEnabled() {
			continue
		}
		if err := channel.Send(payload.Title, payload.Message); err != nil {
			logger.Error("Failed to send notification", "channel", name, "error", err)
			s.saveNotification(name, payload, "failed", err.Error())
			lastErr = err
		} else {
			s.saveNotification(name, payload, "sent", "")
		}
	}
	return lastErr
}

func (s *service) saveNotification(channel string, payload model.NotificationPayload, status, errMsg string) {
	notification := model.Notification{
		Type:    channel,
		Title:   payload.Title,
		Message: payload.Message,
		Status:  status,
		Error:   errMsg,
		EventID: payload.EventID,
	}
	if dbErr := s.db.Create(&notification).Error; dbErr != nil {
		logger.Error("Failed to save notification record", "error", dbErr)
	}
}
