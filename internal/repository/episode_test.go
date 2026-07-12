package repository

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupEpisodeRepository(t *testing.T) (*gorm.DB, EpisodeRepository) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "episode.db")
	db, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.SubscriptionFeed{},
		&model.SubscriptionFeedSeenItem{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	return db, NewEpisodeRepository(db)
}

func TestEpisodeRepositoryClaimForDownloadIsUnique(t *testing.T) {
	db, repo := setupEpisodeRepository(t)

	first, claimed, err := repo.ClaimForDownload(1, 2, model.EpisodeResource{Hash: "A", URL: "https://x/a", Title: "A"})
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, model.EpisodeStatusDownloading, first.Status)

	second, claimed, err := repo.ClaimForDownload(1, 2, model.EpisodeResource{Hash: "B", URL: "https://x/b", Title: "B"})
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "A", second.ActiveTorrentHash)

	var count int64
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).
		Where("subscription_id = ? AND episode = ? AND status = ?", 1, 2, model.EpisodeStatusDownloading).
		Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestEpisodeRepositoryReleaseDownloadClaimOnlyReleasesUnattachedMatchingClaim(t *testing.T) {
	_, repo := setupEpisodeRepository(t)
	resourceA := model.EpisodeResource{Hash: "ABC", URL: "https://x/a", Title: "A"}
	episode, claimed, err := repo.ClaimForDownload(1, 1, resourceA)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.ReleaseDownloadClaim(episode.ID, resourceA))
	released, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, released.Status)
	assert.Nil(t, released.ActiveDownloadID)
	assert.Empty(t, released.ActiveTorrentHash)
	assert.Empty(t, released.ActiveTorrentURL)

	resourceB := model.EpisodeResource{Hash: "DEF", URL: "https://x/b", Title: "B"}
	episode, claimed, err = repo.ClaimForDownload(1, 1, resourceB)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.ReleaseDownloadClaim(episode.ID, resourceA))
	stillClaimed, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, stillClaimed.Status)
	assert.Equal(t, "DEF", stillClaimed.ActiveTorrentHash)

	require.NoError(t, repo.AttachDownload(episode.ID, 99))
	require.NoError(t, repo.ReleaseDownloadClaim(episode.ID, resourceB))
	attached, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	require.NotNil(t, attached.ActiveDownloadID)
	assert.EqualValues(t, 99, *attached.ActiveDownloadID)
	assert.Equal(t, model.EpisodeStatusDownloading, attached.Status)
}

func TestEpisodeRepositoryAttachDownloadInTxUsesCallerTransaction(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	episode, claimed, err := repo.ClaimForDownload(1, 1, model.EpisodeResource{Hash: "abc"})
	require.NoError(t, err)
	require.True(t, claimed)

	err = db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, repo.AttachDownloadInTx(tx, episode.ID, 42))
		return assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)

	got, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Nil(t, got.ActiveDownloadID)
}

func TestEpisodeRepositoryDetachDownloadedKeepsResourceIdentity(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	downloadID := uint(7)
	episode := model.SubscriptionEpisode{
		SubscriptionID:    1,
		Episode:           3,
		Status:            model.EpisodeStatusDownloaded,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &downloadID,
		ActiveTorrentHash: "abc",
		ActiveTorrentURL:  "https://x/3",
		ActiveTitle:       "episode 3",
	}
	require.NoError(t, db.Create(&episode).Error)

	require.NoError(t, repo.DetachDownload(downloadID))

	got, err := repo.GetBySubscriptionAndEpisode(1, 3)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, got.Status)
	assert.Nil(t, got.ActiveDownloadID)
	assert.Equal(t, "abc", got.ActiveTorrentHash)
	assert.Equal(t, "https://x/3", got.ActiveTorrentURL)
	assert.Equal(t, "episode 3", got.ActiveTitle)
}

