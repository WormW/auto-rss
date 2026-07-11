package repository

import (
	"fmt"
	"strings"
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
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
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
