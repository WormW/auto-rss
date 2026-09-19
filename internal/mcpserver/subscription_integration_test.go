package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMCPCreateSubscriptionCreatesSchedulableFeedAndEpisodeLedger(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Test</title><item><title>[Group] Test [01]</title><link>https://example.test/1</link></item></channel></rss>`))
	}))
	defer feed.Close()

	server, db := newSubscriptionIntegrationServer(t)
	input := CreateSubscriptionInput{Name: "Test", RSSURL: feed.URL, TotalEpisodes: 12}
	_, created, err := server.createSubscription(context.Background(), nil, input)
	require.NoError(t, err)

	feeds, err := repository.NewSubscriptionFeedRepository(db).ListEnabledBySubscriptionIDs([]uint{created.Subscription.ID})
	require.NoError(t, err)
	require.Len(t, feeds, 1, "the scheduler only processes enabled subscription_feeds")
	require.Equal(t, feed.URL, feeds[0].RSSURL)
	require.True(t, feeds[0].BaselinePending, "historical items must establish a baseline before automatic downloading")
	var episodes int64
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Where("subscription_id = ?", created.Subscription.ID).Count(&episodes).Error)
	require.EqualValues(t, 12, episodes)

	_, repeated, err := server.createSubscription(context.Background(), nil, input)
	require.NoError(t, err)
	require.Equal(t, created.Subscription.ID, repeated.Subscription.ID)
	var feedCount int64
	require.NoError(t, db.Model(&model.SubscriptionFeed{}).Count(&feedCount).Error)
	require.EqualValues(t, 1, feedCount)
}

func TestMCPCreateSubscriptionRejectsInvalidFeedWithoutPartialState(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer feed.Close()
	server, db := newSubscriptionIntegrationServer(t)
	_, _, err := server.createSubscription(context.Background(), nil, CreateSubscriptionInput{Name: "Unavailable", RSSURL: feed.URL})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&model.Subscription{}).Count(&count).Error)
	require.Zero(t, count)
}

func newSubscriptionIntegrationServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "subscriptions.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.SubscriptionFeed{}, &model.SubscriptionFeedSeenItem{}, &model.SubscriptionEpisode{}, &model.Config{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return New(Dependencies{
		DB: db, Config: &config.Config{},
		SubscriptionRepo: repository.NewSubscriptionRepository(db),
		ConfigRepo:       repository.NewConfigRepository(db),
	}), db
}