func TestEpisodeRepositoryStateTransitionsKeepActiveFieldsConsistent(t *testing.T) {
	_, repo := setupEpisodeRepository(t)
	episode, claimed, err := repo.ClaimForDownload(1, 1, model.EpisodeResource{Hash: "abc", URL: "https://x/1", Title: "one"})
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, repo.AttachDownload(episode.ID, 12))

	require.NoError(t, repo.MarkMissingIfActiveDownload(12))
	missing, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, missing.Status)
	assert.Nil(t, missing.ActiveDownloadID)
	assert.Empty(t, missing.ActiveTorrentHash)

	episode, claimed, err = repo.ClaimForDownload(1, 1, model.EpisodeResource{Hash: "retry"})
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, repo.AttachDownload(episode.ID, 12))

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.MarkDownloaded(episode.ID, 12, model.EpisodeResource{
		Hash: "completed-hash", URL: "https://x/completed", Title: "completed title",
	}, now))
	downloaded, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, downloaded.Status)
	assert.NotNil(t, downloaded.DownloadedAt)
	assert.Equal(t, "completed-hash", downloaded.ActiveTorrentHash)
	assert.Equal(t, "https://x/completed", downloaded.ActiveTorrentURL)
	assert.Equal(t, "completed title", downloaded.ActiveTitle)

	require.NoError(t, repo.SetStatus(1, []int{1}, model.EpisodeStatusIgnored, model.EpisodeStatusSourceUser))
	ignored, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusIgnored, ignored.Status)
	assert.Nil(t, ignored.ActiveDownloadID)
}

func TestEpisodeRepositoryMarkMissingDoesNotDowngradeDownloadedEpisode(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	downloadID := uint(12)
	episode := model.SubscriptionEpisode{
		SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &downloadID,
		ActiveTorrentHash: "abc",
	}
	require.NoError(t, db.Create(&episode).Error)

	require.NoError(t, repo.MarkMissingIfActiveDownload(downloadID))
	got, err := repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, got.Status)
	assert.NotNil(t, got.ActiveDownloadID)
	assert.Equal(t, "abc", got.ActiveTorrentHash)
}

func TestEpisodeRepositoryMarkDownloadedIsIdempotentOnlyForSameOwnerAndResource(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	downloadID := uint(12)
	resource := model.EpisodeResource{Hash: "same-hash", URL: "magnet:same", Title: "same title"}
	episode := model.SubscriptionEpisode{
		SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &downloadID,
		ActiveTorrentHash: resource.Hash, ActiveTorrentURL: resource.URL, ActiveTitle: resource.Title,
	}
	require.NoError(t, db.Create(&episode).Error)

	require.NoError(t, repo.MarkDownloaded(episode.ID, downloadID, resource, time.Now()))
	require.Error(t, repo.MarkDownloaded(episode.ID, downloadID+1, resource, time.Now()))
	require.Error(t, repo.MarkDownloaded(episode.ID, downloadID, model.EpisodeResource{
		Hash: "different-hash", URL: resource.URL, Title: resource.Title,
	}, time.Now()))
}

