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
	require.NoError(t, repo.UpdateRSSWatermark(sub.ID, sub.RssURL, &newWatermark))
	staleWatermark := oldWatermark.Add(30 * time.Minute)
	require.NoError(t, repo.UpdateRSSWatermark(sub.ID, sub.RssURL, &staleWatermark))

	got, err := repo.GetByID(sub.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastRSSPubTime)
	assert.Equal(t, newWatermark, *got.LastRSSPubTime)
	assert.Equal(t, 4, got.CurrentEpisode)
	assert.Equal(t, 6, got.LatestEpisode)
	assert.True(t, got.RSSBaselinePending)

	err = repo.UpdateRSSWatermark(sub.ID, "https://other.example/feed.xml", &newWatermark)
	require.ErrorContains(t, err, "RSS source changed")
}
