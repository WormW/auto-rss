package handler

import (
	"context"
	"encoding/json"
	"errors"
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
				return &model.Subscription{ID: id, Name: "Healthy Anime", RssURL: feed.URL}, nil
			},
		}
		handler := newTestRSSHealthHandler(repo)

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
		assert.Empty(t, body.Data.ErrorMessage)
	})
}

func TestRSSHealthHandler_CheckAllSummarizesLocalFeedResults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	healthyFeed := newRSSFeedServer(t, http.StatusOK, validRSSFeed("Healthy Anime"))
	unhealthyFeed := newRSSFeedServer(t, http.StatusOK, "not rss")
	deadFeed := newRSSFeedServer(t, http.StatusInternalServerError, "server error")

	repo := &mockSubscriptionRepo{
		getActiveFunc: func() ([]model.Subscription, error) {
			return []model.Subscription{
				{ID: 1, Name: "Healthy Anime", RssURL: healthyFeed.URL},
				{ID: 2, Name: "Broken XML", RssURL: unhealthyFeed.URL},
				{ID: 3, Name: "Dead Server", RssURL: deadFeed.URL},
				{ID: 4, Name: "No URL"},
			}, nil
		},
	}
	handler := newTestRSSHealthHandler(repo)

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
				{ID: 1, Name: "Healthy Anime", RssURL: healthyFeed.URL},
				{ID: 2, Name: "Dead Server", RssURL: deadFeed.URL},
			}, nil
		},
	}
	handler := newTestRSSHealthHandler(repo)

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

func newTestRSSHealthHandler(repo *mockSubscriptionRepo) *RSSHealthHandler {
	return NewRSSHealthHandler(rss.NewHealthChecker(repo), repo)
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
