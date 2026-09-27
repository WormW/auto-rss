package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadRepositoryListFiltersByStatus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	repo := NewDownloadRepository(db)

	sub := model.Subscription{Name: "test-sub", RssURL: "https://example.com/rss"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	seed := []model.Download{
		{SubscriptionID: sub.ID, Title: "active", Status: "downloading", TorrentURL: "magnet:?xt=urn:btih:1", TorrentHash: "hash-downloading"},
		{SubscriptionID: sub.ID, Title: "stalled", Status: "stalled", TorrentURL: "magnet:?xt=urn:btih:2", TorrentHash: "hash-stalled"},
		{SubscriptionID: sub.ID, Title: "done", Status: "completed", TorrentURL: "magnet:?xt=urn:btih:3", TorrentHash: "hash-completed"},
	}
	for i := range seed {
		if err := repo.Create(&seed[i]); err != nil {
			t.Fatalf("create download %d: %v", i, err)
		}
	}

	tests := []struct {
		status string
		want   int
		wantID string
	}{
		{status: "downloading", want: 1, wantID: "hash-downloading"},
		{status: "stalled", want: 1, wantID: "hash-stalled"},
		{status: "", want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got, total, err := repo.List(0, 10, tt.status)
			if err != nil {
				t.Fatalf("List(%q): %v", tt.status, err)
			}
			if len(got) != tt.want || int(total) != tt.want {
				t.Fatalf("List(%q) returned len=%d total=%d, want %d", tt.status, len(got), total, tt.want)
			}
			if tt.wantID != "" && got[0].TorrentHash != tt.wantID {
				t.Fatalf("List(%q) first hash=%q, want %q", tt.status, got[0].TorrentHash, tt.wantID)
			}
		})
	}
}

func TestDownloadRepositoryGetByHashesBatchesAndNormalizes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Download{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	seed := []model.Download{
		{Title: "one", TorrentURL: "magnet:one", TorrentHash: "abc123", Status: model.DownloadStatusDownloading},
		{Title: "two", TorrentURL: "magnet:two", TorrentHash: "def456", Status: model.DownloadStatusStalled},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("create download %d: %v", i, err)
		}
	}

	type batchLookup interface {
		GetByHashes([]string) (map[string]*model.Download, error)
	}
	repo, ok := NewDownloadRepository(db).(batchLookup)
	if !ok {
		t.Fatal("download repository does not expose the batch lookup adapter")
	}
	got, err := repo.GetByHashes([]string{" abc123 ", "DEF456", "missing", "abc123"})
	if err != nil {
		t.Fatalf("GetByHashes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetByHashes returned %d records, want 2", len(got))
	}
	if got["abc123"] == nil || got["abc123"].Title != "one" {
		t.Fatalf("GetByHashes missing normalized abc123 result: %#v", got["abc123"])
	}
	if got["def456"] == nil || got["def456"].Title != "two" {
		t.Fatalf("GetByHashes missing normalized def456 result: %#v", got["def456"])
	}
}
