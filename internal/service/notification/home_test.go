package notification

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
)

func TestHomeNotificationChannelSendsIngestPayload(t *testing.T) {
	var gotAuth, gotContentType, gotPath string
	var gotPayload HomeNotificationPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	channel := NewHomeNotificationChannel(&HomeNotificationConfig{
		URL:     server.URL,
		Token:   "test-token",
		PageURL: "https://home.example/#notifications",
	})
	occurredAt := time.Date(2026, 9, 27, 3, 4, 5, 0, time.FixedZone("CST", 8*60*60))
	err := channel.SendWithEvent(model.NotificationPayload{
		Event:     model.EventDownloadComplete,
		Title:     "下载完成",
		Message:   "落第贤者的学院无双 第1集",
		Data:      map[string]any{"image_url": "https://img.example.test/show.jpg"},
		EventID:   "download-1",
		Timestamp: occurredAt,
	})
	if err != nil {
		t.Fatalf("SendWithEvent() error = %v", err)
	}

	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	if gotPath != "/api/notifications/ingest/auto-rss" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotPayload.EventID != "download-1" || gotPayload.Event != string(model.EventDownloadComplete) {
		t.Fatalf("payload identity = %+v", gotPayload)
	}
	if gotPayload.Level != "success" || gotPayload.URL != "https://home.example/#notifications" {
		t.Fatalf("payload mapping = %+v", gotPayload)
	}
	if gotPayload.ImageURL != "https://img.example.test/show.jpg" {
		t.Fatalf("image_url = %q", gotPayload.ImageURL)
	}
	if gotPayload.OccurredAt != "2026-09-26T19:04:05Z" {
		t.Fatalf("occurred_at = %q", gotPayload.OccurredAt)
	}
}

func TestHomeNotificationChannelOmitsUnsafeImageURLs(t *testing.T) {
	var gotPayload HomeNotificationPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	channel := NewHomeNotificationChannel(&HomeNotificationConfig{URL: server.URL, Token: "test-token"})
	err := channel.SendWithEvent(model.NotificationPayload{
		Event:   model.EventDownloadComplete,
		Title:   "下载完成",
		Message: "内容已就绪",
		Data:    map[string]any{"image_url": "/data/covers/show.jpg"},
	})
	if err != nil {
		t.Fatalf("SendWithEvent() error = %v", err)
	}
	if gotPayload.ImageURL != "" {
		t.Fatalf("unsafe image_url = %q", gotPayload.ImageURL)
	}
}

func TestHomeNotificationChannelDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	channel := NewHomeNotificationChannel(&HomeNotificationConfig{URL: server.URL, Token: "test-token"})
	err := channel.Send("title", "message")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Send() error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestHomeNotificationChannelRetriesServerErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	channel := NewHomeNotificationChannel(&HomeNotificationConfig{URL: server.URL, Token: "test-token"})
	if err := channel.Send("title", "message"); err == nil {
		t.Fatal("Send() error = nil, want failure")
	}
	if calls.Load() != homeNotificationRetryAttempts {
		t.Fatalf("calls = %d, want %d", calls.Load(), homeNotificationRetryAttempts)
	}
}

func TestHomeNotificationChannelRequiresTokenAndURL(t *testing.T) {
	if channel := NewHomeNotificationChannel(&HomeNotificationConfig{URL: "http://example.test"}); channel.IsEnabled() {
		t.Fatal("channel without token is enabled")
	}
	if channel := NewHomeNotificationChannel(&HomeNotificationConfig{Token: "test-token"}); channel.IsEnabled() {
		t.Fatal("channel without URL is enabled")
	}
}

func TestHomeNotificationChannelHonorsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Keep the handler alive beyond the client deadline without relying on
		// server-side request cancellation, which varies across transports.
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	channel := NewHomeNotificationChannel(&HomeNotificationConfig{
		URL:        server.URL,
		Token:      "test-token",
		TimeoutSec: 1,
	})
	started := time.Now()
	err := channel.Send("title", "message")
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("Send() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < time.Second || elapsed > 5*time.Second {
		t.Fatalf("Send() took %s, want bounded timeout", elapsed)
	}
}
