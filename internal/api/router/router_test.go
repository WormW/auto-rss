package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/api/handler"
	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/recovery"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/scheduler"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		&model.SubscriptionFeed{},
		&model.SubscriptionFeedSeenItem{},
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
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	return db
}

func TestSetupRegistersSubscriptionFeedRoutes(t *testing.T) {
	router, _, _ := setupRouterForTestWithConfig(t, false, nil)
	want := map[string]bool{
		http.MethodGet + " /api/v1/subscriptions/:id/feeds":                  false,
		http.MethodPost + " /api/v1/subscriptions/:id/feeds":                 false,
		http.MethodPut + " /api/v1/subscriptions/:id/feeds/:feedId":          false,
		http.MethodDelete + " /api/v1/subscriptions/:id/feeds/:feedId":       false,
		http.MethodPost + " /api/v1/subscriptions/:id/feeds/preview":         false,
		http.MethodPost + " /api/v1/subscriptions/:id/feeds/:feedId/preview": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, registered := range want {
		if !registered {
			t.Errorf("subscription feed route not registered: %s", route)
		}
	}
}

func TestSetupRegistersEpisodeLedgerManagementRoutes(t *testing.T) {
	router, _, _ := setupRouterForTestWithConfig(t, false, nil)
	want := map[string]bool{
		http.MethodGet + " /api/v1/subscriptions/:id/episodes":                                                  false,
		http.MethodPut + " /api/v1/subscriptions/:id/episodes/status":                                           false,
		http.MethodGet + " /api/v1/subscriptions/:id/episodes/:episode/candidates":                              false,
		http.MethodPost + " /api/v1/subscriptions/:id/episodes/:episode/candidates/:candidate_id/keep":          false,
		http.MethodPost + " /api/v1/subscriptions/:id/episodes/:episode/candidates/:candidate_id/replace":       false,
		http.MethodPost + " /api/v1/subscriptions/:id/episodes/:episode/candidates/:candidate_id/retry-cleanup": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, registered := range want {
		if !registered {
			t.Errorf("episode management route not registered: %s", route)
		}
	}
}

func TestSetupStartsReplacementRecoveryAsynchronouslyOnce(t *testing.T) {
	db := newTestDB(t)
	cfg := newRouterTestConfig(false)
	qbClient := downloader.NewQBittorrentClient()
	appCtx := newTestAppContext(db, cfg)

	original := newScheduler
	newScheduler = func(*gorm.DB, repository.SubscriptionRepository, repository.DownloadRepository, repository.ConfigRepository, string, rss.Parser, downloader.QBittorrentClient, *episode.Service) scheduler.Scheduler {
		return &mockScheduler{}
	}
	defer func() { newScheduler = original }()

	var calls atomic.Int32
	started := make(chan struct{})
	recovery := func(ctx context.Context) (int, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		return 3, ctx.Err()
	}

	type setupResult struct {
		router *gin.Engine
		err    error
	}
	setupDone := make(chan setupResult, 1)
	go func() {
		r, err := setup(db, cfg, qbClient, appCtx, "", setupDependencies{replacementRecovery: recovery})
		setupDone <- setupResult{router: r, err: err}
	}()

	select {
	case result := <-setupDone:
		require.NoError(t, result.err)
		require.NotNil(t, result.router)
	case <-time.After(2 * time.Second):
		t.Fatal("router setup blocked on replacement recovery")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("replacement recovery was not started")
	}
	assert.EqualValues(t, 1, calls.Load())

	shutdownDone := make(chan struct{})
	go func() {
		appCtx.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel replacement recovery")
	}
	assert.EqualValues(t, 1, calls.Load())
}

func TestStartReplacementRecoveryShutdownTimeoutIsBounded(t *testing.T) {
	db := newTestDB(t)
	cfg := newRouterTestConfig(false)
	appCtx := newTestAppContext(db, cfg)
	started := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	recovery := func(context.Context) (int, error) {
		close(started)
		<-release
		close(exited)
		return 0, nil
	}
	startReplacementRecovery(appCtx, recovery, 25*time.Millisecond)
	<-started

	shutdownDone := make(chan struct{})
	go func() {
		appCtx.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		close(release)
		<-exited
		t.Fatal("shutdown blocked past the replacement recovery timeout")
	}
	close(release)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("replacement recovery did not exit after blocker release")
	}
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
	return setupRouterForTestWithConfig(t, authEnabled, nil)
}

func setupRouterForTestWithConfig(t *testing.T, authEnabled bool, configure func(*config.Config)) (*gin.Engine, *app.Context, *gorm.DB) {
	t.Helper()

	db := newTestDB(t)
	cfg := newRouterTestConfig(authEnabled)
	if configure != nil {
		configure(cfg)
	}
	qbClient := downloader.NewQBittorrentClient()
	appCtx := newTestAppContext(db, cfg)

	original := newScheduler
	newScheduler = func(*gorm.DB, repository.SubscriptionRepository, repository.DownloadRepository, repository.ConfigRepository, string, rss.Parser, downloader.QBittorrentClient, *episode.Service) scheduler.Scheduler {
		return &mockScheduler{}
	}
	t.Cleanup(func() { newScheduler = original })
	t.Cleanup(appCtx.Shutdown)

	r, err := setup(db, cfg, qbClient, appCtx, "", setupDependencies{
		replacementRecovery: func(context.Context) (int, error) { return 0, nil },
	})
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
	newScheduler = func(*gorm.DB, repository.SubscriptionRepository, repository.DownloadRepository, repository.ConfigRepository, string, rss.Parser, downloader.QBittorrentClient, *episode.Service) scheduler.Scheduler {
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

func TestSetup_MCPDisabledByDefaultDoesNotMountEndpoint(t *testing.T) {
	r, _, _ := setupRouterForTestWithConfig(t, false, nil)

	for _, route := range r.Routes() {
		if route.Path == "/mcp" {
			t.Fatalf("expected /mcp route to be absent when MCP is disabled, got route %#v", route)
		}
	}
}

func TestSetup_MCPEnabledRejectsMissingAndInvalidBearerToken(t *testing.T) {
	r, _, _ := setupRouterForTestWithConfig(t, false, func(cfg *config.Config) {
		cfg.MCPEnabled = true
		cfg.MCPToken = "secret-token"
		cfg.MCPAllowedOrigins = []string{"http://localhost:7892"}
	})

	for _, tc := range []struct {
		name          string
		authorization string
	}{
		{name: "missing bearer token"},
		{name: "invalid bearer token", authorization: "Bearer wrong-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := performRouterMCPRequest(r, http.MethodPost, "", tc.authorization, mcpInitializeRequestBody())

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected unauthorized MCP request to return 401, got %d: %s", recorder.Code, recorder.Body.String())
			}
			if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer realm="auto-rss-mcp"` {
				t.Fatalf("WWW-Authenticate = %q, want MCP bearer challenge", got)
			}
		})
	}
}

func TestSetup_MCPEnabledRejectsDisallowedBrowserOrigin(t *testing.T) {
	r, _, _ := setupRouterForTestWithConfig(t, false, func(cfg *config.Config) {
		cfg.MCPEnabled = true
		cfg.MCPToken = "secret-token"
		cfg.MCPAllowedOrigins = []string{"http://localhost:7892"}
	})

	recorder := performRouterMCPRequest(r, http.MethodPost, "http://evil.example", "Bearer secret-token", mcpInitializeRequestBody())

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected disallowed MCP origin to return 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Header().Get("Access-Control-Allow-Origin"), "evil.example") {
		t.Fatalf("disallowed origin was echoed in CORS headers: %q", recorder.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestSetup_MCPEnabledAllowsConfiguredOriginPreflight(t *testing.T) {
	r, _, _ := setupRouterForTestWithConfig(t, false, func(cfg *config.Config) {
		cfg.MCPEnabled = true
		cfg.MCPToken = "secret-token"
		cfg.MCPAllowedOrigins = []string{"http://localhost:7892"}
	})

	recorder := performRouterMCPRequest(r, http.MethodOptions, "http://localhost:7892", "Bearer secret-token", "")

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected allowed MCP preflight to return 204, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:7892" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want configured origin", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) || !strings.Contains(got, http.MethodOptions) {
		t.Fatalf("Access-Control-Allow-Methods = %q, want POST and OPTIONS", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") || !strings.Contains(got, "MCP-Protocol-Version") {
		t.Fatalf("Access-Control-Allow-Headers = %q, want MCP auth/protocol headers", got)
	}
	if got := recorder.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestSetup_MCPEnabledRoutesAuthorizedInitializeRequest(t *testing.T) {
	r, _, _ := setupRouterForTestWithConfig(t, false, func(cfg *config.Config) {
		cfg.MCPEnabled = true
		cfg.MCPToken = "secret-token"
		cfg.MCPAllowedOrigins = []string{"http://localhost:7892"}
	})

	recorder := performRouterMCPRequest(r, http.MethodPost, "http://localhost:7892", "Bearer secret-token", mcpInitializeRequestBody())

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected authorized MCP initialize request to reach handler, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse MCP initialize response: %v; body=%s", err, recorder.Body.String())
	}
	if response.Result.ServerInfo.Name != "auto-rss" || response.Result.ProtocolVersion == "" {
		t.Fatalf("unexpected MCP initialize response: %#v", response)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:7892" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want configured origin", got)
	}
}

func TestSetup_AuthEnabledProtectsPhase7Routes(t *testing.T) {
	r, _ := setupRouterForTest(t, true)

	routes := []struct {
		name   string
		method string
		path   string
	}{
		{name: "tag list", method: http.MethodGet, path: "/api/v1/tags"},
		{name: "tag create", method: http.MethodPost, path: "/api/v1/tags"},
		{name: "download history", method: http.MethodGet, path: "/api/v1/downloads/history"},
		{name: "download statistics", method: http.MethodGet, path: "/api/v1/downloads/statistics"},
		{name: "rss health aggregate", method: http.MethodGet, path: "/api/v1/rss/health"},
		{name: "rss health single", method: http.MethodGet, path: "/api/v1/rss/health/1"},
		{name: "rss dead", method: http.MethodGet, path: "/api/v1/rss/dead"},
		{name: "rss health trigger", method: http.MethodPost, path: "/api/v1/rss/health-check"},
		{name: "recovery scan", method: http.MethodPost, path: "/api/v1/recovery/scan"},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(route.method, route.path, nil)
			r.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected unauthenticated %s %s to return 401, got %d: %s", route.method, route.path, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestSetup_RoutesRecoveryScanValidationThroughAPIGroup(t *testing.T) {
	r, _, _ := setupRouterForTestWithDB(t, false)

	recorder := performRouterJSONRequest(r, http.MethodPost, "/api/v1/recovery/scan", `{`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid recovery scan JSON to be rejected through router, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse recovery validation response: %v", err)
	}
	if response.Code != 400 || response.Message == "" {
		t.Fatalf("unexpected recovery validation response: %#v", response)
	}
}

func TestSetup_RoutesRecoveryScanDryRunWithIsolatedFixtures(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)
	sub, existing, missing := seedRouterRecoveryFixture(t, db)

	recorder := performRouterJSONRequest(r, http.MethodPost, "/api/v1/recovery/scan", `{"dry_run":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected recovery dry-run route to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Code    int                 `json:"code"`
		Message string              `json:"message"`
		Data    recovery.ScanResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse recovery dry-run response: %v", err)
	}
	if response.Code != 0 || response.Message != "扫描完成" {
		t.Fatalf("unexpected recovery dry-run envelope: %#v", response)
	}

	result := response.Data
	if result.Applied || result.BackupPath != "" {
		t.Fatalf("expected dry-run route not to apply or create a backup, got applied=%t backup=%q", result.Applied, result.BackupPath)
	}
	if result.ScannedFiles != 3 || result.MatchedFiles != 2 || len(result.OrphanFiles) != 1 {
		t.Fatalf("unexpected recovery scan counts: scanned=%d matched=%d orphan=%d", result.ScannedFiles, result.MatchedFiles, len(result.OrphanFiles))
	}
	if len(result.Subscriptions) != 1 {
		t.Fatalf("expected one subscription result, got %#v", result.Subscriptions)
	}
	report := result.Subscriptions[0]
	if report.SubscriptionID != sub.ID || report.Name != "Router Recovery Show" {
		t.Fatalf("unexpected recovery subscription report: %#v", report)
	}
	if report.CurrentEpisodeOld != 1 || report.CurrentEpisodeNew != 3 || report.LatestEpisodeOld != 2 || report.LatestEpisodeNew != 3 {
		t.Fatalf("expected episode totals to be reported without applying, got %#v", report)
	}
	if !equalInts(report.EpisodesOnDisk, []int{2, 3}) {
		t.Fatalf("expected router request shape to reach scanner and report episodes [2 3], got %#v", report.EpisodesOnDisk)
	}
	if !equalUints(report.DownloadsToUpdate, []uint{existing.ID}) {
		t.Fatalf("expected existing download to be proposed for update, got %#v", report.DownloadsToUpdate)
	}
	if !equalInts(report.DownloadsToCreate, []int{3}) {
		t.Fatalf("expected episode 3 synthetic record proposal, got %#v", report.DownloadsToCreate)
	}
	if !equalUints(report.DownloadsMissing, []uint{missing.ID}) {
		t.Fatalf("expected missing completed download to be reported, got %#v", report.DownloadsMissing)
	}

	var afterSub model.Subscription
	if err := db.First(&afterSub, sub.ID).Error; err != nil {
		t.Fatalf("expected seeded subscription after dry-run: %v", err)
	}
	if afterSub.CurrentEpisode != 1 || afterSub.LatestEpisode != 2 {
		t.Fatalf("expected dry-run route not to mutate subscription, got current=%d latest=%d", afterSub.CurrentEpisode, afterSub.LatestEpisode)
	}

	var afterExisting model.Download
	if err := db.First(&afterExisting, existing.ID).Error; err != nil {
		t.Fatalf("expected seeded download after dry-run: %v", err)
	}
	if afterExisting.Status != model.DownloadStatusDownloading || afterExisting.RenamedPath != "" {
		t.Fatalf("expected dry-run route not to mutate download, got %#v", afterExisting)
	}
}

func TestSetup_RoutesRecoveryScanRejectsApplyModeByDefault(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)
	t.Setenv("AUTO_RSS_ENABLE_RECOVERY_APPLY", "")
	sub, existing, _ := seedRouterRecoveryFixture(t, db)

	recorder := performRouterJSONRequest(r, http.MethodPost, "/api/v1/recovery/scan", `{"dry_run":false}`)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected recovery apply route to be rejected by default, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse recovery apply rejection response: %v", err)
	}
	if response.Code != 403 || response.Message == "" {
		t.Fatalf("unexpected recovery apply rejection envelope: %#v", response)
	}
	if !strings.Contains(response.Message, recovery.ErrApplyDisabled.Error()) {
		t.Fatalf("expected API rejection to preserve apply gate guidance %q, got %q", recovery.ErrApplyDisabled.Error(), response.Message)
	}

	var afterSub model.Subscription
	if err := db.First(&afterSub, sub.ID).Error; err != nil {
		t.Fatalf("expected seeded subscription after rejected apply: %v", err)
	}
	if afterSub.CurrentEpisode != 1 || afterSub.LatestEpisode != 2 {
		t.Fatalf("expected rejected apply not to mutate subscription, got current=%d latest=%d", afterSub.CurrentEpisode, afterSub.LatestEpisode)
	}

	var afterExisting model.Download
	if err := db.First(&afterExisting, existing.ID).Error; err != nil {
		t.Fatalf("expected seeded download after rejected apply: %v", err)
	}
	if afterExisting.Status != model.DownloadStatusDownloading || afterExisting.RenamedPath != "" {
		t.Fatalf("expected rejected apply not to mutate download, got %#v", afterExisting)
	}
}

func TestSetup_RoutesTagValidationThroughAPIGroup(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)

	createRecorder := performRouterJSONRequest(r, http.MethodPost, "/api/v1/tags", `{"name":"router-tag","description":"from router"}`)
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

	listRecorder := performRouterRequest(r, http.MethodGet, "/api/v1/tags", nil)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected router tag list to succeed, got %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse struct {
		Code int `json:"code"`
		Data []struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("failed to parse tag list response: %v", err)
	}
	if listResponse.Code != 0 || len(listResponse.Data) != 1 || listResponse.Data[0].ID != createResponse.Data.ID {
		t.Fatalf("unexpected tag list response: %#v", listResponse)
	}

	updateRecorder := performRouterJSONRequest(r, http.MethodPut, "/api/v1/tags/"+strconv.FormatUint(uint64(createResponse.Data.ID), 10), `{"name":"router-tag-updated","color":"#abcdef","description":"updated by router"}`)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("expected router tag update to succeed, got %d: %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updateResponse struct {
		Code int `json:"code"`
		Data struct {
			ID          uint   `json:"id"`
			Name        string `json:"name"`
			Color       string `json:"color"`
			Description string `json:"description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updateResponse); err != nil {
		t.Fatalf("failed to parse tag update response: %v", err)
	}
	if updateResponse.Code != 0 || updateResponse.Data.ID != createResponse.Data.ID || updateResponse.Data.Name != "router-tag-updated" || updateResponse.Data.Color != "#abcdef" {
		t.Fatalf("unexpected tag update response: %#v", updateResponse)
	}

	duplicateRecorder := performRouterJSONRequest(r, http.MethodPost, "/api/v1/tags", `{"name":"router-tag-updated"}`)
	if duplicateRecorder.Code != http.StatusConflict {
		t.Fatalf("expected duplicate tag to be rejected through router, got %d: %s", duplicateRecorder.Code, duplicateRecorder.Body.String())
	}

	missingNameRecorder := performRouterJSONRequest(r, http.MethodPost, "/api/v1/tags", `{"description":"missing name"}`)
	if missingNameRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected tag validation error through router, got %d: %s", missingNameRecorder.Code, missingNameRecorder.Body.String())
	}

	badUpdateIDRecorder := performRouterJSONRequest(r, http.MethodPut, "/api/v1/tags/not-a-number", `{"name":"bad"}`)
	if badUpdateIDRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid tag update id through router, got %d: %s", badUpdateIDRecorder.Code, badUpdateIDRecorder.Body.String())
	}

	var tag model.SubscriptionTag
	if err := db.First(&tag, createResponse.Data.ID).Error; err != nil {
		t.Fatalf("expected created tag to persist: %v", err)
	}
	if tag.Name != "router-tag-updated" || tag.Color != "#abcdef" {
		t.Fatalf("expected updated tag to persist, got %#v", tag)
	}

	deleteRecorder := performRouterRequest(r, http.MethodDelete, "/api/v1/tags/"+strconv.FormatUint(uint64(createResponse.Data.ID), 10), nil)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("expected router tag delete to succeed, got %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	badDeleteIDRecorder := performRouterRequest(r, http.MethodDelete, "/api/v1/tags/not-a-number", nil)
	if badDeleteIDRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid tag delete id through router, got %d: %s", badDeleteIDRecorder.Code, badDeleteIDRecorder.Body.String())
	}
}

func TestSetup_RoutesSubscriptionTagValidationThroughAPIGroup(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)

	subscription := model.Subscription{Name: "Router Tagged Anime", RssURL: "https://example.com/router-tagged.xml", Season: 1, Enabled: true, Status: "active"}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}
	tagOne := model.SubscriptionTag{Name: "router-sub-tag-one", Color: "#111111", SortOrder: 20}
	tagTwo := model.SubscriptionTag{Name: "router-sub-tag-two", Color: "#222222", SortOrder: 10}
	if err := db.Create(&tagOne).Error; err != nil {
		t.Fatalf("failed to seed first tag: %v", err)
	}
	if err := db.Create(&tagTwo).Error; err != nil {
		t.Fatalf("failed to seed second tag: %v", err)
	}

	subscriptionPath := "/api/v1/subscriptions/" + strconv.FormatUint(uint64(subscription.ID), 10) + "/tags"
	addRecorder := performRouterJSONRequest(r, http.MethodPost, subscriptionPath, `{"tag_ids":[`+strconv.FormatUint(uint64(tagOne.ID), 10)+`,`+strconv.FormatUint(uint64(tagTwo.ID), 10)+`]}`)
	if addRecorder.Code != http.StatusOK {
		t.Fatalf("expected router subscription tag add to succeed, got %d: %s", addRecorder.Code, addRecorder.Body.String())
	}

	listRecorder := performRouterRequest(r, http.MethodGet, subscriptionPath, nil)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected router subscription tag list to succeed, got %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse struct {
		Code int `json:"code"`
		Data []struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("failed to parse subscription tag list response: %v", err)
	}
	if listResponse.Code != 0 || len(listResponse.Data) != 2 || listResponse.Data[0].ID != tagTwo.ID || listResponse.Data[1].ID != tagOne.ID {
		t.Fatalf("unexpected subscription tag list response: %#v", listResponse)
	}

	emptyIDsRecorder := performRouterJSONRequest(r, http.MethodPost, subscriptionPath, `{"tag_ids":[]}`)
	if emptyIDsRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected empty tag ids validation through router, got %d: %s", emptyIDsRecorder.Code, emptyIDsRecorder.Body.String())
	}

	badSubscriptionIDRecorder := performRouterJSONRequest(r, http.MethodPost, "/api/v1/subscriptions/not-a-number/tags", `{"tag_ids":[1]}`)
	if badSubscriptionIDRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid subscription id validation through router, got %d: %s", badSubscriptionIDRecorder.Code, badSubscriptionIDRecorder.Body.String())
	}

	notFoundRecorder := performRouterRequest(r, http.MethodGet, "/api/v1/subscriptions/99999/tags", nil)
	if notFoundRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected missing subscription validation through router, got %d: %s", notFoundRecorder.Code, notFoundRecorder.Body.String())
	}

	removeRecorder := performRouterRequest(r, http.MethodDelete, subscriptionPath+"/"+strconv.FormatUint(uint64(tagOne.ID), 10), nil)
	if removeRecorder.Code != http.StatusOK {
		t.Fatalf("expected router subscription tag remove to succeed, got %d: %s", removeRecorder.Code, removeRecorder.Body.String())
	}

	badTagIDRecorder := performRouterRequest(r, http.MethodDelete, subscriptionPath+"/not-a-number", nil)
	if badTagIDRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid tag id validation through router, got %d: %s", badTagIDRecorder.Code, badTagIDRecorder.Body.String())
	}

	var relationCount int64
	if err := db.Model(&model.SubscriptionTagRelation{}).Where("subscription_id = ?", subscription.ID).Count(&relationCount).Error; err != nil {
		t.Fatalf("failed to count subscription tag relations: %v", err)
	}
	if relationCount != 1 {
		t.Fatalf("expected one subscription tag relation after removal, got %d", relationCount)
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
		CreatedAt:      time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("failed to seed download: %v", err)
	}
	if err := db.Create(&model.Download{
		SubscriptionID: subscription.ID,
		Title:          "Router History Failed Episode",
		Episode:        2,
		TorrentURL:     "https://example.com/router-history-failed.torrent",
		TorrentHash:    "router-history-failed-hash",
		Status:         model.DownloadStatusFailed,
		CreatedAt:      time.Date(2026, time.June, 3, 12, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("failed to seed failed download: %v", err)
	}
	if err := db.Create(&model.Download{
		SubscriptionID: subscription.ID,
		Title:          "Router History Older Episode",
		Episode:        3,
		TorrentURL:     "https://example.com/router-history-older.torrent",
		TorrentHash:    "router-history-older-hash",
		Status:         model.DownloadStatusCompleted,
		CreatedAt:      time.Date(2026, time.June, 4, 12, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("failed to seed older download: %v", err)
	}

	historyRecorder := httptest.NewRecorder()
	historyReq := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/history?page=1&page_size=10&status=completed&subscription_id="+strconv.FormatUint(uint64(subscription.ID), 10)+"&start_date=2026-06-02&end_date=2026-06-02", nil)
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
	if historyResponse.Data.List[0].Title != "Router History Episode" {
		t.Fatalf("expected status filter to keep only completed first episode, got %#v", historyResponse.Data.List)
	}

	paginatedRecorder := httptest.NewRecorder()
	paginatedReq := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/history?page=2&page_size=1&start_date=2026-06-01&end_date=2026-06-30", nil)
	r.ServeHTTP(paginatedRecorder, paginatedReq)
	if paginatedRecorder.Code != http.StatusOK {
		t.Fatalf("expected paginated download history route to succeed, got %d: %s", paginatedRecorder.Code, paginatedRecorder.Body.String())
	}

	var paginatedResponse struct {
		Code int `json:"code"`
		Data struct {
			List     []model.Download `json:"list"`
			Total    int64            `json:"total"`
			Page     int              `json:"page"`
			PageSize int              `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(paginatedRecorder.Body.Bytes(), &paginatedResponse); err != nil {
		t.Fatalf("failed to parse paginated history response: %v", err)
	}
	if paginatedResponse.Code != 0 || paginatedResponse.Data.Total != 3 || paginatedResponse.Data.Page != 2 || paginatedResponse.Data.PageSize != 1 {
		t.Fatalf("unexpected paginated history metadata: %#v", paginatedResponse)
	}
	if len(paginatedResponse.Data.List) != 1 || paginatedResponse.Data.List[0].Title != "Router History Failed Episode" {
		t.Fatalf("expected page 2 to return second newest matching download, got %#v", paginatedResponse.Data.List)
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
	if statisticsResponse.Code != 0 || statisticsResponse.Data.TotalCount != 3 || statisticsResponse.Data.CompletedCount != 2 || statisticsResponse.Data.FailedCount != 1 {
		t.Fatalf("unexpected statistics response: %#v", statisticsResponse)
	}
}

func TestSetup_RoutesRSSHealthValidationThroughAPIGroup(t *testing.T) {
	ensureRouterNoRunningTask(t)

	r, _, db := setupRouterForTestWithDB(t, false)
	feed := newRouterRSSFeedServer(t, http.StatusOK, routerValidRSSFeed("Router Healthy Anime"))
	deadFeed := newRouterRSSFeedServer(t, http.StatusInternalServerError, "server error")

	subscription := model.Subscription{Name: "Router Healthy Anime", RssURL: feed.URL, Season: 1, Enabled: true, Status: "active"}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}
	deadSubscription := model.Subscription{Name: "Router Dead Anime", RssURL: deadFeed.URL, Season: 1, Enabled: true, Status: "active"}
	if err := db.Create(&deadSubscription).Error; err != nil {
		t.Fatalf("failed to seed dead subscription: %v", err)
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
	if allResponse.Code != 0 || allResponse.Data.Summary.Total != 2 || allResponse.Data.Summary.Healthy != 1 || allResponse.Data.Summary.Dead != 1 {
		t.Fatalf("unexpected aggregate rss health response: %#v", allResponse)
	}

	deadRecorder := httptest.NewRecorder()
	deadReq := httptest.NewRequest(http.MethodGet, "/api/v1/rss/dead", nil)
	r.ServeHTTP(deadRecorder, deadReq)
	if deadRecorder.Code != http.StatusOK {
		t.Fatalf("expected dead rss route to succeed, got %d: %s", deadRecorder.Code, deadRecorder.Body.String())
	}

	var deadResponse struct {
		Code int `json:"code"`
		Data struct {
			Count int                     `json:"count"`
			Items []rss.HealthCheckResult `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(deadRecorder.Body.Bytes(), &deadResponse); err != nil {
		t.Fatalf("failed to parse dead rss response: %v", err)
	}
	if deadResponse.Code != 0 || deadResponse.Data.Count != 1 || len(deadResponse.Data.Items) != 1 {
		t.Fatalf("unexpected dead rss response: %#v", deadResponse)
	}
	if deadResponse.Data.Items[0].SubscriptionID != deadSubscription.ID || deadResponse.Data.Items[0].Status != rss.HealthStatusDead {
		t.Fatalf("expected dead route to return seeded dead subscription, got %#v", deadResponse.Data.Items)
	}

	triggerRecorder := httptest.NewRecorder()
	triggerReq := httptest.NewRequest(http.MethodPost, "/api/v1/rss/health-check", nil)
	r.ServeHTTP(triggerRecorder, triggerReq)
	if triggerRecorder.Code != http.StatusOK {
		t.Fatalf("expected rss health trigger route to start task, got %d: %s", triggerRecorder.Code, triggerRecorder.Body.String())
	}

	var triggerResponse struct {
		Code int                          `json:"code"`
		Data handler.TriggerCheckResponse `json:"data"`
	}
	if err := json.Unmarshal(triggerRecorder.Body.Bytes(), &triggerResponse); err != nil {
		t.Fatalf("failed to parse trigger response: %v", err)
	}
	if triggerResponse.Code != 0 || triggerResponse.Data.TaskID == "" || triggerResponse.Data.Status != string(task.TaskStatusRunning) {
		t.Fatalf("unexpected trigger response: %#v", triggerResponse)
	}
	ensureRouterNoRunningTask(t)
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

func TestOnboardingStatusDoesNotProbeDownloadDirectoryWriteAccess(t *testing.T) {
	r, _, db := setupRouterForTestWithDB(t, false)
	downloadPath := t.TempDir()
	seedCompleteOnboardingSetupWithDownloadPath(t, db, downloadPath)

	beforeEntries, err := os.ReadDir(downloadPath)
	if err != nil {
		t.Fatalf("failed to read download path before status request: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/status", nil)
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected onboarding status to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}

	afterEntries, err := os.ReadDir(downloadPath)
	if err != nil {
		t.Fatalf("failed to read download path after status request: %v", err)
	}
	if len(afterEntries) != len(beforeEntries) {
		t.Fatalf("expected onboarding status not to create write probe files, before=%d after=%d", len(beforeEntries), len(afterEntries))
	}

	var response struct {
		Data struct {
			DownloadPath struct {
				Writable bool `json:"writable"`
			} `json:"download_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse onboarding response: %v", err)
	}
	if !response.Data.DownloadPath.Writable {
		t.Fatalf("expected existing directory to remain usable in status response")
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
	seedCompleteOnboardingSetupWithDownloadPath(t, db, t.TempDir())
}

func seedCompleteOnboardingSetupWithDownloadPath(t *testing.T, db *gorm.DB, downloadPath string) {
	t.Helper()

	configs := []model.Config{
		{Key: "qbittorrent_host", Value: "http://localhost:8080"},
		{Key: "qbittorrent_username", Value: "admin"},
		{Key: "qbittorrent_password", Value: "secret"},
		{Key: "download_path", Value: downloadPath},
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

func seedRouterRecoveryFixture(t *testing.T, db *gorm.DB) (model.Subscription, model.Download, model.Download) {
	t.Helper()

	root := t.TempDir()
	requireRouterFixtureFile(t, filepath.Join(root, "Router Recovery Show", "Season 1", "Router Recovery Show S01E02.mkv"))
	requireRouterFixtureFile(t, filepath.Join(root, "Router Recovery Show", "Season 1", "Router Recovery Show S01E03.mkv"))
	requireRouterFixtureFile(t, filepath.Join(root, "Unknown Router Show", "Unknown Router Show S01E01.mkv"))

	if err := db.Create(&model.Config{Key: "download_path", Value: root}).Error; err != nil {
		t.Fatalf("failed to seed recovery download_path config: %v", err)
	}

	sub := model.Subscription{
		Name:           "Router Recovery Show",
		RssURL:         "https://example.com/router-recovery.xml",
		Season:         1,
		CurrentEpisode: 1,
		LatestEpisode:  2,
		Enabled:        true,
		Status:         "active",
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("failed to seed recovery subscription: %v", err)
	}

	existing := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Router Recovery Show 02",
		Episode:        2,
		TorrentURL:     "memory://router-existing",
		TorrentHash:    "router-existing-02",
		Status:         model.DownloadStatusDownloading,
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("failed to seed existing recovery download: %v", err)
	}

	missing := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Router Recovery Show 04",
		Episode:        4,
		TorrentURL:     "memory://router-missing",
		TorrentHash:    "router-missing-04",
		RenamedPath:    filepath.Join(root, "Router Recovery Show", "Season 1", "Router Recovery Show S01E04.mkv"),
		Status:         model.DownloadStatusCompleted,
	}
	if err := db.Create(&missing).Error; err != nil {
		t.Fatalf("failed to seed missing recovery download: %v", err)
	}

	return sub, existing, missing
}

func requireRouterFixtureFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
		t.Fatalf("failed to create fixture file: %v", err)
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

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalUints(got, want []uint) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func performRouterRequest(r http.Handler, method, target string, body *bytes.Buffer) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	if body == nil {
		body = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, target, body)
	r.ServeHTTP(recorder, req)
	return recorder
}

func performRouterJSONRequest(r http.Handler, method, target, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(recorder, req)
	return recorder
}

func performRouterMCPRequest(r http.Handler, method, origin, authorization, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	r.ServeHTTP(recorder, req)
	return recorder
}

func mcpInitializeRequestBody() string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
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

func ensureRouterNoRunningTask(t *testing.T) {
	t.Helper()

	manager := task.GetManager()
	if manager.IsRunning() {
		_ = manager.CancelTask()
	}

	deadline := time.Now().Add(time.Second)
	for manager.IsRunning() {
		if time.Now().After(deadline) {
			t.Fatalf("expected no running task")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
