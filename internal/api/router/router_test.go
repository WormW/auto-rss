package router

import (
	"errors"
	"testing"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/scheduler"
	"github.com/robfig/cron/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mockScheduler struct{ startErr error }

func (m *mockScheduler) Start() error { return m.startErr }
func (m *mockScheduler) Stop()        {}
func (m *mockScheduler) AddJob(string, func()) (cron.EntryID, error) {
	return 0, nil
}
func (m *mockScheduler) RunRSSCheckNow() error { return nil }

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	return db
}

func TestSetup_ReturnsErrorWhenSchedulerStartFailsAndBlockingEnabled(t *testing.T) {
	db := newTestDB(t)
	cfg := &config.Config{RSSInterval: "30m", BlockAPIBootOnSchedulerFailure: true}
	qbClient := downloader.NewQBittorrentClient()

	original := newScheduler
	newScheduler = func(*gorm.DB, repository.SubscriptionRepository, repository.DownloadRepository, repository.ConfigRepository, string, rss.Parser, downloader.QBittorrentClient) scheduler.Scheduler {
		return &mockScheduler{startErr: errors.New("boom")}
	}
	defer func() { newScheduler = original }()

	_, err := Setup(db, cfg, qbClient, nil)
	if err == nil {
		t.Fatalf("expected error when scheduler start fails")
	}
}
