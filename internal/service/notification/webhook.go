package notification

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
)

const (
	webhookTimeout         = 30 * time.Second
	webhookRetryAttempts   = 3
	webhookRetryBaseDelay  = 1 * time.Second
)

// WebhookConfig Webhook 配置
type WebhookConfig struct {
	Name         string            `json:"name"`                    // 渠道名称（如 nanobot、openclaw）
	URL          string            `json:"url"`                     // Webhook URL
	Method       string            `json:"method"`                  // HTTP 方法：POST/PUT/PATCH
	Headers      map[string]string `json:"headers"`                 // 自定义 Headers
	BodyTemplate string            `json:"body_template"`           // Body 模板（JSON 或文本）
	ContentType  string            `json:"content_type"`            // Content-Type: application/json 或 text/plain
	Secret       string            `json:"secret,omitempty"`        // HMAC 签名密钥（可选）
	RetryEnabled bool              `json:"retry_enabled"`           // 是否启用重试
	TimeoutSec   int               `json:"timeout_sec,omitempty"`   // 超时时间（秒）
}

// WebhookChannel Webhook 通知渠道
type WebhookChannel struct {
	config  *WebhookConfig
	client  *http.Client
	enabled bool
}

// WebhookPayload Webhook 消息载荷
type WebhookPayload struct {
	Event     string                 `json:"event"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data"`
	DataJSON  string                 `json:"_data_json"`  // Data 的 JSON 字符串（用于模板）
	Timestamp int64                  `json:"timestamp"`
	EventID   string                 `json:"event_id"`
}

// NewWebhookChannel 创建 Webhook 通知渠道
func NewWebhookChannel(config *WebhookConfig) *WebhookChannel {
	if config.Method == "" {
		config.Method = "POST"
	}
	if config.ContentType == "" {
		config.ContentType = "application/json"
	}
	if config.TimeoutSec <= 0 {
		config.TimeoutSec = 30
	}

	return &WebhookChannel{
		config:  config,
		client:  &http.Client{
			Timeout: time.Duration(config.TimeoutSec) * time.Second,
		},
		enabled: config.URL != "",
	}
}

// Name 返回渠道名称
func (w *WebhookChannel) Name() string {
	if w.config.Name != "" {
		return "webhook." + w.config.Name
	}
	return "webhook"
}

// IsEnabled 返回渠道是否启用
func (w *WebhookChannel) IsEnabled() bool {
	return w.enabled
}

// Send 发送 Webhook 通知
func (w *WebhookChannel) Send(title, message string) error {
	if !w.enabled {
		return fmt.Errorf("webhook channel not enabled: url is empty")
	}

	// 构建 payload
	payload := WebhookPayload{
		Title:   title,
		Message: message,
		Timestamp: time.Now().Unix(),
	}

	return w.sendWithRetry(payload)
}

// SendWithEvent 发送带事件类型的 Webhook 通知
func (w *WebhookChannel) SendWithEvent(event, title, message string, data map[string]interface{}, eventID string) error {
	if !w.enabled {
		return fmt.Errorf("webhook channel not enabled: url is empty")
	}

	// 将 data 转为 JSON 字符串供模板使用
	dataJSON, _ := json.Marshal(data)

	payload := WebhookPayload{
		Event:     event,
		Title:     title,
		Message:   message,
		Data:      data,
		DataJSON:  string(dataJSON),
		Timestamp: time.Now().Unix(),
		EventID:   eventID,
	}

	return w.sendWithRetry(payload)
}

// sendWithRetry 带重试的发送
func (w *WebhookChannel) sendWithRetry(payload WebhookPayload) error {
	maxAttempts := 1
	if w.config.RetryEnabled {
		maxAttempts = webhookRetryAttempts
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := webhookRetryBaseDelay * time.Duration(attempt)
			logger.Debug("Webhook retry waiting",
				"attempt", attempt,
				"delay", delay.String(),
				"channel", w.Name())
			time.Sleep(delay)
		}

		err := w.doSend(payload)
		if err == nil {
			return nil
		}

		lastErr = err
		logger.Warn("Webhook send failed, will retry",
			"attempt", attempt+1,
			"max_attempts", maxAttempts,
			"channel", w.Name(),
			"error", err.Error())
	}

	return fmt.Errorf("webhook send failed after %d attempts: %w", maxAttempts, lastErr)
}

