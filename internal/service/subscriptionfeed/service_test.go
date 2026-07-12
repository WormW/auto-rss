package subscriptionfeed_test

import (
	"context"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscriptionfeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPreviewMapsOriginalEpisodesWithFeedOffset(t *testing.T) {
	parser := &fakeParser{items: []rss.RSSItem{
		{Title: "Anime 101", Episode: 101},
		{Title: "Anime 100", Episode: 100},
	}}
	svc, _, _ := newFeedServiceFixture(t, parser)

	preview, err := svc.Preview(context.Background(), subscriptionfeed.Input{
		RSSURL:        "https://example.test/rss",
		EpisodeOffset: 100,
	})

	require.NoError(t, err)
	require.Len(t, preview.Items, 2)
	assert.Equal(t, 1, preview.Items[0].RelativeEpisode)
	assert.True(t, preview.Items[0].Valid)
	assert.False(t, preview.Items[1].Valid)
	assert.Equal(t, "relative_episode_not_positive", preview.Items[1].InvalidReason)
}

func TestCreateRejectsFeedWhoseNonEmptyItemsHaveNoValidMapping(t *testing.T) {
	parser := &fakeParser{items: []rss.RSSItem{{Title: "Anime 100", Episode: 100}}}
	svc, _, _ := newFeedServiceFixture(t, parser)

	_, err := svc.Create(context.Background(), 1, subscriptionfeed.Input{
		Name:          "B",
		RSSURL:        "https://example.test/rss",
		EpisodeOffset: 100,
	})

	assert.ErrorIs(t, err, subscriptionfeed.ErrNoMappableEpisodes)
}

func TestUpdateURLOrOffsetResetsBaselineButRenameDoesNot(t *testing.T) {
	parser := &fakeParser{items: []rss.RSSItem{{Title: "Anime 101", Episode: 101}}}
	svc, repo, db := newFeedServiceFixture(t, parser)
	feed := model.SubscriptionFeed{
		SubscriptionID:   1,
		Name:             "A",
		RSSURL:           "https://a.test/rss",
		RSSURLNormalized: "https://a.test/rss",
		EpisodeOffset:    0,
		Enabled:          true,
		BaselinePending:  false,
	}
	require.NoError(t, repo.Create(&feed))
	require.NoError(t, db.Create(&model.SubscriptionFeedSeenItem{
		SubscriptionFeedID: feed.ID,
		ResourceKey:        "hash:old",
		OriginalEpisode:    1,
		FirstSeenAt:        time.Now(),
	}).Error)

	renamed, err := svc.Update(context.Background(), feed.ID, subscriptionfeed.Input{
		Name:          "A renamed",
		RSSURL:        feed.RSSURL,
		EpisodeOffset: 0,
		Enabled:       true,
	})
	require.NoError(t, err)
	assert.False(t, renamed.BaselinePending)
	var seenCount int64
	require.NoError(t, db.Model(&model.SubscriptionFeedSeenItem{}).
		Where("subscription_feed_id = ?", feed.ID).Count(&seenCount).Error)
	assert.EqualValues(t, 1, seenCount)

	changed, err := svc.Update(context.Background(), feed.ID, subscriptionfeed.Input{
		Name:          "A renamed",
		RSSURL:        feed.RSSURL,
		EpisodeOffset: 100,
		Enabled:       true,
	})
	require.NoError(t, err)
	assert.True(t, changed.BaselinePending)
	assert.Nil(t, changed.LastRSSPubTime)
	require.NoError(t, db.Model(&model.SubscriptionFeedSeenItem{}).
		Where("subscription_feed_id = ?", feed.ID).Count(&seenCount).Error)
	assert.Zero(t, seenCount)
}

func newFeedServiceFixture(
	t *testing.T,
	parser rss.Parser,
) (*subscriptionfeed.Service, repository.SubscriptionFeedRepository, *gorm.DB) {
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
	subscription := model.Subscription{ID: 1, Name: "Anime", Status: "active", Enabled: true}
	require.NoError(t, db.Create(&subscription).Error)
	repo := repository.NewSubscriptionFeedRepository(db)
	return subscriptionfeed.NewService(db, repo, parser), repo, db
}

type fakeParser struct {
	items []rss.RSSItem
	err   error
}

func (p *fakeParser) FetchAndParse(string) ([]rss.RSSItem, error) {
	return append([]rss.RSSItem(nil), p.items...), p.err
}

func (p *fakeParser) FetchAndParseWithTimeout(string, time.Duration) ([]rss.RSSItem, error) {
	return p.FetchAndParse("")
}

func (p *fakeParser) Parse(interface{}) ([]rss.RSSItem, error) { return nil, nil }
func (p *fakeParser) ExtractFansub(string) string              { return "" }
func (p *fakeParser) ExtractEpisode(string) int                { return 0 }
func (p *fakeParser) SetProxy(string) error                    { return nil }
