package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	episodeservice "github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionDiagnosticsReportsFeedHealthAndFreshness(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthy" {
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(validRSSFeed("Healthy Feed")))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(gateway.Close)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.SubscriptionFeed{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}, &model.Config{},
	))
	subRepo := repository.NewSubscriptionRepository(db)
	feedRepo := repository.NewSubscriptionFeedRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	sub := model.Subscription{Name: "Multi Feed", Enabled: true, RssURL: "https://legacy.invalid/rss"}
	require.NoError(t, subRepo.Create(&sub))
	lastSuccess := time.Now().Add(-time.Hour)
	lastPost := time.Now().Add(-2 * time.Hour)
	for _, feed := range []model.SubscriptionFeed{
		{SubscriptionID: sub.ID, Name: "Dead", RSSURL: gateway.URL + "/dead", RSSURLNormalized: gateway.URL + "/dead", Enabled: true},
		{SubscriptionID: sub.ID, Name: "Healthy", RSSURL: gateway.URL + "/healthy", RSSURLNormalized: gateway.URL + "/healthy", Enabled: true, LastSuccessAt: &lastSuccess, LastRSSPubTime: &lastPost},
	} {
		feed := feed
		require.NoError(t, feedRepo.Create(&feed))
	}

	handler := NewSubscriptionDiagnosticsHandler(subRepo, feedRepo, downloadRepo, repository.NewConfigRepository(db), nil, t.TempDir())
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/subscriptions/:id/diagnostics", handler.Get)
	router.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)

	initial := performSubscriptionDiagnosticsRequest(router, http.MethodGet, "/subscriptions/1/diagnostics")
	require.Equal(t, http.StatusOK, initial.Code, initial.Body.String())
	var initialResponse struct {
		Data SubscriptionDiagnosticsResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(initial.Body.Bytes(), &initialResponse))
	require.Len(t, initialResponse.Data.Feeds, 2)
	assert.Equal(t, rss.HealthStatusUnknown, initialResponse.Data.Feeds[0].Status)

	reachability := performSubscriptionDiagnosticsRequest(router, http.MethodPost, "/subscriptions/1/diagnostics/checks/rss_reachability")
	require.Equal(t, http.StatusOK, reachability.Code, reachability.Body.String())
	reachabilityResult := decodeSubscriptionDiagnosticCheck(t, reachability)
	assert.Equal(t, SubscriptionDiagnosticWarning, reachabilityResult.Check.Status)
	assert.Contains(t, reachabilityResult.Check.Detail, "Dead")
	require.Len(t, reachabilityResult.Feeds, 2)

	freshness := performSubscriptionDiagnosticsRequest(router, http.MethodPost, "/subscriptions/1/diagnostics/checks/rss_freshness")
	require.Equal(t, http.StatusOK, freshness.Code, freshness.Body.String())
	freshnessResult := decodeSubscriptionDiagnosticCheck(t, freshness)
	assert.Equal(t, SubscriptionDiagnosticHealthy, freshnessResult.Check.Status)
	assert.Contains(t, freshnessResult.Check.Detail, "feed 水位线")
}

func TestBuildRSSReachabilityCheckReportsErrorWhenAllFeedsFail(t *testing.T) {
	handler := &SubscriptionDiagnosticsHandler{}
	check := handler.buildRSSReachabilityCheck(&rss.HealthCheckResult{
		Status: rss.HealthStatusDead,
		Feeds: []rss.FeedHealthCheckResult{
			{Name: "A", Status: rss.HealthStatusDead, ErrorMessage: "timeout"},
			{Name: "B", Status: rss.HealthStatusUnhealthy, ErrorMessage: "parse failed"},
		},
	})

	assert.Equal(t, SubscriptionDiagnosticError, check.Status)
	assert.True(t, strings.Contains(check.Detail, "A") && strings.Contains(check.Detail, "B"))
}

