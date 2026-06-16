package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

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