func TestEpisodeRepositoryUpsertCandidateKeepsWorkflowStatus(t *testing.T) {
	_, repo := setupEpisodeRepository(t)
	episode, err := repo.ObserveEpisode(1, 1)
	require.NoError(t, err)

	candidate := &model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: episode.ID,
		ResourceKey:           "hash:abc",
		TorrentHash:           "abc",
		Status:                model.CandidateStatusKeptExisting,
	}
	persisted, created, err := repo.UpsertCandidate(episode.ID, candidate)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, episode.ID, persisted.SubscriptionEpisodeID)
	createdCandidateID := persisted.ID

	retry := &model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: 999,
		ResourceKey:           "hash:abc",
		TorrentHash:           "ABC",
		Title:                 "new title",
		Status:                model.CandidateStatusPending,
	}
	retryBefore := *retry
	persisted, created, err = repo.UpsertCandidate(episode.ID, retry)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, createdCandidateID, persisted.ID)
	assert.Equal(t, retryBefore, *retry)

	candidates, err := repo.ListCandidates(episode.ID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, model.CandidateStatusKeptExisting, candidates[0].Status)

	_, _, err = repo.UpsertCandidate(episode.ID, &model.EpisodeResourceCandidate{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource key")
}

func TestEpisodeRepositoryListWithCandidateCountsUsesOneConditionalJoin(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	episodes := []model.SubscriptionEpisode{
		{SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: "test"},
		{SubscriptionID: 1, Episode: 2, Status: model.EpisodeStatusMissing, StatusSource: "test"},
	}
	require.NoError(t, db.Create(&episodes).Error)
	statuses := []string{
		model.CandidateStatusPending,
		model.CandidateStatusFailed,
		model.CandidateStatusAcceptedCleanupFailed,
		model.CandidateStatusAccepted,
		model.CandidateStatusKeptExisting,
	}
	for i, status := range statuses {
		require.NoError(t, db.Create(&model.EpisodeResourceCandidate{
			SubscriptionEpisodeID: episodes[0].ID,
			ResourceKey:           fmt.Sprintf("hash:%d", i),
			Status:                status,
		}).Error)
	}

	queryCount := 0
	callbackName := "test:count_episode_list_queries"
	countQuery := func(*gorm.DB) {
		queryCount++
	}
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, countQuery))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register(callbackName, countQuery))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
		_ = db.Callback().Row().Remove(callbackName)
	})

	got, err := repo.ListWithCandidateCounts(1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.EqualValues(t, 3, got[0].ActionRequiredCandidateCount)
	assert.EqualValues(t, 0, got[1].ActionRequiredCandidateCount)
	assert.Equal(t, 1, queryCount)
}

func TestEpisodeRepositoryEnsureRangeAndRefreshProgress(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	sub := model.Subscription{ID: 1, Name: "offset", EpisodeOffset: 170, TotalEpisodes: 4}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, repo.EnsureRange(sub.ID, 4))

	for episodeNumber, status := range map[int]string{
		1: model.EpisodeStatusDownloaded,
		2: model.EpisodeStatusMarkedDownloaded,
		4: model.EpisodeStatusDownloaded,
	} {
		require.NoError(t, repo.SetStatus(sub.ID, []int{episodeNumber}, status, model.EpisodeStatusSourceUser))
	}
	require.NoError(t, repo.EnsureRange(sub.ID, 2))

	require.NoError(t, repo.RefreshSubscriptionProgress(sub.ID))
	require.NoError(t, db.First(&sub, sub.ID).Error)
	assert.Equal(t, 172, sub.CurrentEpisode)
	assert.Equal(t, 174, sub.LatestEpisode)
	assert.Nil(t, sub.CompletedAt)

	var count int64
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Where("subscription_id = ?", sub.ID).Count(&count).Error)
	assert.EqualValues(t, 4, count)
}

func TestEpisodeRepositoryRefreshProgressIgnoresUnobservedRangePlaceholders(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	sub := model.Subscription{ID: 1, Name: "airing", TotalEpisodes: 12, EpisodeOffset: 100}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, repo.EnsureRange(sub.ID, sub.TotalEpisodes))

	feed := model.SubscriptionFeed{
		SubscriptionID:   sub.ID,
		Name:             "default",
		RSSURL:           "https://example.test/feed",
		RSSURLNormalized: "https://example.test/feed",
		EpisodeOffset:    100,
		Enabled:          true,
	}
	require.NoError(t, db.Create(&feed).Error)
	require.NoError(t, db.Create(&model.SubscriptionFeedSeenItem{
		SubscriptionFeedID: feed.ID,
		ResourceKey:        "hash:episode-3",
		OriginalEpisode:    103,
		FirstSeenAt:        time.Now(),
	}).Error)

	require.NoError(t, repo.RefreshSubscriptionProgress(sub.ID))
	require.NoError(t, db.First(&sub, sub.ID).Error)
	assert.Zero(t, sub.CurrentEpisode)
	assert.Equal(t, 103, sub.LatestEpisode)
	assert.Equal(t, 3, sub.RelativeLatestEpisode())
}

