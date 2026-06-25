package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/api/handler"
	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/scheduler"
	"github.com/robfig/cron/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mockScheduler struct{ startErr error }

func (m *mockScheduler) Start() error { return m.startErr }
func (m *mockScheduler) Stop()        {}
func (m *mockScheduler) AddJob(string, func()) (cron.EntryID, error) {
	return 0, nil
}
func (m *mockScheduler) RunRSSCheckNow() error { return nil }

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Subscription{},
		&model.SubscriptionTag{},
		&model.SubscriptionTagRelation{},
		&model.Download{},
		&model.Config{},
		&model.DiskSample{},
		&model.DiskCleanupRecord{},
		&model.RSSSource{},
		&model.Log{},
		&model.RefreshToken{},
		&model.Notification{},
		&model.NotificationSetting{},
	); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	return db
}

func newRouterTestConfig(authEnabled bool) *config.Config {
	return &config.Config{
		DBPath:                         ":memory:",
		QBHost:                         "http://localhost:8080",
		RSSInterval:                    "30m",
		BlockAPIBootOnSchedulerFailure: true,
		ServerPort:                     7892,
		DownloadPath:                   "/tmp",
		AuthEnabled:                    authEnabled,
		JWTSecret:                      "0123456789abcdef0123456789abcdef",
		JWTAccessTokenExpiry:           time.Minute,
		JWTRefreshTokenExpiry:          time.Hour,
		JWTUsername:                    "admin",
		JWTPassword:                    "strong-password",
		RateLimit: config.RateLimitConfig{
			RPS:        1000,
			Burst:      1000,
			AuthRPM:    1000,
			MaxEntries: 10000,
			TTL:        time.Hour,
		},
	}
}

func newTestAppContext(db *gorm.DB, cfg *config.Config) *app.Context {
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	return app.NewContext(db, cfg, subscriptionRepo, downloadRepo, bangumi.NewBangumiService())
}

func setupRouterForTest(t *testing.T, authEnabled bool) (http.Handler, *app.Context) {
	r, appCtx, _ := setupRouterForTestWithDB(t, authEnabled)
	return r, appCtx
}

func setupRouterForTestWithDB(t *testing.T, authEnabled bool) (http.Handler, *app.Context, *gorm.DB) {
	t.Helper()

	db := newTestDB(t)
	cfg := newRouterTestConfig(authEnabled)
	qbClient := downloader.NewQBittorrentClient()
	appCtx := newTestAppContext(db, cfg)

	original := newScheduler
	newScheduler = func(*gorm.DB, repository.SubscriptionRepository, repository.DownloadRepository, repository.ConfigRepository, string, rss.Parser, downloader.QBittorrentClient) scheduler.Scheduler {
		return &mockScheduler{}
	}
	t.Cleanup(func() { newScheduler = original })
	t.Cleanup(appCtx.Shutdown)

	r, err := Setup(db, cfg, qbClient, appCtx, "")
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}
	return r, appCtx, db
}

func TestSetup_ReturnsErrorWhenSchedulerStartFailsAndBlockingEnabled(t *testing.T) {
	db := newTestDB(t)
	cfg := newRouterTestConfig(false)
	qbClient := downloader.NewQBittorrentClient()

	original := newScheduler
	newScheduler = func(*gorm.DB, repository.SubscriptionRepository, repository.DownloadRepository, repository.ConfigRepository, string, rss.Parser, downloader.QBittorrentClient) scheduler.Scheduler {
		return &mockScheduler{startErr: errors.New("boom")}
	}
	defer func() { newScheduler = original }()

	appCtx := newTestAppContext(db, cfg)
	defer appCtx.Shutdown()

	_, err := Setup(db, cfg, qbClient, appCtx, "")
	if err == nil {
		t.Fatalf("expected error when scheduler start fails")
	}
}

func TestSetup_AuthEnabledProtectsFeatureRoutesAndLeavesAuthRoutesPublic(t *testing.T) {
	r, _ := setupRouterForTest(t, true)

	statusRecorder := httptest.NewRecorder()
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	r.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected auth status to remain public, got %d: %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	protectedRecorder := httptest.NewRecorder()
	protectedReq := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	r.ServeHTTP(protectedRecorder, protectedReq)
	if protectedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated protected route to return 401, got %d: %s", protectedRecorder.Code, protectedRecorder.Body.String())
	}
}

