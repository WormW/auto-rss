package repository

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadRepositoryUnlimitedRetryIsReady(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	download := model.Download{
		Title: "unlimited retry", TorrentURL: "magnet:unlimited", Status: model.DownloadStatusFailed,
		RetryCount: 100, MaxRetries: 0, NextRetryAt: &past,
	}
	if err := db.Create(&download).Error; err != nil {
		t.Fatalf("create download: %v", err)
	}
	if err := db.Model(&download).Update("max_retries", 0).Error; err != nil {
		t.Fatalf("set unlimited retries: %v", err)
	}

	ready, err := NewDownloadRepository(db).GetFailedDownloadsReadyForRetry(10)
	if err != nil {
		t.Fatalf("GetFailedDownloadsReadyForRetry: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != download.ID {
		t.Fatalf("ready downloads = %#v, want unlimited retry download %d", ready, download.ID)
	}
}

func TestDownloadRepository_StalledDownloadingFilter(t *testing.T) {
	// Setup in-memory test DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}
	repo := NewDownloadRepository(db)

	// Create test subscription
	testSub := model.Subscription{Name: "test-sub", RssURL: "https://example.com/rss"}
	if err := db.Create(&testSub).Error; err != nil {
		t.Fatalf("failed to create test subscription: %v", err)
	}

	// Seed test data with distinct statuses
	seed := []model.Download{
		{SubscriptionID: testSub.ID, Title: "active download", Status: "downloading", TorrentURL: "magnet:1", TorrentHash: "hash-dl1"},
		{SubscriptionID: testSub.ID, Title: "stalled download", Status: "stalled", TorrentURL: "magnet:2", TorrentHash: "hash-st1"},
		{SubscriptionID: testSub.ID, Title: "completed download", Status: "completed", TorrentURL: "magnet:3", TorrentHash: "hash-co1"},
	}
	for i := range seed {
		if err := repo.Create(&seed[i]); err != nil {
			t.Fatalf("failed to create seed download %d: %v", i, err)
		}
	}

	// Test downloading filter only returns downloading
	dlRes, dlTotal, err := repo.List(0, 10, "downloading")
	if err != nil {
		t.Fatalf("failed to filter downloading: %v", err)
	}
	if dlTotal != 1 || len(dlRes) != 1 || dlRes[0].Status != "downloading" {
		t.Fatalf("downloading filter returned %d total, %d items (status: %q), want 1 downloading", dlTotal, len(dlRes), dlRes[0].Status)
	}

	// Test stalled filter only returns stalled
	stRes, stTotal, err := repo.List(0, 10, "stalled")
	if err != nil {
		t.Fatalf("failed to filter stalled: %v", err)
	}
	if stTotal != 1 || len(stRes) != 1 || stRes[0].Status != "stalled" {
		t.Fatalf("stalled filter returned %d total, %d items (status: %q), want 1 stalled", stTotal, len(stRes), stRes[0].Status)
	}

	// Test no filter returns all
	allRes, allTotal, err := repo.List(0, 10, "")
	if err != nil {
		t.Fatalf("failed to list all: %v", err)
	}
	if allTotal != 3 || len(allRes) != 3 {
		t.Fatalf("no filter returned %d total, %d items, want 3", allTotal, len(allRes))
	}
}
