package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRSSHealthHandler_CheckOne(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid subscription id", func(t *testing.T) {
		handler := newTestRSSHealthHandler(&mockSubscriptionRepo{})
		w := performRSSHealthRequest(handler.CheckOne, http.MethodGet, "/rss/health/not-a-number", "/rss/health/:subscription_id")

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 400, body.Code)
		assert.Equal(t, "Invalid subscription ID", body.Message)
	})

	t.Run("subscription not found", func(t *testing.T) {
		repo := &mockSubscriptionRepo{
			getByIDFunc: func(id uint) (*model.Subscription, error) {
				assert.Equal(t, uint(42), id)
				return nil, errors.New("not found")
			},
		}
		handler := newTestRSSHealthHandler(repo)

		w := performRSSHealthRequest(handler.CheckOne, http.MethodGet, "/rss/health/42", "/rss/health/:subscription_id")

		assert.Equal(t, http.StatusNotFound, w.Code)
		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 404, body.Code)
		assert.Equal(t, "Subscription not found", body.Message)
	})

	t.Run("success", func(t *testing.T) {
		feed := newRSSFeedServer(t, http.StatusOK, validRSSFeed("Healthy Anime"))
		repo := &mockSubscriptionRepo{
			getByIDFunc: func(id uint) (*model.Subscription, error) {
				assert.Equal(t, uint(7), id)
				return &model.Subscription{ID: id, Name: "Healthy Anime"}, nil
			},
		}
		handler := newTestRSSHealthHandler(repo, model.SubscriptionFeed{
			ID: 70, SubscriptionID: 7, Name: "Default", RSSURL: feed.URL, Enabled: true,
		})

		w := performRSSHealthRequest(handler.CheckOne, http.MethodGet, "/rss/health/7", "/rss/health/:subscription_id")

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Code int                   `json:"code"`
			Data rss.HealthCheckResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		assert.Equal(t, uint(7), body.Data.SubscriptionID)
		assert.Equal(t, "Healthy Anime", body.Data.Name)
		assert.Equal(t, rss.HealthStatusHealthy, body.Data.Status)
		require.Len(t, body.Data.Feeds, 1)
		assert.Equal(t, rss.HealthStatusHealthy, body.Data.Feeds[0].Status)
	})
}

func TestRSSHealthHandler_CheckOneAppliesSystemProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	proxyCalls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, validRSSFeed("Proxy Anime"))
	}))
	defer proxy.Close()

	_, subRepo, _, configRepo := setupSubscriptionDiagnosticsTest(t)
	subscription := model.Subscription{Name: "Proxy Anime", RssURL: "http://rss.invalid/feed"}
	require.NoError(t, subRepo.Create(&subscription))
	require.NoError(t, configRepo.Set("system_proxy", proxy.URL))
	handler := NewRSSHealthHandler(rss.NewHealthChecker(subRepo, nil), subRepo, configRepo)

	w := performRSSHealthRequest(handler.CheckOne, http.MethodGet, "/rss/health/1", "/rss/health/:subscription_id")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Data rss.HealthCheckResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, rss.HealthStatusHealthy, body.Data.Status)
	require.Equal(t, 1, proxyCalls)
}

func TestRSSHealthHandler_CheckAllSummarizesLocalFeedResults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	healthyFeed := newRSSFeedServer(t, http.StatusOK, validRSSFeed("Healthy Anime"))
	unhealthyFeed := newRSSFeedServer(t, http.StatusOK, "not rss")
	deadFeed := newRSSFeedServer(t, http.StatusInternalServerError, "server error")

	repo := &mockSubscriptionRepo{
		getActiveFunc: func() ([]model.Subscription, error) {
			return []model.Subscription{
				{ID: 1, Name: "Healthy Anime"},
				{ID: 2, Name: "Broken XML"},
				{ID: 3, Name: "Dead Server"},
				{ID: 4, Name: "No URL"},
			}, nil
		},
	}
	handler := newTestRSSHealthHandler(repo,
		model.SubscriptionFeed{ID: 1, SubscriptionID: 1, RSSURL: healthyFeed.URL, Enabled: true},
		model.SubscriptionFeed{ID: 2, SubscriptionID: 2, RSSURL: unhealthyFeed.URL, Enabled: true},
		model.SubscriptionFeed{ID: 3, SubscriptionID: 3, RSSURL: deadFeed.URL, Enabled: true},
	)

	w := performRSSHealthRequest(handler.CheckAll, http.MethodGet, "/rss/health", "/rss/health")

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Code int `json:"code"`
		Data struct {
			Results []rss.HealthCheckResult `json:"results"`
			Summary HealthCheckSummary      `json:"summary"`
			At      string                  `json:"checked_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.Len(t, body.Data.Results, 4)
	assert.NotEmpty(t, body.Data.At)
	assert.Equal(t, HealthCheckSummary{
		Total:     4,
		Healthy:   1,
		Unhealthy: 1,
		Dead:      1,
		Unknown:   1,
	}, body.Data.Summary)
}

func TestRSSHealthHandler_GetDeadReturnsOnlyDeadSubscriptions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	healthyFeed := newRSSFeedServer(t, http.StatusOK, validRSSFeed("Healthy Anime"))
	deadFeed := newRSSFeedServer(t, http.StatusInternalServerError, "server error")
	repo := &mockSubscriptionRepo{
		getActiveFunc: func() ([]model.Subscription, error) {
			return []model.Subscription{
				{ID: 1, Name: "Healthy Anime"},
				{ID: 2, Name: "Dead Server"},
			}, nil
		},
	}
	handler := newTestRSSHealthHandler(repo,
		model.SubscriptionFeed{ID: 1, SubscriptionID: 1, RSSURL: healthyFeed.URL, Enabled: true},
		model.SubscriptionFeed{ID: 2, SubscriptionID: 2, RSSURL: deadFeed.URL, Enabled: true},
	)

	w := performRSSHealthRequest(handler.GetDead, http.MethodGet, "/rss/dead", "/rss/dead")

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Code int `json:"code"`
		Data struct {
			Count int                     `json:"count"`
			Items []rss.HealthCheckResult `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.Equal(t, 1, body.Data.Count)
	require.Len(t, body.Data.Items, 1)
	assert.Equal(t, uint(2), body.Data.Items[0].SubscriptionID)
	assert.Equal(t, rss.HealthStatusDead, body.Data.Items[0].Status)
}