func TestEpisodeRepositoryRefreshProgressPreservesBangumiLatestEpisode(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	sub := model.Subscription{
		ID:                   1,
		Name:                 "airing",
		TotalEpisodes:        12,
		EpisodeOffset:        100,
		BangumiLatestEpisode: 4,
	}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, repo.EnsureRange(sub.ID, sub.TotalEpisodes))

	feed := model.SubscriptionFeed{
		SubscriptionID:   sub.ID,
		Name:             "default",
		RSSURL:           "https://example.test/feed",
		RSSURLNormalized: "https://example.test/feed",
		EpisodeOffset:    100,
		Enabled:          true,
	}
	require.NoError(t, db.Create(&feed).Error)
	require.NoError(t, db.Create(&model.SubscriptionFeedSeenItem{
		SubscriptionFeedID: feed.ID,
		ResourceKey:        "hash:episode-3",
		OriginalEpisode:    103,
		FirstSeenAt:        time.Now(),
	}).Error)

	require.NoError(t, repo.RefreshSubscriptionProgress(sub.ID))
	require.NoError(t, db.First(&sub, sub.ID).Error)
	assert.Equal(t, 104, sub.LatestEpisode)
	assert.Equal(t, 4, sub.RelativeLatestEpisode())
}

func TestEpisodeRepositoryEnsureRangeRejectsExcessiveTotal(t *testing.T) {
	_, repo := setupEpisodeRepository(t)
	err := repo.EnsureRange(1, 10001)
	require.ErrorContains(t, err, "10000")
}

func TestEpisodeRepositorySetUserStatusChunksMaximumEpisodeRangeAtomically(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	sub := model.Subscription{Name: "large", TotalEpisodes: model.MaxSubscriptionEpisodes}
	require.NoError(t, db.Create(&sub).Error)
	episodes := make([]int, model.MaxSubscriptionEpisodes)
	for index := range episodes {
		episodes[index] = index + 1
	}

	download := model.Download{
		SubscriptionID: sub.ID, Episode: model.MaxSubscriptionEpisodes - 1,
		Title: "active", TorrentURL: "magnet:active-large-batch", TorrentHash: "active-large-batch",
		Status: model.DownloadStatusDownloading,
	}
	require.NoError(t, db.Create(&download).Error)
	active := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: model.MaxSubscriptionEpisodes - 1,
		Status: model.EpisodeStatusDownloading, StatusSource: model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID: &download.ID,
	}
	require.NoError(t, db.Create(&active).Error)

	err := repo.SetUserStatus(sub.ID, episodes, model.EpisodeStatusMissing)
	require.ErrorIs(t, err, ErrActiveDownloadMustBeResolved)
	var count int64
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Where("subscription_id = ?", sub.ID).Count(&count).Error)
	assert.EqualValues(t, 1, count, "conflict must roll back every inserted chunk")

	require.NoError(t, repo.SetUserStatus(sub.ID, episodes, model.EpisodeStatusIgnored))
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).
		Where("subscription_id = ? AND status = ?", sub.ID, model.EpisodeStatusIgnored).
		Count(&count).Error)
	assert.EqualValues(t, model.MaxSubscriptionEpisodes, count)
}

