package notification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
)

const (
	defaultHomeNotificationSourceID = "auto-rss"
	homeNotificationTimeout         = 10 * time.Second
	homeNotificationRetryAttempts   = 3
	homeNotificationRetryDelay      = 100 * time.Millisecond
)

// HomeNotificationConfig 配置 home-console 通知渠道。
// URL 可以填写 home-console 根地址，也可以直接填写完整 ingest 地址。
type HomeNotificationConfig struct {
	URL        string
	Token      string
	PageURL    string
	SourceID   string
	TimeoutSec int
}

// HomeNotificationChannel 将 Auto-RSS 事件写入 home-console 通知中心。
type HomeNotificationChannel struct {
	config   HomeNotificationConfig
	endpoint string
	client   *http.Client
	enabled  bool
}

// HomeNotificationPayload 是 home-console ingest 接口接受的载荷。
type HomeNotificationPayload struct {
	EventID    string `json:"event_id"`
	Title      string `json:"title"`
	Message    string `json:"message"`
	Level      string `json:"level"`
	Event      string `json:"event"`
	URL        string `json:"url,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`
	OccurredAt string `json:"occurred_at"`
}

// NewHomeNotificationChannel 创建 home-console 通知渠道。
func NewHomeNotificationChannel(config *HomeNotificationConfig) *HomeNotificationChannel {
	if config == nil {
		config = &HomeNotificationConfig{}
	}

	copied := *config
	if strings.TrimSpace(copied.SourceID) == "" {
		copied.SourceID = defaultHomeNotificationSourceID
	}
	if copied.TimeoutSec <= 0 {
		copied.TimeoutSec = int(homeNotificationTimeout / time.Second)
	}

	endpoint, err := buildHomeNotificationEndpoint(copied.URL, copied.SourceID)
	if err != nil {
		endpoint = ""
	}

	return &HomeNotificationChannel{
		config:   copied,
		endpoint: endpoint,
		client:   &http.Client{Timeout: time.Duration(copied.TimeoutSec) * time.Second},
		enabled:  endpoint != "" && strings.TrimSpace(copied.Token) != "",
	}
}

// Name 返回渠道名称。
func (h *HomeNotificationChannel) Name() string {
	return "home"
}

// IsEnabled 返回渠道是否启用。
func (h *HomeNotificationChannel) IsEnabled() bool {
	return h != nil && h.enabled
}

// Send 兼容通用 Channel 接口。通知服务发送事件时会优先调用 SendWithEvent。
func (h *HomeNotificationChannel) Send(title, message string) error {
	payload := model.NotificationPayload{
		Event:     model.NotificationEvent("notification"),
		Title:     title,
		Message:   message,
		Timestamp: time.Now().UTC(),
	}
	return h.SendWithEvent(payload)
}

// SendWithEvent 保留 Auto-RSS 原始 event_id，供 home-console 做幂等去重。
func (h *HomeNotificationChannel) SendWithEvent(payload model.NotificationPayload) error {
	if h == nil || !h.enabled {
		return fmt.Errorf("home notification channel not enabled: url and token are required")
	}
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now().UTC()
	}
	if payload.EventID == "" {
		payload.EventID = generateEventID(payload)
	}

	body, err := json.Marshal(HomeNotificationPayload{
		EventID:    payload.EventID,
		Title:      payload.Title,
		Message:    payload.Message,
		Level:      homeNotificationLevel(payload.Event),
		Event:      string(payload.Event),
		URL:        h.config.PageURL,
		ImageURL:   homeNotificationImageURL(payload),
		OccurredAt: payload.Timestamp.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("failed to encode home notification: %w", err)
	}

	return h.sendWithRetry(body)
}

func homeNotificationImageURL(payload model.NotificationPayload) string {
	if payload.Data == nil {
		return ""
	}
	raw, ok := payload.Data["image_url"].(string)
	if !ok {
		return ""
	}
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return ""
	}
	return raw
}

func (h *HomeNotificationChannel) sendWithRetry(body []byte) error {
	var lastErr error
	for attempt := 1; attempt <= homeNotificationRetryAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(homeNotificationRetryDelay * time.Duration(attempt-1))
		}

		err := h.doSend(body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryableHomeNotificationError(err) || attempt == homeNotificationRetryAttempts {
			break
		}
		logger.Warn("Home notification send failed, will retry",
			"attempt", attempt,
			"max_attempts", homeNotificationRetryAttempts,
			"channel", h.Name(),
			"error", err.Error())
	}

	return fmt.Errorf("home notification send failed: %w", lastErr)
}

func (h *HomeNotificationChannel) doSend(body []byte) error {
	req, err := http.NewRequest(http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil {
		return &homeNotificationError{err: fmt.Errorf("failed to create request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+h.config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return &homeNotificationError{err: fmt.Errorf("failed to send request: %w", err)}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &homeNotificationError{
			status: resp.StatusCode,
			body:   strings.TrimSpace(string(respBody)),
		}
	}

	logger.Debug("Home notification sent", "channel", h.Name(), "status", resp.StatusCode)
	return nil
}

type homeNotificationError struct {
	status int
	body   string
	err    error
}

func (e *homeNotificationError) Error() string {
	if e.status != 0 {
		if e.body == "" {
			return fmt.Sprintf("home notification returned non-2xx status: %d", e.status)
		}
		return fmt.Sprintf("home notification returned non-2xx status: %d, body: %s", e.status, e.body)
	}
	if e.err != nil {
		return e.err.Error()
	}
	return "home notification request failed"
}

func (e *homeNotificationError) Unwrap() error { return e.err }

func isRetryableHomeNotificationError(err error) bool {
	var homeErr *homeNotificationError
	if !errors.As(err, &homeErr) {
		return true
	}
	return homeErr.status == 0 || homeErr.status == http.StatusTooManyRequests || homeErr.status >= 500
}

func buildHomeNotificationEndpoint(rawURL, sourceID string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("home notification URL is empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid home notification URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("home notification URL must use http or https")
	}
	if !strings.Contains(parsed.Path, "/api/notifications/ingest/") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/notifications/ingest/" + url.PathEscape(sourceID)
	}
	return parsed.String(), nil
}

func homeNotificationLevel(event model.NotificationEvent) string {
	switch event {
	case model.EventDownloadComplete:
		return "success"
	case model.EventDownloadFailed, model.EventSystemError:
		return "error"
	default:
		return "info"
	}
}
