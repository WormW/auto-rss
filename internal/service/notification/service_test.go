package notification

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type eventTestChannel struct {
	payload model.NotificationPayload
	calls   int
}

func (c *eventTestChannel) Name() string    { return "event-test" }
func (c *eventTestChannel) IsEnabled() bool { return true }
func (c *eventTestChannel) Send(string, string) error {
	return nil
}
func (c *eventTestChannel) SendWithEvent(payload model.NotificationPayload) error {
	c.payload = payload
	c.calls++
	return nil
}

type plainTestChannel struct {
	title   string
	message string
	calls   int
}

func (c *plainTestChannel) Name() string    { return "plain-test" }
func (c *plainTestChannel) IsEnabled() bool { return true }
func (c *plainTestChannel) Send(title, message string) error {
	c.title, c.message = title, message
	c.calls++
	return nil
}

func TestSendSyncUsesFullPayloadForEventChannels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Notification{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	eventChannel := &eventTestChannel{}
	plainChannel := &plainTestChannel{}
	svc := &service{
		db: db,
		channels: map[string]Channel{
			eventChannel.Name(): eventChannel,
			plainChannel.Name(): plainChannel,
		},
		wsHub: nil,
	}
	payload := model.NotificationPayload{
		Event:     model.EventDownloadComplete,
		Title:     "title",
		Message:   "message",
		EventID:   "event-1",
		Timestamp: time.Now(),
	}
	if err := svc.sendSyncInternal(payload); err != nil {
		t.Fatalf("sendSyncInternal() error = %v", err)
	}

	if eventChannel.calls != 1 || eventChannel.payload.EventID != "event-1" || eventChannel.payload.Event != model.EventDownloadComplete {
		t.Fatalf("event channel received %+v after %d calls", eventChannel.payload, eventChannel.calls)
	}
	if plainChannel.calls != 1 || plainChannel.title != payload.Title || plainChannel.message != payload.Message {
		t.Fatalf("plain channel received title=%q message=%q calls=%d", plainChannel.title, plainChannel.message, plainChannel.calls)
	}
}