func TestSetup_AuthEnabledLoginAllowsProtectedRoutes(t *testing.T) {
	r, _ := setupRouterForTest(t, true)

	loginRecorder := httptest.NewRecorder()
	loginReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		bytes.NewBufferString(`{"username":"admin","password":"strong-password"}`),
	)
	loginReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(loginRecorder, loginReq)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("expected login to succeed, got %d: %s", loginRecorder.Code, loginRecorder.Body.String())
	}

	var loginResponse struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("failed to parse login response: %v", err)
	}
	accessToken := loginResponse.Data.AccessToken
	if accessToken == "" {
		t.Fatalf("expected login response to include access_token: %s", loginRecorder.Body.String())
	}

	protectedRecorder := httptest.NewRecorder()
	protectedReq := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	protectedReq.Header.Set("Authorization", "Bearer "+accessToken)
	r.ServeHTTP(protectedRecorder, protectedReq)
	if protectedRecorder.Code != http.StatusOK {
		t.Fatalf("expected authenticated protected route to succeed, got %d: %s", protectedRecorder.Code, protectedRecorder.Body.String())
	}
}

func TestSetup_AuthDisabledKeepsLocalRoutesAccessible(t *testing.T) {
	r, _ := setupRouterForTest(t, false)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected local no-auth route to remain accessible, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSetup_RoutesTagValidationThroughAPIGroup(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/tags", bytes.NewBufferString(`{"name":"router-tag","description":"from router"}`))
	createReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("expected router tag create to succeed, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	var createResponse struct {
		Code int `json:"code"`
		Data struct {
			ID          uint   `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createResponse); err != nil {
		t.Fatalf("failed to parse tag create response: %v", err)
	}
	if createResponse.Code != 0 || createResponse.Data.Name != "router-tag" || createResponse.Data.Description != "from router" {
		t.Fatalf("unexpected tag create response: %#v", createResponse)
	}

	duplicateRecorder := httptest.NewRecorder()
	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/v1/tags", bytes.NewBufferString(`{"name":"router-tag"}`))
	duplicateReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(duplicateRecorder, duplicateReq)
	if duplicateRecorder.Code != http.StatusConflict {
		t.Fatalf("expected duplicate tag to be rejected through router, got %d: %s", duplicateRecorder.Code, duplicateRecorder.Body.String())
	}

	missingNameRecorder := httptest.NewRecorder()
	missingNameReq := httptest.NewRequest(http.MethodPost, "/api/v1/tags", bytes.NewBufferString(`{"description":"missing name"}`))
	missingNameReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(missingNameRecorder, missingNameReq)
	if missingNameRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected tag validation error through router, got %d: %s", missingNameRecorder.Code, missingNameRecorder.Body.String())
	}

	var tag model.SubscriptionTag
	if err := db.First(&tag, createResponse.Data.ID).Error; err != nil {
		t.Fatalf("expected created tag to persist: %v", err)
	}
}

func TestSetup_RoutesDownloadHistoryAndStatisticsValidationThroughAPIGroup(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)

	subscription := model.Subscription{Name: "Router History Anime", RssURL: "https://example.com/router-history.xml", Season: 1, Enabled: true, Status: "active"}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}
	if err := db.Create(&model.Download{
		SubscriptionID: subscription.ID,
		Title:          "Router History Episode",
		Episode:        1,
		TorrentURL:     "https://example.com/router-history.torrent",
		TorrentHash:    "router-history-hash",
		Status:         model.DownloadStatusCompleted,
	}).Error; err != nil {
		t.Fatalf("failed to seed download: %v", err)
	}

	historyRecorder := httptest.NewRecorder()
	historyReq := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/history?page=1&page_size=10&status=completed&subscription_id="+strconv.FormatUint(uint64(subscription.ID), 10), nil)
	r.ServeHTTP(historyRecorder, historyReq)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("expected download history route to succeed, got %d: %s", historyRecorder.Code, historyRecorder.Body.String())
	}

	var historyResponse struct {
		Code int `json:"code"`
		Data struct {
			List     []model.Download `json:"list"`
			Total    int64            `json:"total"`
			Page     int              `json:"page"`
			PageSize int              `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(historyRecorder.Body.Bytes(), &historyResponse); err != nil {
		t.Fatalf("failed to parse history response: %v", err)
	}
	if historyResponse.Code != 0 || historyResponse.Data.Total != 1 || len(historyResponse.Data.List) != 1 {
		t.Fatalf("unexpected history response: %#v", historyResponse)
	}
	if historyResponse.Data.Page != 1 || historyResponse.Data.PageSize != 10 {
		t.Fatalf("expected router query params to reach history handler, got page=%d page_size=%d", historyResponse.Data.Page, historyResponse.Data.PageSize)
	}

	invalidDateRecorder := httptest.NewRecorder()
	invalidDateReq := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/history?start_date=2026/06/01", nil)
	r.ServeHTTP(invalidDateRecorder, invalidDateReq)
	if invalidDateRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid date validation error through router, got %d: %s", invalidDateRecorder.Code, invalidDateRecorder.Body.String())
	}

	statisticsRecorder := httptest.NewRecorder()
	statisticsReq := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/statistics?days=0", nil)
	r.ServeHTTP(statisticsRecorder, statisticsReq)
	if statisticsRecorder.Code != http.StatusOK {
		t.Fatalf("expected download statistics route to succeed, got %d: %s", statisticsRecorder.Code, statisticsRecorder.Body.String())
	}

	var statisticsResponse struct {
		Code int                           `json:"code"`
		Data repository.DownloadStatistics `json:"data"`
	}
	if err := json.Unmarshal(statisticsRecorder.Body.Bytes(), &statisticsResponse); err != nil {
		t.Fatalf("failed to parse statistics response: %v", err)
	}
	if statisticsResponse.Code != 0 || statisticsResponse.Data.TotalCount != 1 || statisticsResponse.Data.CompletedCount != 1 {
		t.Fatalf("unexpected statistics response: %#v", statisticsResponse)
	}
}

func TestSetup_RoutesRSSHealthValidationThroughAPIGroup(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)
	feed := newRouterRSSFeedServer(t, http.StatusOK, routerValidRSSFeed("Router Healthy Anime"))

	subscription := model.Subscription{Name: "Router Healthy Anime", RssURL: feed.URL, Season: 1, Enabled: true, Status: "active"}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rss/health/"+strconv.FormatUint(uint64(subscription.ID), 10), nil)
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected rss health route to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Code int                   `json:"code"`
		Data rss.HealthCheckResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse rss health response: %v", err)
	}
	if response.Code != 0 || response.Data.SubscriptionID != subscription.ID || response.Data.Status != rss.HealthStatusHealthy {
		t.Fatalf("unexpected rss health response: %#v", response)
	}

	badIDRecorder := httptest.NewRecorder()
	badIDReq := httptest.NewRequest(http.MethodGet, "/api/v1/rss/health/not-a-number", nil)
	r.ServeHTTP(badIDRecorder, badIDReq)
	if badIDRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid subscription id validation error through router, got %d: %s", badIDRecorder.Code, badIDRecorder.Body.String())
	}

	allRecorder := httptest.NewRecorder()
	allReq := httptest.NewRequest(http.MethodGet, "/api/v1/rss/health", nil)
	r.ServeHTTP(allRecorder, allReq)
	if allRecorder.Code != http.StatusOK {
		t.Fatalf("expected aggregate rss health route to succeed, got %d: %s", allRecorder.Code, allRecorder.Body.String())
	}

	var allResponse struct {
		Code int `json:"code"`
		Data struct {
			Summary handler.HealthCheckSummary `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(allRecorder.Body.Bytes(), &allResponse); err != nil {
		t.Fatalf("failed to parse aggregate rss health response: %v", err)
	}
	if allResponse.Code != 0 || allResponse.Data.Summary.Total != 1 || allResponse.Data.Summary.Healthy != 1 {
		t.Fatalf("unexpected aggregate rss health response: %#v", allResponse)
	}
}

func TestOnboardingStatusReportsMissingSetup(t *testing.T) {
	r, _ := setupRouterForTest(t, false)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/status", nil)
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected onboarding status to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Code int `json:"code"`
		Data struct {
			Completed  bool     `json:"completed"`
			ShouldShow bool     `json:"should_show"`
			Missing    []string `json:"missing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse onboarding response: %v", err)
	}
	if response.Code != 0 {
		t.Fatalf("expected code 0, got %d", response.Code)
	}
	if response.Data.Completed {
		t.Fatalf("expected onboarding to be incomplete")
	}
	if !response.Data.ShouldShow {
		t.Fatalf("expected onboarding to show when setup is missing")
	}
	if len(response.Data.Missing) == 0 {
		t.Fatalf("expected missing setup keys")
	}
}

