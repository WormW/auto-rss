package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/medialibrary"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMediaLibraryHandlerTest(t *testing.T) (*gin.Engine, repository.DownloadRepository, *medialibrary.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	configRepo := repository.NewConfigRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	service := medialibrary.NewService(configRepo, downloadRepo)
	handler := NewMediaLibraryHandler(service, downloadRepo, subscriptionRepo)

	router := gin.New()
	router.GET("/media-library/config", handler.GetConfig)
	router.PUT("/media-library/config", handler.SaveConfig)
	router.POST("/media-library/downloads/:id/refresh", handler.RefreshDownload)

	return router, downloadRepo, service
}

func TestMediaLibraryHandler_SaveConfigRedactsToken(t *testing.T) {
	router, _, _ := setupMediaLibraryHandlerTest(t)

	body := bytes.NewBufferString(`{
		"enabled": true,
		"provider": "jellyfin",
		"base_url": "http://jellyfin.local",
		"token": "secret-token",
		"refresh_on_import": true,
		"path_mappings": [{"from": "/downloads", "to": "/media"}]
	}`)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/media-library/config", body)
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-token") {
		t.Fatalf("response should not contain token: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"token_configured":true`) {
		t.Fatalf("response should indicate configured token: %s", recorder.Body.String())
	}
}

func TestMediaLibraryHandler_RefreshDownloadReportsMappingError(t *testing.T) {
	router, downloadRepo, service := setupMediaLibraryHandlerTest(t)
	if err := service.SaveConfig(medialibrary.Config{
		Enabled:         true,
		Provider:        medialibrary.ProviderJellyfin,
		BaseURL:         "http://jellyfin.local",
		Token:           "secret-token",
		RefreshOnImport: true,
		PathMappings: []medialibrary.PathMapping{
			{From: "/downloads", To: "/media"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	download := &model.Download{
		Title:      "Test Episode",
		FilePath:   "/other/show/ep01.mkv",
		Status:     model.DownloadStatusCompleted,
		TorrentURL: "http://example.com/test.torrent",
	}
	if err := downloadRepo.Create(download); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/media-library/downloads/1/refresh", nil)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}

	updated, err := downloadRepo.GetByID(download.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if updated.MediaLibraryRefreshStatus != medialibrary.RefreshStatusFailed {
		t.Fatalf("MediaLibraryRefreshStatus = %q", updated.MediaLibraryRefreshStatus)
	}
	if updated.MediaLibraryRefreshError == "" {
		t.Fatal("expected refresh error to be persisted")
	}
}