func TestSubscriptionDiagnosticsHandler_GetInitialState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, subRepo, _, _ := setupSubscriptionDiagnosticsTest(t)
	sub := model.Subscription{
		Name:    "Test Anime",
		Enabled: true,
		RssURL:  "http://rss.invalid/feed",
		Season:  1,
	}
	require.NoError(t, subRepo.Create(&sub))

	handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, nil, nil, nil, "/path-that-must-not-be-statted")
	r := gin.New()
	r.GET("/subscriptions/:id/diagnostics", handler.Get)
	w := performSubscriptionDiagnosticsRequest(r, http.MethodGet, "/subscriptions/1/diagnostics")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int                             `json:"code"`
		Data SubscriptionDiagnosticsResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, string(SubscriptionDiagnosticUnknown), resp.Data.Summary.Overall)
	require.Equal(t, 0, resp.Data.Summary.Checked)
	require.Equal(t, 8, resp.Data.Summary.Total)
	require.Equal(t, 8, resp.Data.Summary.Unknown)
	require.Len(t, resp.Data.Checks, 8)
	require.NotNil(t, resp.Data.Downloads.FailedItems)
	require.NotNil(t, resp.Data.Files.MissingEpisodes)
	for _, check := range resp.Data.Checks {
		require.Equal(t, SubscriptionDiagnosticUnknown, check.Status)
		require.False(t, check.Checked)
		require.Equal(t, "未检查", check.Summary)
	}

	actionByKey := map[string]SubscriptionDiagnosticAction{}
	for _, action := range resp.Data.Actions {
		actionByKey[action.Key] = action
	}
	require.True(t, actionByKey["refresh_rss"].Enabled)
	require.False(t, actionByKey["retry_failed"].Enabled)
	require.Equal(t, "请先检查下载任务", actionByKey["retry_failed"].Reason)
	_, exposesScan := actionByKey["scan_files"]
	require.False(t, exposesScan)
	require.NotNil(t, db)
}

