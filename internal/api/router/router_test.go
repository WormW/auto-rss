package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	return r, appCtx
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
