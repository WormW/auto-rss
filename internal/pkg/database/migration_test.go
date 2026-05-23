package database

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateRSSTimeout(t *testing.T) {
	// Create in-memory database for testing
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Migrate the schema
	if err := db.AutoMigrate(&model.RSSSource{}); err != nil {
		t.Fatalf("Failed to migrate schema: %v", err)
	}

	// Create test sources with different timeout values
	sources := []model.RSSSource{
		{Name: "Source with zero timeout", BaseURL: "https://example.com/1", Timeout: 0},
		{Name: "Source with valid timeout", BaseURL: "https://example.com/2", Timeout: 45 * time.Second},
		{Name: "Source with another valid timeout", BaseURL: "https://example.com/3", Timeout: 60 * time.Second},
	}

	for i := range sources {
		if err := db.Create(&sources[i]).Error; err != nil {
			t.Fatalf("Failed to create test source: %v", err)
		}
	}

	// Run the migration
	if err := MigrateRSSTimeout(db); err != nil {
		t.Fatalf("MigrateRSSTimeout failed: %v", err)
	}

	// Verify results
	var results []model.RSSSource
	if err := db.Find(&results).Error; err != nil {
		t.Fatalf("Failed to fetch results: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 sources, got %d", len(results))
	}

	for _, source := range results {
		switch source.Name {
		case "Source with zero timeout":
			if source.Timeout != 30*time.Second {
				t.Errorf("Expected timeout 30s for zero-timeout source, got %v", source.Timeout)
			}
		case "Source with valid timeout":
			if source.Timeout != 45*time.Second {
				t.Errorf("Expected timeout 45s for valid source, got %v", source.Timeout)
			}
		case "Source with another valid timeout":
			if source.Timeout != 60*time.Second {
				t.Errorf("Expected timeout 60s for another valid source, got %v", source.Timeout)
			}
		}
	}
}

func TestMigrateRSSTimeout_Idempotent(t *testing.T) {
	// Create in-memory database for testing
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Migrate the schema
	if err := db.AutoMigrate(&model.RSSSource{}); err != nil {
		t.Fatalf("Failed to migrate schema: %v", err)
	}

	// Create a source with zero timeout
	source := model.RSSSource{Name: "Test Source", BaseURL: "https://example.com", Timeout: 0}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("Failed to create test source: %v", err)
	}

	// Run the migration twice
	if err := MigrateRSSTimeout(db); err != nil {
		t.Fatalf("First MigrateRSSTimeout failed: %v", err)
	}
	if err := MigrateRSSTimeout(db); err != nil {
		t.Fatalf("Second MigrateRSSTimeout failed: %v", err)
	}

	// Verify the timeout is still 30s (not 60s)
	var result model.RSSSource
	if err := db.First(&result, source.ID).Error; err != nil {
		t.Fatalf("Failed to fetch result: %v", err)
	}

	if result.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s after idempotent migration, got %v", result.Timeout)
	}
}