func TestSubscriptionDiagnosticsHandler_Check(t *testing.T) {
	t.Run("runs one known check and rejects an unknown key", func(t *testing.T) {
		_, subRepo, _, _ := setupSubscriptionDiagnosticsTest(t)
		sub := model.Subscription{Name: "Check Anime", Enabled: true, Status: "active"}
		require.NoError(t, subRepo.Create(&sub))

		handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, nil, nil, nil, t.TempDir())
		r := gin.New()
		r.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)

		ok := performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/subscription_enabled")
		require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
		result := decodeSubscriptionDiagnosticCheck(t, ok)
		require.Equal(t, "subscription_enabled", result.Check.Key)
		require.Equal(t, SubscriptionDiagnosticHealthy, result.Check.Status)

		bad := performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/not-real")
		require.Equal(t, http.StatusBadRequest, bad.Code, bad.Body.String())
	})

	t.Run("marks inconclusive checks as executed", func(t *testing.T) {
		_, subRepo, downloadRepo, _ := setupSubscriptionDiagnosticsTest(t)
		sub := model.Subscription{Name: "Incomplete Anime", Enabled: true, RenameEnabled: false}
		require.NoError(t, subRepo.Create(&sub))

		handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, downloadRepo, nil, nil, t.TempDir())
		r := gin.New()
		r.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)

		for _, key := range []string{"rss_reachability", "downloads", "qbittorrent", "files", "organizer"} {
			w := performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/"+key)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			result := decodeSubscriptionDiagnosticCheck(t, w)
			require.True(t, result.Check.Checked, key)
		}
	})

	t.Run("reports relative pending episodes", func(t *testing.T) {
		_, subRepo, _, _ := setupSubscriptionDiagnosticsTest(t)
		sub := model.Subscription{
			Name:           "Offset Anime",
			Enabled:        true,
			EpisodeOffset:  170,
			CurrentEpisode: 221,
			LatestEpisode:  222,
		}
		require.NoError(t, subRepo.Create(&sub))

		handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, nil, nil, nil, t.TempDir())
		r := gin.New()
		r.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)
		w := performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/episode_progress")

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		result := decodeSubscriptionDiagnosticCheck(t, w)
		require.Equal(t, "episode_progress", result.Check.Key)
		require.Equal(t, "待收集集数", result.Check.Label)
		require.NotNil(t, result.Files)
		require.NotNil(t, result.Files.MissingEpisodes)
		require.Equal(t, []int{52}, *result.Files.MissingEpisodes)
		require.NotContains(t, w.Body.String(), "completed_with_file")
	})

	t.Run("checks qBittorrent with one list request", func(t *testing.T) {
		_, subRepo, downloadRepo, _ := setupSubscriptionDiagnosticsTest(t)
		sub := model.Subscription{Name: "qB Anime", Enabled: true}
		require.NoError(t, subRepo.Create(&sub))
		require.NoError(t, downloadRepo.Create(&model.Download{
			SubscriptionID: sub.ID,
			Title:          "qB Anime 01",
			TorrentHash:    "missing-hash",
			Status:         model.DownloadStatusDownloading,
		}))
		qb := &subscriptionDiagnosticsQBClient{
			version:  "4.6.0",
			torrents: []*downloader.TorrentInfo{{Hash: "another-hash"}},
		}

		handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, downloadRepo, nil, qb, t.TempDir())
		r := gin.New()
		r.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)
		w := performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/qbittorrent")

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		result := decodeSubscriptionDiagnosticCheck(t, w)
		require.Equal(t, 1, qb.listCalls)
		require.Equal(t, 0, qb.infoCalls)
		require.NotNil(t, result.Downloads)
		require.NotNil(t, result.Downloads.MissingTorrentTasks)
		require.Equal(t, 1, *result.Downloads.MissingTorrentTasks)
		require.NotContains(t, w.Body.String(), "\"total\"")
		require.NotContains(t, w.Body.String(), "\"failed_items\"")
	})

	t.Run("download check does not overwrite qBittorrent metrics", func(t *testing.T) {
		_, subRepo, downloadRepo, _ := setupSubscriptionDiagnosticsTest(t)
		sub := model.Subscription{Name: "Downloads Anime", Enabled: true}
		require.NoError(t, subRepo.Create(&sub))

		handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, downloadRepo, nil, nil, t.TempDir())
		r := gin.New()
		r.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)
		w := performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/downloads")

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "\"missing_torrent_tasks\"")
	})

	t.Run("applies configured RSS proxy", func(t *testing.T) {
		var proxyCalls int
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyCalls++
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Proxy</title><link>https://example.com</link><description>Proxy feed</description><item><title>Anime 01</title><link>https://example.com/1</link><guid>1</guid></item></channel></rss>`)
		}))
		defer proxy.Close()

		_, subRepo, _, configRepo := setupSubscriptionDiagnosticsTest(t)
		sub := model.Subscription{Name: "Proxy Anime", Enabled: true, RssURL: "http://rss.invalid/feed"}
		require.NoError(t, subRepo.Create(&sub))
		require.NoError(t, configRepo.Set("system_proxy", proxy.URL))

		handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, nil, configRepo, nil, t.TempDir())
		r := gin.New()
		r.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)
		w := performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/rss_reachability")

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		result := decodeSubscriptionDiagnosticCheck(t, w)
		require.Equal(t, SubscriptionDiagnosticHealthy, result.Check.Status)
		require.Equal(t, 1, proxyCalls)
	})
}