// doSend 执行实际发送
func (w *WebhookChannel) doSend(payload WebhookPayload) error {
	// 构建请求 body
	body, err := w.buildBody(payload)
	if err != nil {
		return fmt.Errorf("failed to build webhook body: %w", err)
	}

	// 创建请求
	req, err := http.NewRequest(w.config.Method, w.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	// 设置 Headers
	req.Header.Set("Content-Type", w.config.ContentType)
	for key, value := range w.config.Headers {
		req.Header.Set(key, value)
	}

	// 添加 HMAC 签名（如果配置了 secret）
	if w.config.Secret != "" {
		signature := w.generateSignature(body)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// 发送请求
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应 body（用于错误日志）
	respBody, _ := io.ReadAll(resp.Body)

	// 检查响应状态
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status: %d, body: %s",
			resp.StatusCode, string(respBody))
	}

	logger.Debug("Webhook sent successfully",
		"channel", w.Name(),
		"url", w.config.URL,
		"status", resp.StatusCode,
		"response", string(respBody))

	return nil
}

// buildBody 构建请求 body
func (w *WebhookChannel) buildBody(payload WebhookPayload) ([]byte, error) {
	// 如果没有配置模板，使用默认 JSON 格式
	if w.config.BodyTemplate == "" {
		return json.Marshal(payload)
	}

	// 使用模板引擎
	tmpl, err := template.New("webhook").Parse(w.config.BodyTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse body template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		return nil, fmt.Errorf("failed to execute body template: %w", err)
	}

	return buf.Bytes(), nil
}

// generateSignature 生成 HMAC-SHA256 签名
func (w *WebhookChannel) generateSignature(body []byte) string {
	h := hmac.New(sha256.New, []byte(w.config.Secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateSignature 验证 Webhook 签名
func ValidateSignature(body []byte, signature, secret string) bool {
	if secret == "" || signature == "" {
		return false
	}

	// 支持 hex 或 base64 编码的签名
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	expected := hex.EncodeToString(h.Sum(nil))

	// 支持带前缀的签名（如 sha256=xxx）
	if strings.Contains(signature, "=") {
		parts := strings.SplitN(signature, "=", 2)
		if len(parts) == 2 {
			signature = parts[1]
		}
	}

	return hmac.Equal([]byte(expected), []byte(signature))
}

// TestWebhookConfig 测试 Webhook 配置
func TestWebhookConfig(config *WebhookConfig) error {
	channel := NewWebhookChannel(config)
	if !channel.IsEnabled() {
		return fmt.Errorf("webhook config incomplete: url is required")
	}

	// 发送测试消息
	testPayload := WebhookPayload{
		Event:     "test",
		Title:     "Auto RSS 连接测试",
		Message:   "✅ 配置成功！您已成功启用 Webhook 通知。\n\n此消息用于验证 Webhook 配置是否正确。",
		Data:      map[string]interface{}{"test": true},
		Timestamp: time.Now().Unix(),
		EventID:   "test-" + fmt.Sprintf("%d", time.Now().Unix()),
	}

	return channel.doSend(testPayload)
}

// PredefinedTemplates 预定义的模板
var PredefinedTemplates = map[string]string{
	"default": `{
  "event": "{{.Event}}",
  "title": "{{.Title}}",
  "message": "{{.Message}}",
  "timestamp": {{.Timestamp}},
  "event_id": "{{.EventID}}",
  "data": {{.DataJSON}}
}`,
	"nanobot": `{
  "source": "auto-rss",
  "event": "{{.Event}}",
  "title": "{{.Title}}",
  "message": "{{.Message}}",
  "timestamp": {{.Timestamp}},
  "data": {{.DataJSON}}
}`,
	"openclaw": `{
  "session": "auto-rss-notifications",
  "message": "**{{.Title}}**\n\n{{.Message}}"
}`,
	"discord": `{
  "username": "Auto RSS",
  "embeds": [{
    "title": "{{.Title}}",
    "description": "{{.Message}}",
    "timestamp": "{{.Timestamp}}",
    "color": 3447003
  }]
}`,
	"slack": `{
  "text": "*{{.Title}}*\n{{.Message}}"
}`,
}

// GetPredefinedTemplate 获取预定义模板
func GetPredefinedTemplate(name string) string {
	if tmpl, ok := PredefinedTemplates[name]; ok {
		return tmpl
	}
	return PredefinedTemplates["default"]
}
