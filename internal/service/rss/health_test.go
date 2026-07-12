package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSubscriptionFeedsReportsHealthyWhenOneFeedWorks(t *testing.T) {
	checker, server := newFeedHealthFixture(t)
	sub := model.Subscription{ID: 1, Name: "Anime"}
	lastSuccess := time.Now().UTC().Add(-time.Hour)
	feeds := []model.SubscriptionFeed{
		{ID: 10, SubscriptionID: 1, Name: "A", RSSURL: server.URL + "/dead", Enabled: true},
		{ID: 11, SubscriptionID: 1, Name: "B", RSSURL: server.URL + "/healthy", Enabled: true, LastSuccessAt: &lastSuccess, LastError: "previous timeout"},
	}

	result := checker.CheckSubscriptionFeeds(context.Background(), &sub, feeds)

	assert.Equal(t, HealthStatusHealthy, result.Status)
	require.Len(t, result.Feeds, 2)
	assert.Equal(t, HealthStatusDead, result.Feeds[0].Status)
	assert.Equal(t, HealthStatusHealthy, result.Feeds[1].Status)
	assert.Equal(t, &lastSuccess, result.Feeds[1].LastSuccessAt)
	assert.Equal(t, "previous timeout", result.Feeds[1].LastError)
}

func TestCheckSubscriptionFeedsIsDeadOnlyWhenAllEnabledFeedsAreDead(t *testing.T) {
	checker, server := newFeedHealthFixture(t)
	result := checker.CheckSubscriptionFeeds(context.Background(), &model.Subscription{ID: 1}, []model.SubscriptionFeed{
		{ID: 1, RSSURL: server.URL + "/dead", Enabled: true},
		{ID: 2, RSSURL: server.URL + "/dead", Enabled: true},
	})

	assert.Equal(t, HealthStatusDead, result.Status)
}

func newFeedHealthFixture(t *testing.T) (*RSSHealthChecker, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthy":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(validHealthRSSFeed))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)
	checker := NewHealthChecker(nil, nil)
	checker.httpClient = server.Client()
	return checker, server
}

const validHealthRSSFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Healthy</title><item><title>Episode 1</title><link>https://example.test/1</link><pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate></item></channel></rss>`
