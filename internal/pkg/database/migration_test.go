package database

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

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

func TestRunMigrationsAddsDiskCleanupFailureColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations should be repeatable: %v", err)
	}

	if !db.Migrator().HasColumn(&model.DiskCleanupRecord{}, "failed_count") {
		t.Fatalf("expected disk cleanup failed_count column")
	}
	if !db.Migrator().HasColumn(&model.DiskCleanupRecord{}, "failed_paths") {
		t.Fatalf("expected disk cleanup failed_paths column")
	}
}

func TestRunMigrationsBackfillsEpisodeLedgerWithoutInferringGaps(t *testing.T) {
	db := openMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	sub := model.Subscription{Name: "Offset Anime", RssURL: "https://old.test/rss", EpisodeOffset: 170, CurrentEpisode: 173}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, db.Create(&model.Download{
		SubscriptionID: sub.ID,
		Episode:        171,
		Title:          "Offset Anime - 171",
		TorrentURL:     "https://old.test/171.torrent",
		TorrentHash:    "hash-171",
		Status:         model.DownloadStatusCompleted,
		RenamedPath:    "/media/Offset Anime S01E01.mkv",
	}).Error)
	require.NoError(t, db.Create(&model.Download{
		SubscriptionID: sub.ID,
		Episode:        173,
		Title:          "Offset Anime - 173",
		TorrentURL:     "https://old.test/173.torrent",
		TorrentHash:    "hash-173",
		Status:         model.DownloadStatusDownloading,
	}).Error)

	require.NoError(t, RunMigrations(db))

	var episodes []model.SubscriptionEpisode
	require.NoError(t, db.Order("episode").Find(&episodes).Error)
	require.Len(t, episodes, 2)
	assert.Equal(t, 1, episodes[0].Episode)
	assert.Equal(t, model.EpisodeStatusDownloaded, episodes[0].Status)
	assert.Equal(t, 3, episodes[1].Episode)
	assert.Equal(t, model.EpisodeStatusDownloading, episodes[1].Status)
}

func TestRunMigrationsMergesSameEpisodeResourcesDeterministically(t *testing.T) {
	db := openMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	sub := model.Subscription{Name: "Merge Anime", RssURL: "https://merge.test/rss"}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, db.Create(&model.Download{
		SubscriptionID: sub.ID,
		Episode:        1,
		Title:          "E01 pending",
		TorrentURL:     "https://merge.test/pending",
		TorrentHash:    "pending-hash",
		Status:         model.DownloadStatusDownloading,
	}).Error)
	completed := model.Download{
		SubscriptionID: sub.ID,
		Episode:        1,
		Title:          "E01 completed",
		TorrentURL:     "https://merge.test/completed",
		TorrentHash:    "completed-hash",
		Status:         model.DownloadStatusCompleted,
		RenamedPath:    "/media/E01.mkv",
	}
	require.NoError(t, db.Create(&completed).Error)

	require.NoError(t, RunMigrations(db))

	var episodes []model.SubscriptionEpisode
	require.NoError(t, db.Find(&episodes).Error)
	require.Len(t, episodes, 1)
	assert.Equal(t, model.EpisodeStatusDownloaded, episodes[0].Status)
	assert.Equal(t, completed.ID, *episodes[0].ActiveDownloadID)
	assert.Equal(t, "completed-hash", episodes[0].ActiveTorrentHash)
	assert.Equal(t, "https://merge.test/completed", episodes[0].ActiveTorrentURL)
}

func TestRunMigrationsCreatesMissingRowsWithoutMarkingCurrentRangeDownloaded(t *testing.T) {
	db := openMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	sub := model.Subscription{
		Name:           "Known Range",
		RssURL:         "https://range.test/rss",
		CurrentEpisode: 3,
		LatestEpisode:  3,
		TotalEpisodes:  3,
	}
	require.NoError(t, db.Create(&sub).Error)

	require.NoError(t, RunMigrations(db))

	var episodes []model.SubscriptionEpisode
	require.NoError(t, db.Order("episode").Find(&episodes).Error)
	require.Len(t, episodes, 3)
	for index, ledger := range episodes {
		assert.Equal(t, index+1, ledger.Episode)
		assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
	}
}

func TestRunMigrationsEpisodeLedgerConstraintsAreIdempotent(t *testing.T) {
	db := openMigrationTestDB(t)
	require.NoError(t, RunMigrations(db))
	require.NoError(t, RunMigrations(db))

	first := model.SubscriptionEpisode{SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusMissing, StatusSource: "test"}
	require.NoError(t, db.Create(&first).Error)
	duplicate := model.SubscriptionEpisode{SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusMissing, StatusSource: "test"}
	require.Error(t, db.Create(&duplicate).Error)
}

func TestRunMigrationsMarksExistingRSSSubscriptionsForSafeBaseline(t *testing.T) {
	db := openMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	sub := model.Subscription{Name: "Existing", RssURL: "https://existing.test/rss"}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, RunMigrations(db))
	require.NoError(t, db.First(&sub, sub.ID).Error)
	assert.True(t, sub.RSSBaselinePending)
}
