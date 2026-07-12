package subscription_test

import (
	"context"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscription"
	"github.com/WormW/auto-rss/internal/service/subscriptionfeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreatorRejectsDuplicateInitialFeedsWithoutPartialWrite(t *testing.T) {
	creator, db := newSubscriptionCreatorFixture(t)
	sub := &model.Subscription{Name: "Anime", Season: 1, Enabled: true}

	err := creator.Create(context.Background(), sub, []subscriptionfeed.Input{
		{Name: "A", RSSURL: "https://same.test/rss", EpisodeOffset: 0, Enabled: true},
		{Name: "Duplicate", RSSURL: "https://same.test/rss", EpisodeOffset: 100, Enabled: true},
	})

	require.Error(t, err)
	var subscriptions, feeds int64
	require.NoError(t, db.Model(&model.Subscription{}).Count(&subscriptions).Error)
	require.NoError(t, db.Model(&model.SubscriptionFeed{}).Count(&feeds).Error)
	assert.Zero(t, subscriptions)
	assert.Zero(t, feeds)
}

func newSubscriptionCreatorFixture(t *testing.T) (subscription.Creator, *gorm.DB) {
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
	feedRepo := repository.NewSubscriptionFeedRepository(db)
	parser := &creatorParser{items: []rss.RSSItem{{Title: "Anime 101", Episode: 101}}}
	feedService := subscriptionfeed.NewService(db, feedRepo, parser)
	return subscription.NewCreator(db, feedService), db
}

type creatorParser struct {
	items []rss.RSSItem
}

func (p *creatorParser) FetchAndParse(string) ([]rss.RSSItem, error) {
	return append([]rss.RSSItem(nil), p.items...), nil
}

func (p *creatorParser) FetchAndParseWithTimeout(string, time.Duration) ([]rss.RSSItem, error) {
	return p.FetchAndParse("")
}

func (p *creatorParser) Parse(interface{}) ([]rss.RSSItem, error) { return nil, nil }
func (p *creatorParser) ExtractFansub(string) string              { return "" }
func (p *creatorParser) ExtractEpisode(string) int                { return 0 }
func (p *creatorParser) SetProxy(string) error                    { return nil }