func TestOnboardingStatusRequiresUsableDownloadDirectory(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)

	filePath := t.TempDir() + "/not-a-directory"
	if err := os.WriteFile(filePath, []byte("not a directory"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := db.Create(&model.Config{Key: "download_path", Value: filePath}).Error; err != nil {
		t.Fatalf("failed to seed download_path config: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/status", nil)
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected onboarding status to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Data struct {
			Missing      []string `json:"missing"`
			DownloadPath struct {
				IsDir    bool `json:"is_dir"`
				Writable bool `json:"writable"`
			} `json:"download_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse onboarding response: %v", err)
	}
	if response.Data.DownloadPath.IsDir || response.Data.DownloadPath.Writable {
		t.Fatalf("expected file path not to be marked usable: %#v", response.Data.DownloadPath)
	}
	if !containsString(response.Data.Missing, "download_path") {
		t.Fatalf("expected download_path to remain missing, got %#v", response.Data.Missing)
	}
}

func TestOnboardingCompleteRejectsIncompleteSetup(t *testing.T) {
	r, _ := setupRouterForTest(t, false)

	completeRecorder := httptest.NewRecorder()
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", nil)
	r.ServeHTTP(completeRecorder, completeReq)
	if completeRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected onboarding complete to reject incomplete setup, got %d: %s", completeRecorder.Code, completeRecorder.Body.String())
	}
}

func TestOnboardingCompletePersistsStateWhenRequiredSetupComplete(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)

	seedCompleteOnboardingSetup(t, db)

	completeRecorder := httptest.NewRecorder()
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", nil)
	r.ServeHTTP(completeRecorder, completeReq)
	if completeRecorder.Code != http.StatusOK {
		t.Fatalf("expected onboarding complete to succeed, got %d: %s", completeRecorder.Code, completeRecorder.Body.String())
	}

	statusRecorder := httptest.NewRecorder()
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/status", nil)
	r.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected onboarding status to succeed, got %d: %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	var response struct {
		Data struct {
			Completed  bool `json:"completed"`
			ShouldShow bool `json:"should_show"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse onboarding response: %v", err)
	}
	if !response.Data.Completed {
		t.Fatalf("expected onboarding to be completed")
	}
	if response.Data.ShouldShow {
		t.Fatalf("expected completed onboarding not to show")
	}
}

func seedCompleteOnboardingSetup(t *testing.T, db *gorm.DB) {
	t.Helper()

	configs := []model.Config{
		{Key: "qbittorrent_host", Value: "http://localhost:8080"},
		{Key: "qbittorrent_username", Value: "admin"},
		{Key: "qbittorrent_password", Value: "secret"},
		{Key: "download_path", Value: t.TempDir()},
		{Key: "rename_template", Value: "${title}/S${seasonFormat}E${episodeFormat}"},
	}
	for _, cfg := range configs {
		if err := db.Create(&cfg).Error; err != nil {
			t.Fatalf("failed to seed config %s: %v", cfg.Key, err)
		}
	}

	source := model.RSSSource{Name: "Test RSS", BaseURL: "https://example.com/rss.xml", Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("failed to seed rss source: %v", err)
	}
	subscription := model.Subscription{Name: "Test Anime", RssURL: source.BaseURL, Season: 1, Enabled: true, Status: "active", RSSSourceID: &source.ID}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func newRouterRSSFeedServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func routerValidRSSFeed(title string) string {
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