func TestRSSHealthHandler_TriggerCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("starts async health check task", func(t *testing.T) {
		ensureNoRunningTask(t)

		release := make(chan struct{})
		var releaseOnce sync.Once
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(release) })
			ensureNoRunningTask(t)
		})

		repo := &mockSubscriptionRepo{
			getActiveFunc: func() ([]model.Subscription, error) {
				<-release
				return nil, nil
			},
		}
		handler := newTestRSSHealthHandler(repo)

		w := performRSSHealthRequest(handler.TriggerCheck, http.MethodPost, "/rss/health-check", "/rss/health-check")

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Code int                  `json:"code"`
			Data TriggerCheckResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		assert.NotEmpty(t, body.Data.TaskID)
		assert.Equal(t, "RSS订阅健康检查", body.Data.TaskName)
		assert.Equal(t, string(task.TaskStatusRunning), body.Data.Status)

		releaseOnce.Do(func() { close(release) })
	})

	t.Run("returns conflict while rss health check is already running", func(t *testing.T) {
		ensureNoRunningTask(t)

		started := make(chan struct{})
		manager := task.GetManager()
		_, err := manager.StartTask(task.TaskType("rss_health_check"), 0, "existing health check", func(ctx context.Context, t *task.Task) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		require.NoError(t, err)
		<-started
		t.Cleanup(func() { ensureNoRunningTask(t) })

		handler := newTestRSSHealthHandler(&mockSubscriptionRepo{})
		w := performRSSHealthRequest(handler.TriggerCheck, http.MethodPost, "/rss/health-check", "/rss/health-check")

		assert.Equal(t, http.StatusConflict, w.Code)
		var body struct {
			Code int `json:"code"`
			Data struct {
				TaskID string `json:"task_id"`
				Status string `json:"status"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 409, body.Code)
		assert.NotEmpty(t, body.Data.TaskID)
		assert.Equal(t, string(task.TaskStatusRunning), body.Data.Status)
	})
}

type rssHealthFeedRepo struct {
	feedsBySubscription map[uint][]model.SubscriptionFeed
}

func (r *rssHealthFeedRepo) ListBySubscription(subscriptionID uint) ([]model.SubscriptionFeed, error) {
	return append([]model.SubscriptionFeed(nil), r.feedsBySubscription[subscriptionID]...), nil
}

func (r *rssHealthFeedRepo) ListEnabledBySubscriptionIDs(subscriptionIDs []uint) ([]model.SubscriptionFeed, error) {
	feeds := make([]model.SubscriptionFeed, 0)
	for _, subscriptionID := range subscriptionIDs {
		for _, feed := range r.feedsBySubscription[subscriptionID] {
			if feed.Enabled {
				feeds = append(feeds, feed)
			}
		}
	}
	return feeds, nil
}

func newTestRSSHealthHandler(repo *mockSubscriptionRepo, feeds ...model.SubscriptionFeed) *RSSHealthHandler {
	feedRepo := &rssHealthFeedRepo{feedsBySubscription: make(map[uint][]model.SubscriptionFeed)}
	for _, feed := range feeds {
		feedRepo.feedsBySubscription[feed.SubscriptionID] = append(feedRepo.feedsBySubscription[feed.SubscriptionID], feed)
	}
	return NewRSSHealthHandler(rss.NewHealthChecker(repo, feedRepo), repo)
}

func performRSSHealthRequest(handler gin.HandlerFunc, method, target, route string) *httptest.ResponseRecorder {
	router := gin.New()
	router.Handle(method, route, handler)

	req := httptest.NewRequest(method, target, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func newRSSFeedServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func validRSSFeed(title string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>` + title + `</title>
    <item>
      <title>Episode 1</title>
      <link>https://example.test/episode-1</link>
      <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
    </item>
  </channel>
</rss>`
}

func ensureNoRunningTask(t *testing.T) {
	t.Helper()

	manager := task.GetManager()
	if manager.IsRunning() {
		_ = manager.CancelTask()
	}

	require.Eventually(t, func() bool {
		return !manager.IsRunning()
	}, time.Second, 10*time.Millisecond)
}
