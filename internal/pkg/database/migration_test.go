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

func TestRunMigrationsAddsReplacementRecoverySnapshotColumnsToExistingLedger(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.Exec(`CREATE TABLE episode_resource_candidates (
		id integer PRIMARY KEY AUTOINCREMENT,
		subscription_episode_id integer NOT NULL,
		resource_key text,
		status text,
		replacement_stage text
	)`).Error; err != nil {
		t.Fatalf("create legacy candidate table: %v", err)
	}
	if err := db.AutoMigrate(&MigrationRecord{}); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	for _, id := range []string{
		"202504090001", "202504090002", "202504230001", "202606120001",
		"202606150001", "202606150002", "202606180001", "202607110001",
	} {
		if err := db.Create(&MigrationRecord{ID: id}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", id, err)
		}
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	if !db.Migrator().HasColumn(&model.EpisodeResourceCandidate{}, "old_download_id") {
		t.Fatal("expected old_download_id recovery snapshot column")
	}
	if !db.Migrator().HasColumn(&model.EpisodeResourceCandidate{}, "old_torrent_hash") {
		t.Fatal("expected old_torrent_hash recovery snapshot column")
	}
	if !db.Migrator().HasIndex(&model.EpisodeResourceCandidate{}, "idx_episode_candidate_single_replacing") {
		t.Fatal("expected unique replacing candidate index")
	}
	if !db.Migrator().HasColumn(&model.Download{}, "replacement_torrent_owned") {
		t.Fatal("expected replacement_torrent_owned ownership marker")
	}
}

func TestRunMigrationsAddsReplacementOwnershipAfterRecoveryMigrationApplied(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &MigrationRecord{}); err != nil {
		t.Fatalf("create pre-ownership schema: %v", err)
	}
	if err := db.Migrator().DropColumn(&model.Download{}, "replacement_torrent_owned"); err != nil {
		t.Fatalf("drop ownership column to simulate 120001 schema: %v", err)
	}
	for _, id := range []string{
		"202504090001", "202504090002", "202504230001", "202606120001",
		"202606150001", "202606150002", "202606180001", "202607110001", "202607120001",
	} {
		if err := db.Create(&MigrationRecord{ID: id}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", id, err)
		}
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	if !db.Migrator().HasColumn(&model.Download{}, "replacement_torrent_owned") {
		t.Fatal("expected replacement_torrent_owned from a migration after 120001")
	}
	version, err := GetCurrentVersion(db)
	if err != nil {
		t.Fatalf("GetCurrentVersion failed: %v", err)
	}
	if version != "202607130001" {
		t.Fatalf("migration version = %q, want 202607130001", version)
	}
}

func TestRunMigrationsBackfillsEpisodeLedgerWithoutInferringGaps(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	subscription := model.Subscription{
		Name:           "Offset Show",
		EpisodeOffset:  170,
		CurrentEpisode: 173,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}
	downloadedAt := time.Now().Add(-time.Hour)
	downloads := []model.Download{
		{
			SubscriptionID: subscription.ID,
			Title:          "Offset Show 171",
			Episode:        171,
			TorrentURL:     "https://example.test/offset-171.torrent",
			TorrentHash:    "offset-171",
			RenamedPath:    "/library/Offset Show/Offset Show S01E01.mkv",
			Status:         model.DownloadStatusCompleted,
			DownloadedAt:   &downloadedAt,
		},
		{
			SubscriptionID: subscription.ID,
			Title:          "Offset Show 173",
			Episode:        173,
			TorrentURL:     "https://example.test/offset-173.torrent",
			TorrentHash:    "offset-173",
			Status:         model.DownloadStatusDownloading,
		},
	}
	if err := db.Create(&downloads).Error; err != nil {
		t.Fatalf("Failed to create downloads: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var episodes []model.SubscriptionEpisode
	if err := db.Order("episode ASC").Find(&episodes).Error; err != nil {
		t.Fatalf("Failed to load episode ledger: %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("expected only downloaded/active episodes, got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].Episode != 1 || episodes[0].Status != model.EpisodeStatusDownloaded {
		t.Fatalf("episode 1 = (%d, %q), want (1, %q)", episodes[0].Episode, episodes[0].Status, model.EpisodeStatusDownloaded)
	}
	if episodes[1].Episode != 3 || episodes[1].Status != model.EpisodeStatusDownloading {
		t.Fatalf("episode 3 = (%d, %q), want (3, %q)", episodes[1].Episode, episodes[1].Status, model.EpisodeStatusDownloading)
	}
}

func TestRunMigrationsMergesSameEpisodeResourcesDeterministically(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	subscription := model.Subscription{Name: "Duplicate Resources"}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}
	downloads := []model.Download{
		{
			SubscriptionID: subscription.ID,
			Title:          "Downloading resource",
			Episode:        1,
			TorrentURL:     "https://example.test/downloading.torrent",
			TorrentHash:    "downloading-resource",
			Status:         model.DownloadStatusDownloading,
		},
		{
			SubscriptionID: subscription.ID,
			Title:          "Completed resource",
			Episode:        1,
			TorrentURL:     "https://example.test/completed.torrent",
			TorrentHash:    "completed-resource",
			RenamedPath:    "/library/Duplicate Resources/Duplicate Resources S01E01.mkv",
			Status:         model.DownloadStatusCompleted,
		},
	}
	if err := db.Create(&downloads).Error; err != nil {
		t.Fatalf("Failed to create downloads: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var episodes []model.SubscriptionEpisode
	if err := db.Find(&episodes).Error; err != nil {
		t.Fatalf("Failed to load episode ledger: %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected one merged episode, got %d", len(episodes))
	}
	episode := episodes[0]
	if episode.Status != model.EpisodeStatusDownloaded {
		t.Fatalf("status = %q, want %q", episode.Status, model.EpisodeStatusDownloaded)
	}
	if episode.ActiveDownloadID == nil || *episode.ActiveDownloadID != downloads[1].ID {
		t.Fatalf("active download = %v, want %d", episode.ActiveDownloadID, downloads[1].ID)
	}
	if episode.ActiveTorrentHash != downloads[1].TorrentHash ||
		episode.ActiveTorrentURL != downloads[1].TorrentURL ||
		episode.ActiveTitle != downloads[1].Title {
		t.Fatalf("active resource = (%q, %q, %q), want completed resource", episode.ActiveTorrentHash, episode.ActiveTorrentURL, episode.ActiveTitle)
	}
}

func TestRunMigrationsCreatesMissingRowsWithoutMarkingCurrentRangeDownloaded(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	subscription := model.Subscription{
		Name:           "Known Range",
		TotalEpisodes:  3,
		LatestEpisode:  3,
		CurrentEpisode: 3,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var episodes []model.SubscriptionEpisode
	if err := db.Order("episode ASC").Find(&episodes).Error; err != nil {
		t.Fatalf("Failed to load episode ledger: %v", err)
	}
	if len(episodes) != 3 {
		t.Fatalf("expected 3 missing episodes, got %d", len(episodes))
	}
	for i, episode := range episodes {
		wantEpisode := i + 1
		if episode.Episode != wantEpisode || episode.Status != model.EpisodeStatusMissing {
			t.Fatalf("episode[%d] = (%d, %q), want (%d, %q)", i, episode.Episode, episode.Status, wantEpisode, model.EpisodeStatusMissing)
		}
	}
}

func TestRunMigrationsEpisodeLedgerConstraintsAreIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first RunMigrations failed: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second RunMigrations failed: %v", err)
	}

	first := model.SubscriptionEpisode{
		SubscriptionID: 1,
		Episode:        1,
		Status:         model.EpisodeStatusMissing,
		StatusSource:   model.EpisodeStatusSourceMigration,
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("Failed to create first episode: %v", err)
	}
	duplicate := first
	duplicate.ID = 0
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("expected duplicate subscription_id + episode insert to fail")
	}
}

func TestRunMigrationsMarksExistingRSSSubscriptionsForSafeBaseline(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	subscription := model.Subscription{
		Name:   "Existing RSS",
		RssURL: "https://example.test/feed.xml",
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var migrated model.Subscription
	if err := db.First(&migrated, subscription.ID).Error; err != nil {
		t.Fatalf("Failed to load subscription: %v", err)
	}
	if !migrated.RSSBaselinePending {
		t.Fatal("expected existing RSS subscription to require a safe baseline")
	}
}

func TestBackfillSubscriptionEpisodesFailedDownloadRemainsUnclaimed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	subscription := model.Subscription{Name: "Failed Download"}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}
	downloadedAt := time.Now().Add(-time.Hour)
	download := model.Download{
		SubscriptionID: subscription.ID,
		Title:          "Failed Download 1",
		Episode:        1,
		TorrentURL:     "https://example.test/failed.torrent",
		TorrentHash:    "failed-resource",
		Status:         model.DownloadStatusFailed,
		DownloadedAt:   &downloadedAt,
	}
	if err := db.Create(&download).Error; err != nil {
		t.Fatalf("Failed to create failed download: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var episode model.SubscriptionEpisode
	if err := db.Where("subscription_id = ? AND episode = ?", subscription.ID, 1).First(&episode).Error; err != nil {
		t.Fatalf("Failed to load episode ledger: %v", err)
	}
	if episode.Status != model.EpisodeStatusMissing {
		t.Fatalf("status = %q, want %q", episode.Status, model.EpisodeStatusMissing)
	}
	if episode.ActiveDownloadID != nil {
		t.Fatalf("active download = %v, want nil", episode.ActiveDownloadID)
	}
	if episode.ActiveTorrentHash != "" || episode.ActiveTorrentURL != "" || episode.ActiveTitle != "" {
		t.Fatalf("failed download remained active: hash=%q url=%q title=%q", episode.ActiveTorrentHash, episode.ActiveTorrentURL, episode.ActiveTitle)
	}
	if episode.DownloadedAt != nil {
		t.Fatalf("downloaded_at = %v, want nil", episode.DownloadedAt)
	}
}

func TestBackfillSubscriptionEpisodesRollsBackAllChangesOnFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
	); err != nil {
		t.Fatalf("Failed to migrate schema: %v", err)
	}

	subscription := model.Subscription{
		Name:   "Transactional Backfill",
		RssURL: "https://example.test/transactional.xml",
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}
	download := model.Download{
		SubscriptionID: subscription.ID,
		Title:          "Transactional Backfill 1",
		Episode:        1,
		TorrentURL:     "https://example.test/transactional-1.torrent",
		TorrentHash:    "transactional-1",
		Status:         model.DownloadStatusCompleted,
	}
	if err := db.Create(&download).Error; err != nil {
		t.Fatalf("Failed to create download: %v", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER fail_rss_baseline
		BEFORE UPDATE OF rss_baseline_pending ON subscriptions
		WHEN NEW.rss_baseline_pending = 1
		BEGIN
			SELECT RAISE(FAIL, 'forced baseline failure');
		END
	`).Error; err != nil {
		t.Fatalf("Failed to create failure trigger: %v", err)
	}

	if err := backfillSubscriptionEpisodes(db); err == nil {
		t.Fatal("expected backfill to fail while updating RSS baseline")
	}

	var episodeCount int64
	if err := db.Model(&model.SubscriptionEpisode{}).Count(&episodeCount).Error; err != nil {
		t.Fatalf("Failed to count episode ledger: %v", err)
	}
	if episodeCount != 0 {
		t.Fatalf("episode ledger count = %d, want 0 after rollback", episodeCount)
	}
	var reloaded model.Subscription
	if err := db.First(&reloaded, subscription.ID).Error; err != nil {
		t.Fatalf("Failed to reload subscription: %v", err)
	}
	if reloaded.RSSBaselinePending {
		t.Fatal("RSS baseline marker should be rolled back")
	}
}

func TestRunMigrationsBackfillsDefaultSubscriptionFeed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	waterline := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	subscription := model.Subscription{
		Name:               "Offset Anime",
		RssURL:             " https://EXAMPLE.test:443/feed?b=2&a=1 ",
		Fansub:             "Group A",
		EpisodeOffset:      100,
		LastRSSPubTime:     &waterline,
		RSSBaselinePending: true,
		Status:             "active",
		Enabled:            true,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var feeds []model.SubscriptionFeed
	if err := db.Find(&feeds).Error; err != nil {
		t.Fatalf("Failed to load subscription feeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("feed count = %d, want 1", len(feeds))
	}
	feed := feeds[0]
	if feed.SubscriptionID != subscription.ID || feed.Name != "Group A" || feed.Fansub != "Group A" {
		t.Fatalf("unexpected feed ownership: %#v", feed)
	}
	if feed.EpisodeOffset != 100 {
		t.Fatalf("episode offset = %d, want 100", feed.EpisodeOffset)
	}
	if feed.LastRSSPubTime == nil || !feed.LastRSSPubTime.Equal(waterline) {
		t.Fatalf("waterline = %v, want %v", feed.LastRSSPubTime, waterline)
	}
	if !feed.BaselinePending {
		t.Fatal("backfilled feed must inherit the subscription baseline marker")
	}
	if feed.RSSURLNormalized != "https://example.test/feed?a=1&b=2" {
		t.Fatalf("normalized URL = %q", feed.RSSURLNormalized)
	}
}

func TestRunMigrationsSubscriptionFeedBackfillIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}
	subscription := model.Subscription{Name: "Anime", RssURL: "https://example.test/rss", Enabled: true}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first RunMigrations failed: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second RunMigrations failed: %v", err)
	}

	var count int64
	if err := db.Model(&model.SubscriptionFeed{}).Where("subscription_id = ?", subscription.ID).Count(&count).Error; err != nil {
		t.Fatalf("Failed to count subscription feeds: %v", err)
	}
	if count != 1 {
		t.Fatalf("feed count = %d, want 1", count)
	}
}

func TestRunMigrationsAddsNullableFeedReferences(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	if !db.Migrator().HasColumn(&model.Download{}, "subscription_feed_id") {
		t.Fatal("downloads.subscription_feed_id is missing")
	}
	if !db.Migrator().HasColumn(&model.EpisodeResourceCandidate{}, "subscription_feed_id") {
		t.Fatal("episode_resource_candidates.subscription_feed_id is missing")
	}
	if !db.Migrator().HasColumn(&model.EpisodeResourceCandidate{}, "source_feed_name") {
		t.Fatal("episode_resource_candidates.source_feed_name is missing")
	}
	if !db.Migrator().HasTable(&model.SubscriptionFeedSeenItem{}) {
		t.Fatal("subscription_feed_seen_items table is missing")
	}
}

func TestRunMigrationsBackfillsRSSLatestEpisodeSeparatelyFromBangumi(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Subscription{},
		&model.SubscriptionFeed{},
		&model.SubscriptionFeedSeenItem{},
	); err != nil {
		t.Fatalf("Failed to migrate legacy schema: %v", err)
	}

	subscription := model.Subscription{
		Name:                 "Anime",
		RssURL:               "https://example.test/rss",
		EpisodeOffset:        100,
		TotalEpisodes:        12,
		LatestEpisode:        104,
		BangumiLatestEpisode: 4,
		Enabled:              true,
		Status:               "active",
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}
	feed := model.SubscriptionFeed{
		SubscriptionID:   subscription.ID,
		Name:             "default",
		RSSURL:           subscription.RssURL,
		RSSURLNormalized: subscription.RssURL,
		EpisodeOffset:    subscription.EpisodeOffset,
		Enabled:          true,
	}
	if err := db.Create(&feed).Error; err != nil {
		t.Fatalf("Failed to create feed: %v", err)
	}
	if err := db.Create(&model.SubscriptionFeedSeenItem{
		SubscriptionFeedID: feed.ID,
		ResourceKey:        "hash:rss-episode-3",
		OriginalEpisode:    103,
		FirstSeenAt:        time.Now(),
	}).Error; err != nil {
		t.Fatalf("Failed to create seen item: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	if err := db.First(&subscription, subscription.ID).Error; err != nil {
		t.Fatalf("Failed to reload subscription: %v", err)
	}
	if subscription.RSSLatestEpisode != 103 {
		t.Fatalf("rss latest episode = %d, want 103", subscription.RSSLatestEpisode)
	}
	if subscription.LatestEpisode != 104 {
		t.Fatalf("combined latest episode = %d, want 104", subscription.LatestEpisode)
	}
}