func TestSubscriptionDiagnosticsHandler_RetryFailedResetsRetryableDownloads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.SubscriptionFeed{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}, &model.Config{},
	))

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	sub := model.Subscription{Name: "Retry Anime", Enabled: true, Season: 1}
	require.NoError(t, subRepo.Create(&sub))

	failed := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Retry Anime 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:3333333333333333333333333333333333333333",
		TorrentHash:    "old-hash",
		Status:         model.DownloadStatusFailed,
		RetryCount:     5,
		MaxRetries:     5,
		LastError:      "timeout",
	}
	require.NoError(t, downloadRepo.Create(&failed))
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID,
		Episode:        1,
		Status:         model.EpisodeStatusMissing,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, db.Create(&ledger).Error)
	require.NoError(t, downloadRepo.Create(&model.Download{
		SubscriptionID: sub.ID,
		Title:          "Retry Anime 02",
		Episode:        2,
		TorrentHash:    "other-hash",
		Status:         model.DownloadStatusFailed,
	}))

	episodeRepo := repository.NewEpisodeRepository(db)
	qb := &mockQBittorrentClient{}
	handler := NewSubscriptionDiagnosticsHandler(subRepo, repository.NewSubscriptionFeedRepository(db), downloadRepo, nil, qb, t.TempDir(), episodeservice.NewService(episodeRepo))
	r := gin.New()
	r.POST("/subscriptions/:id/diagnostics/retry-failed", handler.RetryFailed)

	req := httptest.NewRequest(http.MethodPost, "/subscriptions/1/diagnostics/retry-failed", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int                             `json:"code"`
		Data SubscriptionRetryFailedResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Data.Retried)
	require.Equal(t, 1, resp.Data.Skipped)
	require.Equal(t, 0, resp.Data.Failed)
	require.Len(t, resp.Data.Results, 2)
	var retryResult *SubscriptionRetryResult
	for i := range resp.Data.Results {
		if resp.Data.Results[i].ID == failed.ID {
			retryResult = &resp.Data.Results[i]
			break
		}
	}
	require.NotNil(t, retryResult)
	require.Equal(t, model.DownloadStatusRetryCleanup, retryResult.Status)
	require.Equal(t, "已排队清理旧下载，清理完成后将自动重试", retryResult.Message)

	updated, err := downloadRepo.GetByID(failed.ID)
	require.NoError(t, err)
	require.Equal(t, model.DownloadStatusRetryCleanup, updated.Status)
	require.Equal(t, 0, updated.RetryCount)
	require.Equal(t, "old-hash", updated.TorrentHash)
	require.Empty(t, updated.LastError)
	require.Equal(t, "user_retry", updated.RetryReason)
	require.False(t, qb.deleteWithPayloadCalled)
	require.False(t, qb.addCalled, "DownloadMonitor must own the first qBittorrent Add")
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	require.Equal(t, model.EpisodeStatusDownloading, after.Status)
	require.NotNil(t, after.ActiveDownloadID)
	require.Equal(t, failed.ID, *after.ActiveDownloadID)
}

func TestSubscriptionDiagnosticsRetryConflictDoesNotDeletePayload(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.SubscriptionFeed{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{},
	))
	sub := model.Subscription{Name: "Diagnostics Conflict", Season: 1}
	require.NoError(t, db.Create(&sub).Error)
	oldDownload := model.Download{
		SubscriptionID: sub.ID, Title: "old", Episode: 1,
		TorrentURL: "magnet:old", TorrentHash: "old-hash", Status: model.DownloadStatusFailed,
		RetryCount: 3, MaxRetries: 3,
	}
	require.NoError(t, db.Create(&oldDownload).Error)
	otherDownload := model.Download{
		SubscriptionID: sub.ID, Title: "other", Episode: 1,
		TorrentURL: "magnet:other", TorrentHash: "other-hash", Status: model.DownloadStatusDownloading,
	}
	require.NoError(t, db.Create(&otherDownload).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &otherDownload.ID,
		ActiveTorrentHash: otherDownload.TorrentHash,
	}
	require.NoError(t, db.Create(&ledger).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	qb := &mockQBittorrentClient{}
	handler := NewSubscriptionDiagnosticsHandler(
		repository.NewSubscriptionRepository(db),
		repository.NewSubscriptionFeedRepository(db),
		downloadRepo,
		nil,
		qb,
		t.TempDir(),
		episodeservice.NewService(episodeRepo),
	)

	err = handler.retryDownload(&sub, &oldDownload)
	require.Error(t, err)
	require.False(t, qb.deleteWithPayloadCalled)
	persisted, reloadErr := downloadRepo.GetByID(oldDownload.ID)
	require.NoError(t, reloadErr)
	require.Equal(t, model.DownloadStatusFailed, persisted.Status)
	require.Equal(t, "old-hash", persisted.TorrentHash)
	after, reloadErr := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, reloadErr)
	require.NotNil(t, after.ActiveDownloadID)
	require.Equal(t, otherDownload.ID, *after.ActiveDownloadID)
}

