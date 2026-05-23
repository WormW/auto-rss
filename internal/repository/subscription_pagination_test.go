package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionRepository_List_PaginationLimits(t *testing.T) {
	// Setup in-memory test DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}
	repo := NewSubscriptionRepository(db)

	// Seed test data with 25 subscriptions
	for i := 1; i <= 25; i++ {
		sub := model.Subscription{
			Name:   "subscription-" + string(rune('A'+i-1)),
			RssURL: "https://example.com/rss" + string(rune('0'+i)),
		}
		if err := repo.Create(&sub); err != nil {
			t.Fatalf("failed to create seed subscription %d: %v", i, err)
		}
	}

	// Test 1: List with limit=0 uses DefaultPageSize (20)
	t.Run("limit=0 uses DefaultPageSize", func(t *testing.T) {
		results, total, err := repo.List(0, 0)
		if err != nil {
			t.Fatalf("failed to list with limit=0: %v", err)
		}
		if total != 25 {
			t.Errorf("total = %d, want 25", total)
		}
		if len(results) != DefaultPageSize {
			t.Errorf("got %d results, want DefaultPageSize (%d)", len(results), DefaultPageSize)
		}
	})

	// Test 2: List with limit=5000 is capped to MaxPageSize (1000)
	t.Run("limit>MaxPageSize is capped to MaxPageSize", func(t *testing.T) {
		results, total, err := repo.List(0, 5000)
		if err != nil {
			t.Fatalf("failed to list with large limit: %v", err)
		}
		if total != 25 {
			t.Errorf("total = %d, want 25", total)
		}
		// Should return all 25 since it's less than MaxPageSize
		if len(results) != 25 {
			t.Errorf("got %d results, want 25", len(results))
		}
	})

	// Test 3: List with offset=-10 uses 0
	t.Run("negative offset uses 0", func(t *testing.T) {
		results, total, err := repo.List(-10, 5)
		if err != nil {
			t.Fatalf("failed to list with negative offset: %v", err)
		}
		if total != 25 {
			t.Errorf("total = %d, want 25", total)
		}
		if len(results) != 5 {
			t.Errorf("got %d results, want 5", len(results))
		}
	})

	// Test 4: List with negative limit uses DefaultPageSize
	t.Run("negative limit uses DefaultPageSize", func(t *testing.T) {
		results, total, err := repo.List(0, -5)
		if err != nil {
			t.Fatalf("failed to list with negative limit: %v", err)
		}
		if total != 25 {
			t.Errorf("total = %d, want 25", total)
		}
		if len(results) != DefaultPageSize {
			t.Errorf("got %d results, want DefaultPageSize (%d)", len(results), DefaultPageSize)
		}
	})

	// Test 5: Verify MaxPageSize constant exists
	t.Run("MaxPageSize constant is 1000", func(t *testing.T) {
		if MaxPageSize != 1000 {
			t.Errorf("MaxPageSize = %d, want 1000", MaxPageSize)
		}
	})

	// Test 6: Verify DefaultPageSize constant exists
	t.Run("DefaultPageSize constant is 20", func(t *testing.T) {
		if DefaultPageSize != 20 {
			t.Errorf("DefaultPageSize = %d, want 20", DefaultPageSize)
		}
	})

	// Test 7: Normal pagination works correctly
	t.Run("normal pagination works", func(t *testing.T) {
		results, total, err := repo.List(5, 10)
		if err != nil {
			t.Fatalf("failed to list with offset=5 limit=10: %v", err)
		}
		if total != 25 {
			t.Errorf("total = %d, want 25", total)
		}
		if len(results) != 10 {
			t.Errorf("got %d results, want 10", len(results))
		}
	})
}
