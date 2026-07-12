package rss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

func TestRSSHealthChecker_SetProxyAndClear(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, validHealthRSS("Proxy Anime"))
	}))
	defer proxy.Close()

	checker := NewHealthChecker(nil, nil)
	require.NoError(t, checker.SetProxy(proxy.URL))

	proxied := checker.CheckSubscription(context.Background(), &model.Subscription{
		RssURL: "http://rss.invalid/feed",
	})
	require.Equal(t, HealthStatusHealthy, proxied.Status)
	require.EqualValues(t, 1, proxyCalls.Load())

	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, validHealthRSS("Direct Anime"))
	}))
	defer direct.Close()

	require.NoError(t, checker.SetProxy(""))
	directResult := checker.CheckSubscription(context.Background(), &model.Subscription{
		RssURL: direct.URL,
	})
	require.Equal(t, HealthStatusHealthy, directResult.Status)
	require.EqualValues(t, 1, proxyCalls.Load())
}

func TestRSSHealthChecker_SetProxyConcurrentWithChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, validHealthRSS("Concurrent Anime"))
	}))
	defer server.Close()

	checker := NewHealthChecker(nil, nil)
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			require.NoError(t, checker.SetProxy(""))
		}
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				result := checker.CheckSubscription(context.Background(), &model.Subscription{RssURL: server.URL})
				require.Equal(t, HealthStatusHealthy, result.Status)
			}
		}()
	}

	close(start)
	wg.Wait()
}

func validHealthRSS(title string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Auto RSS Test</title>
    <link>https://example.com</link>
    <description>Test feed</description>
    <item>
      <title>%s</title>
      <link>https://example.com/anime.torrent</link>
      <guid>health-test-item</guid>
      <pubDate>Fri, 11 Jul 2026 12:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`, title)
}
