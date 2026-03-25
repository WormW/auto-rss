package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
)

const (
	telegramAPIBase = "https://api.telegram.org/bot%s/%s"
	timeout         = 30 * time.Second
)

// TelegramConfig Telegram 配置
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	Proxy    string `json:"proxy,omitempty"`
}

// TelegramChannel Telegram 通知渠道
type TelegramChannel struct {
	config  *TelegramConfig
	client  *http.Client
	enabled bool
}

// NewTelegramChannel 创建 Telegram 通知渠道
func NewTelegramChannel(config *TelegramConfig) *TelegramChannel {
	return &TelegramChannel{
		config: config,
		client: &http.Client{
			Timeout: timeout,
		},
		enabled: config.BotToken != "" && config.ChatID != "",
	}
}

// Name 返回渠道名称
func (t *TelegramChannel) Name() string {
	return "telegram"
}

// IsEnabled 返回渠道是否启用
func (t *TelegramChannel) IsEnabled() bool {
	return t.enabled
}

// Send 发送 Telegram 消息
func (t *TelegramChannel) Send(title, message string) error {
	if !t.enabled {
		return fmt.Errorf("telegram channel not enabled")
	}
	text := fmt.Sprintf("*%s*\n\n%s", escapeMarkdown(title), escapeMarkdown(message))
	payload := map[string]interface{}{
		"chat_id":    t.config.ChatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}
	url := fmt.Sprintf(telegramAPIBase, t.config.BotToken, "sendMessage")
	resp, err := t.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
			if description, ok := result["description"].(string); ok {
				return fmt.Errorf("telegram API error: %s", description)
			}
		}
		return fmt.Errorf("telegram API returned status: %d", resp.StatusCode)
	}
	var result telegramResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Warn("Failed to decode telegram response", "error", err)
		return nil
	}
	if !result.OK {
		if result.Description != "" {
			return fmt.Errorf("telegram API error: %s", result.Description)
		}
		return fmt.Errorf("telegram API returned not ok")
	}
	logger.Debug("Telegram notification sent successfully", "chat_id", t.config.ChatID)
	return nil
}

type telegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
	Result      struct {
		MessageID int64 `json:"message_id,omitempty"`
	} `json:"result,omitempty"`
}

func escapeMarkdown(text string) string {
	chars := []rune{'_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'}
	result := []rune(text)
	var escaped []rune
	for _, r := range result {
		needsEscape := false
		for _, c := range chars {
			if r == c {
				needsEscape = true
				break
			}
		}
		if needsEscape {
			escaped = append(escaped, '\\')
		}
		escaped = append(escaped, r)
	}
	return string(escaped)
}

// TestTelegramConfig 测试 Telegram 配置
func TestTelegramConfig(config *TelegramConfig) error {
	channel := NewTelegramChannel(config)
	if !channel.IsEnabled() {
		return fmt.Errorf("telegram config incomplete: bot_token and chat_id are required")
	}
	return channel.Send("Auto RSS 连接测试", "✅ 配置成功！您已成功启用 Telegram 通知。")
}

// TelegramBotInfo Bot 信息
type TelegramBotInfo struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}