func TestEpisodeRepositoryKeepCandidateConcurrentWALRequestsAreIdempotent(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "episode-keep.db")
	dsn := databasePath + "?_journal_mode=WAL&_busy_timeout=5000"
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	}
	dbOne := open()
	dbTwo := open()
	require.NoError(t, dbOne.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	sub := model.Subscription{Name: "concurrent"}
	require.NoError(t, dbOne.Create(&sub).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, dbOne.Create(&ledger).Error)
	candidate := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: ledger.ID, ResourceKey: "hash:concurrent", Status: model.CandidateStatusPending,
	}
	require.NoError(t, dbOne.Create(&candidate).Error)

	reachedUpdate := make(chan struct{}, 2)
	releaseUpdates := make(chan struct{})
	registerBarrier := func(db *gorm.DB, name string) {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(name, func(*gorm.DB) {
			reachedUpdate <- struct{}{}
			<-releaseUpdates
		}))
	}
	registerBarrier(dbOne, "test:keep_candidate_barrier_one")
	registerBarrier(dbTwo, "test:keep_candidate_barrier_two")

	repositories := []EpisodeRepository{NewEpisodeRepository(dbOne), NewEpisodeRepository(dbTwo)}
	errorsByCall := make([]error, len(repositories))
	var waitGroup sync.WaitGroup
	for index, repo := range repositories {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, errorsByCall[index] = repo.KeepCandidate(sub.ID, ledger.Episode, candidate.ID)
		}()
	}
	for range repositories {
		select {
		case <-reachedUpdate:
		case <-time.After(5 * time.Second):
			t.Fatal("both keep requests did not reach the conditional update")
		}
	}
	close(releaseUpdates)
	waitGroup.Wait()
	for _, err := range errorsByCall {
		require.NoError(t, err)
	}

	var persisted model.EpisodeResourceCandidate
	require.NoError(t, dbOne.First(&persisted, candidate.ID).Error)
	assert.Equal(t, model.CandidateStatusKeptExisting, persisted.Status)
}

func TestEpisodeRepositoryEnsureRangeOnlyInsertsMissingEpisodes(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	existing := make([]model.SubscriptionEpisode, 0, 9)
	for episodeNumber := 1; episodeNumber < 10; episodeNumber++ {
		existing = append(existing, model.SubscriptionEpisode{
			SubscriptionID: 1,
			Episode:        episodeNumber,
			Status:         model.EpisodeStatusMissing,
			StatusSource:   model.EpisodeStatusSourceAutomatic,
		})
	}
	require.NoError(t, db.Create(&existing).Error)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_existing_episode_reinsert
		BEFORE INSERT ON subscription_episodes
		WHEN NEW.episode < 10
		BEGIN SELECT RAISE(ABORT, 'existing episode reinserted'); END;
	`).Error)

	require.NoError(t, repo.EnsureRange(1, 10))

	var episodes []model.SubscriptionEpisode
	require.NoError(t, db.Where("subscription_id = ?", 1).Order("episode").Find(&episodes).Error)
	require.Len(t, episodes, 10)
	assert.Equal(t, 10, episodes[9].Episode)
}

func TestEpisodeRepositoryRefreshProgressDoesNotOverwriteConcurrentSubscriptionFields(t *testing.T) {
	db, repo := setupEpisodeRepository(t)
	sub := model.Subscription{
		ID:             1,
		Name:           "offset",
		RssURL:         "https://old.example/rss",
		FilterKeywords: `["old"]`,
		EpisodeOffset:  170,
		TotalEpisodes:  2,
	}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, db.Create(&[]model.SubscriptionEpisode{
		{SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: "test"},
		{SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusIgnored, StatusSource: "test"},
		{SubscriptionID: sub.ID, Episode: 4, Status: model.EpisodeStatusDownloaded, StatusSource: "test"},
	}).Error)

	callbackName := "test:concurrent_subscription_update"
	callbackRan := false
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if callbackRan || tx.Statement.Table != (model.Subscription{}).TableName() {
			return
		}
		callbackRan = true
		err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec(
			"UPDATE subscriptions SET rss_url = ?, filter_keywords = ? WHERE id = ?",
			"https://new.example/rss",
			`["new"]`,
			sub.ID,
		).Error
		tx.AddError(err)
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	require.NoError(t, repo.RefreshSubscriptionProgress(sub.ID))
	require.True(t, callbackRan)

	var refreshed model.Subscription
	require.NoError(t, db.First(&refreshed, sub.ID).Error)
	assert.Equal(t, "https://new.example/rss", refreshed.RssURL)
	assert.Equal(t, `["new"]`, refreshed.FilterKeywords)
	assert.Equal(t, 172, refreshed.CurrentEpisode)
	assert.Equal(t, 174, refreshed.LatestEpisode)
	assert.NotNil(t, refreshed.CompletedAt)
}
