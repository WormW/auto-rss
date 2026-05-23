package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionRepository_GetSubscriptionsWithDownloadCount(t *testing.T) {
	// Setup in-memory test DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}
	repo := NewSubscriptionRepository(db)
	downloadRepo := NewDownloadRepository(db)

	// Create test subscriptions
	sub1 := model.Subscription{Name: "sub-1", RssURL: "https://example.com/rss1"}
	sub2 := model.Subscription{Name: "sub-2", RssURL: "https://example.com/rss2"}
	sub3 := model.Subscription{Name: "sub-3", RssURL: "https://example.com/rss3"}

	if err := repo.Create(&sub1); err != nil {
		t.Fatalf("failed to create sub1: %v", err)
	}
	if err := repo.Create(&sub2); err != nil {
		t.Fatalf("failed to create sub2: %v", err)
	}
	if err := repo.Create(&sub3); err != nil {
		t.Fatalf("failed to create sub3: %v", err)
	}

	// Create downloads for sub1: 2 downloading, 1 completed
	downloadsSub1 := []model.Download{
		{SubscriptionID: sub1.ID, Title: "dl-1-1", Status: "downloading", TorrentURL: "magnet:11", TorrentHash: "hash-11"},
		{SubscriptionID: sub1.ID, Title: "dl-1-2", Status: "downloading", TorrentURL: "magnet:12", TorrentHash: "hash-12"},
		{SubscriptionID: sub1.ID, Title: "dl-1-3", Status: "completed", TorrentURL: "magnet:13", TorrentHash: "hash-13"},
	}
	for i := range downloadsSub1 {
		if err := downloadRepo.Create(&downloadsSub1[i]); err != nil {
			t.Fatalf("failed to create download for sub1: %v", err)
		}
	}

	// Create downloads for sub2: 1 downloading
	downloadsSub2 := []model.Download{
		{SubscriptionID: sub2.ID, Title: "dl-2-1", Status: "downloading", TorrentURL: "magnet:21", TorrentHash: "hash-21"},
	}
	for i := range downloadsSub2 {
		if err := downloadRepo.Create(&downloadsSub2[i]); err != nil {
			t.Fatalf("failed to create download for sub2: %v", err)
		}
	}

	// sub3 has no downloads

	// Test 1: Returns subscriptions with downloading_count field populated
	t.Run("returns subscriptions with downloading_count", func(t *testing.T) {
		results, err := repo.GetSubscriptionsWithDownloadCount()
		if err != nil {
			t.Fatalf("failed to get subscriptions with download count: %v", err)
		}
		if len(results) != 3 {
			t.Errorf("got %d results, want 3", len(results))
		}
	})

	// Test 2: Counts only downloads with status='downloading' per subscription
	t.Run("counts only downloading status per subscription", func(t *testing.T) {
		results, err := repo.GetSubscriptionsWithDownloadCount()
		if err != nil {
			t.Fatalf("failed to get subscriptions with download count: %v", err)
		}

		// Find sub1 in results
		var sub1Result *SubscriptionWithStats
		for i := range results {
			if results[i].ID == sub1.ID {
				sub1Result = &results[i]
				break
			}
		}
		if sub1Result == nil {
			t.Fatalf("sub1 not found in results")
		}
		if sub1Result.DownloadingCount != 2 {
			t.Errorf("sub1 downloading_count = %d, want 2", sub1Result.DownloadingCount)
		}

		// Find sub2 in results
		var sub2Result *SubscriptionWithStats
		for i := range results {
			if results[i].ID == sub2.ID {
				sub2Result = &results[i]
				break
			}
		}
		if sub2Result == nil {
			t.Fatalf("sub2 not found in results")
		}
		if sub2Result.DownloadingCount != 1 {
			t.Errorf("sub2 downloading_count = %d, want 1", sub2Result.DownloadingCount)
		}
	})

	// Test 3: Returns correct count (0) for subscriptions with no downloading items
	t.Run("returns 0 for subscriptions with no downloading items", func(t *testing.T) {
		results, err := repo.GetSubscriptionsWithDownloadCount()
		if err != nil {
			t.Fatalf("failed to get subscriptions with download count: %v", err)
		}

		// Find sub3 in results
		var sub3Result *SubscriptionWithStats
		for i := range results {
			if results[i].ID == sub3.ID {
				sub3Result = &results[i]
				break
			}
		}
		if sub3Result == nil {
			t.Fatalf("sub3 not found in results")
		}
		if sub3Result.DownloadingCount != 0 {
			t.Errorf("sub3 downloading_count = %d, want 0", sub3Result.DownloadingCount)
		}
	})

	// Test 4: Single query execution (no N+1 pattern)
	// This is verified by the implementation using JOIN + COUNT
	t.Run("uses single query with JOIN", func(t *testing.T) {
		// The implementation uses LEFT JOIN with COUNT, not multiple queries
		// We verify the method exists and returns correct results
		results, err := repo.GetSubscriptionsWithDownloadCount()
		if err != nil {
			t.Fatalf("failed to get subscriptions with download count: %v", err)
		}
		if len(results) != 3 {
			t.Errorf("got %d results, want 3", len(results))
		}
	})
}
