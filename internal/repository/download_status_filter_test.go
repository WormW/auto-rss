package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadRepositoryListFiltersByStalledAndDownloading(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open sqlite in-memory DB: %v", err)
	}

	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	repo := NewDownloadRepository(db)

	testSub := model.Subscription{Name: "test-sub", RssURL: "https://example.com/rss"}
	if err := db.Create(&testSub).Error; err != nil {
		t.Fatalf("Failed to create test subscription: %v", err)
	}

	seedDownloads := []model.Download{
		{SubscriptionID: testSub.ID, Title: "test1", Status: "downloading", TorrentURL: "magnet:1", TorrentHash: "hash1"},
		{SubscriptionID: testSub.ID, Title: "test2", Status: "stalled", TorrentURL: "magnet:2", TorrentHash: "hash2"},
		{SubscriptionID: testSub.ID, Title: "test3", Status: "completed", TorrentURL: "magnet:3", TorrentHash: "hash3"},
		{SubscriptionID: testSub.ID, Title: "test4", Status: "failed", TorrentURL: "magnet:4", TorrentHash: "hash4"},
		{SubscriptionID: testSub.ID, Title: "test5", Status: "pending", TorrentURL: "magnet:5", TorrentHash: "hash5"},
	}
	for i := range seedDownloads {
		if err := repo.Create(&seedDownloads[i]); err != nil {
			t.Fatalf("Failed to create seed download %d: %v", i, err)
		}
	}

	tests := []struct {
		name       string
		filter     string
		wantCount  int
		wantStatus string
	}{
		{name: "Filter by downloading", filter: "downloading", wantCount: 1, wantStatus: "downloading"},
		{name: "Filter by stalled", filter: "stalled", wantCount: 1, wantStatus: "stalled"},
		{name: "No filter returns all", filter: "", wantCount: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, total, err := repo.List(0, 10, tt.filter)
			if err != nil {
				t.Fatalf("List(%q) failed: %v", tt.filter, err)
			}
			if int(total) != tt.wantCount || len(res) != tt.wantCount {
				t.Fatalf("List(%q) returned %d total, %d items, want %d", tt.filter, total, len(res), tt.wantCount)
			}
			if tt.wantStatus != "" && res[0].Status != tt.wantStatus {
				t.Fatalf("List(%q) returned status %q, want %q", tt.filter, res[0].Status, tt.wantStatus)
			}
		})
	}
}