func setupSubscriptionDiagnosticsTest(t *testing.T) (*gorm.DB, repository.SubscriptionRepository, repository.DownloadRepository, repository.ConfigRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
	return db,
		repository.NewSubscriptionRepository(db),
		repository.NewDownloadRepository(db),
		repository.NewConfigRepository(db)
}

func performSubscriptionDiagnosticsRequest(r http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func decodeSubscriptionDiagnosticCheck(t *testing.T, w *httptest.ResponseRecorder) SubscriptionDiagnosticCheckResponse {
	t.Helper()
	var resp struct {
		Data SubscriptionDiagnosticCheckResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Data
}

func retryFailedSubscriptionDiagnostics(t *testing.T, handler *SubscriptionDiagnosticsHandler) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.POST("/subscriptions/:id/diagnostics/retry-failed", handler.RetryFailed)
	return performSubscriptionDiagnosticsRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/retry-failed")
}

type subscriptionDiagnosticsQBClient struct {
	version            string
	torrents           []*downloader.TorrentInfo
	listErr            error
	removeErr          error
	addErr             error
	addHash            string
	listCalls          int
	infoCalls          int
	removeCalls        int
	deletePayloadCalls int
	addCalls           int
}

func (q *subscriptionDiagnosticsQBClient) Login(string, string, string) error { return nil }
func (q *subscriptionDiagnosticsQBClient) TestConnection(string, string, string) error {
	return nil
}
func (q *subscriptionDiagnosticsQBClient) AddTorrent(string, string, string) (string, error) {
	q.addCalls++
	if q.addErr != nil {
		return "", q.addErr
	}
	if q.addHash == "" {
		return "new-hash", nil
	}
	return q.addHash, nil
}
func (q *subscriptionDiagnosticsQBClient) AddTorrentExclusive(url, savePath, category, _ string) (string, error) {
	return q.AddTorrent(url, savePath, category)
}
func (q *subscriptionDiagnosticsQBClient) AddTorrentFile(string, []byte, string, string) (string, error) {
	return "", nil
}
func (q *subscriptionDiagnosticsQBClient) GetTorrentInfo(string) (*downloader.TorrentInfo, error) {
	q.infoCalls++
	return nil, errors.New("not found")
}
func (q *subscriptionDiagnosticsQBClient) GetTorrentsByCategory(string) ([]*downloader.TorrentInfo, error) {
	q.listCalls++
	return q.torrents, q.listErr
}
func (q *subscriptionDiagnosticsQBClient) SetCategory(string, string) error { return nil }
func (q *subscriptionDiagnosticsQBClient) SetLocation(string, string) error { return nil }
func (q *subscriptionDiagnosticsQBClient) RenameTorrentFile(string, string, string) error {
	return nil
}
func (q *subscriptionDiagnosticsQBClient) PauseTorrent(string) error  { return nil }
func (q *subscriptionDiagnosticsQBClient) ResumeTorrent(string) error { return nil }
func (q *subscriptionDiagnosticsQBClient) RemoveTorrentTask(string) error {
	q.removeCalls++
	return q.removeErr
}
func (q *subscriptionDiagnosticsQBClient) DeleteTorrentWithPayload(string) error {
	q.deletePayloadCalls++
	return nil
}
func (q *subscriptionDiagnosticsQBClient) GetTorrentFiles(string) ([]downloader.TorrentFile, error) {
	return nil, nil
}
func (q *subscriptionDiagnosticsQBClient) GetVersion() (string, error) { return q.version, nil }
func (q *subscriptionDiagnosticsQBClient) SetProxy(string) error       { return nil }
func (q *subscriptionDiagnosticsQBClient) DownloadTorrentFile(string) ([]byte, error) {
	return nil, nil
}
