package repository_test

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeleteFeedDetachesRuntimeReferencesAndKeepsCandidateSnapshot(t *testing.T) {
	repo, db := setupSubscriptionFeedRepository(t)
	feed := model.SubscriptionFeed{
		SubscriptionID:   1,
		Name:             "B",
		RSSURL:           "https://b.test/rss",
		RSSURLNormalized: "https://b.test/rss",
		Enabled:          true,
	}
	require.NoError(t, repo.Create(&feed))

	download := model.Download{
		SubscriptionID:     1,
		SubscriptionFeedID: &feed.ID,
		Title:              "B 101",
		TorrentURL:         "https://b.test/101",
		TorrentHash:        "b101",
	}
	require.NoError(t, db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: 1,
		Episode:        1,
		Status:         model.EpisodeStatusDownloaded,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, db.Create(&ledger).Error)
	candidate := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: ledger.ID,
		SubscriptionFeedID:    &feed.ID,
		SourceFeedName:        "B",
		ResourceKey:           "hash:b101",
		Status:                model.CandidateStatusPending,
	}
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, db.Create(&model.SubscriptionFeedSeenItem{
		SubscriptionFeedID: feed.ID,
		ResourceKey:        "hash:b101",
		OriginalEpisode:    101,
		FirstSeenAt:        time.Now(),
	}).Error)

	require.NoError(t, repo.Delete(feed.ID))

	require.NoError(t, db.First(&download, download.ID).Error)
	require.NoError(t, db.First(&candidate, candidate.ID).Error)
	assert.Nil(t, download.SubscriptionFeedID)
	assert.Nil(t, candidate.SubscriptionFeedID)
	assert.Equal(t, "B", candidate.SourceFeedName)
	var seenCount int64
	require.NoError(t, db.Model(&model.SubscriptionFeedSeenItem{}).
		Where("subscription_feed_id = ?", feed.ID).Count(&seenCount).Error)
	assert.Zero(t, seenCount)
}

func TestCreateFeedPreservesExplicitFalseFlags(t *testing.T) {
	repo, _ := setupSubscriptionFeedRepository(t)
	feed := model.SubscriptionFeed{
		SubscriptionID:   1,
		Name:             "Disabled",
		RSSURL:           "https://disabled.test/rss",
		RSSURLNormalized: "https://disabled.test/rss",
		Enabled:          false,
		BaselinePending:  false,
	}

	require.NoError(t, repo.Create(&feed))
	stored, err := repo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.False(t, stored.Enabled)
	assert.False(t, stored.BaselinePending)
}

func setupSubscriptionFeedRepository(t *testing.T) (repository.SubscriptionFeedRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.SubscriptionFeed{},
		&model.SubscriptionFeedSeenItem{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	return repository.NewSubscriptionFeedRepository(db), db
}
