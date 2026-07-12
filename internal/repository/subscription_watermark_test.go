package repository

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateRSSWatermarkOnlyAdvancesMatchingSourceColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}))
	repo := NewSubscriptionRepository(db)
	oldWatermark := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	sub := model.Subscription{
		Name:               "Watermark Show",
		RssURL:             "https://example.com/feed.xml",
		CurrentEpisode:     4,
		LatestEpisode:      6,
		RSSBaselinePending: true,
		LastRSSPubTime:     &oldWatermark,
	}
	require.NoError(t, repo.Create(&sub))

	newWatermark := oldWatermark.Add(time.Hour)
	expectedUpdatedAt := sub.UpdatedAt
	require.NoError(t, repo.UpdateRSSWatermark(sub.ID, sub.RssURL, expectedUpdatedAt, &newWatermark))
	got, err := repo.GetByID(sub.ID)
	require.NoError(t, err)
	expectedUpdatedAt = got.UpdatedAt
	staleWatermark := oldWatermark.Add(30 * time.Minute)
	require.NoError(t, repo.UpdateRSSWatermark(sub.ID, sub.RssURL, expectedUpdatedAt, &staleWatermark))

	got, err = repo.GetByID(sub.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastRSSPubTime)
	assert.Equal(t, newWatermark, *got.LastRSSPubTime)
	assert.Equal(t, 4, got.CurrentEpisode)
	assert.Equal(t, 6, got.LatestEpisode)
	assert.True(t, got.RSSBaselinePending)

	err = repo.UpdateRSSWatermark(sub.ID, "https://other.example/feed.xml", got.UpdatedAt, &newWatermark)
	require.ErrorContains(t, err, "RSS source changed")
}

func TestUpdateRSSWatermarkInTxCASProtectsEmptyFeedAndABA(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}))
	repo := NewSubscriptionRepository(db)
	sub := model.Subscription{Name: "CAS Show", RssURL: "https://example.com/a.xml"}
	require.NoError(t, repo.Create(&sub))
	snapshotUpdatedAt := sub.UpdatedAt

	require.NoError(t, db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{
		"rss_url":    "https://example.com/b.xml",
		"updated_at": snapshotUpdatedAt.Add(time.Second),
	}).Error)
	require.NoError(t, db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{
		"rss_url":    sub.RssURL,
		"updated_at": snapshotUpdatedAt.Add(2 * time.Second),
	}).Error)

	err = db.Transaction(func(tx *gorm.DB) error {
		return repo.UpdateRSSWatermarkInTx(tx, sub.ID, sub.RssURL, snapshotUpdatedAt, nil)
	})
	require.ErrorContains(t, err, "RSS source changed")
}
