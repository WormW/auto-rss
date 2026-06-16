package medialibrary

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMapPath_UsesLongestMatchingPrefix(t *testing.T) {
	got, err := MapPath("/downloads/anime/show/Season 1/ep01.mkv", []PathMapping{
		{From: "/downloads", To: "/media"},
		{From: "/downloads/anime", To: "/library/anime"},
	})
	if err != nil {
		t.Fatalf("MapPath() unexpected error: %v", err)
	}

	want := "/library/anime/show/Season 1/ep01.mkv"
	if got != want {
		t.Fatalf("MapPath() = %q, want %q", got, want)
	}
}

func TestMapPath_ReturnsErrorWhenMappingMissing(t *testing.T) {
	_, err := MapPath("/other/show/ep01.mkv", []PathMapping{
		{From: "/downloads", To: "/media"},
	})
	if err == nil {
		t.Fatal("expected missing mapping error")
	}
}

func TestRefreshDownload_MapsPathTriggersRefreshAndPersistsStatus(t *testing.T) {
	var refreshed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Library/Refresh" {
			refreshed = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Config{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	configRepo := repository.NewConfigRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	service := NewService(configRepo, downloadRepo)

	cfg := Config{
		Enabled:         true,
		Provider:        ProviderJellyfin,
		BaseURL:         server.URL,
		Token:           "test-token",
		RefreshOnImport: true,
		PathMappings: []PathMapping{
			{From: "/downloads", To: "/media"},
		},
	}
	if err := service.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	download := &model.Download{
		Title:       "Test Episode",
		FilePath:    "/downloads/show/ep01.mkv",
		RenamedPath: "/downloads/show/Season 1/ep01.mkv",
		Status:      model.DownloadStatusCompleted,
		TorrentURL:  "http://example.com/test.torrent",
	}
	if err := downloadRepo.Create(download); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	result := service.RefreshDownload(download)
	if result.Status != RefreshStatusSuccess {
		t.Fatalf("RefreshDownload() status = %q, message = %q", result.Status, result.Message)
	}
	if !refreshed {
		t.Fatal("expected test server refresh endpoint to be called")
	}

	updated, err := downloadRepo.GetByID(download.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if updated.MediaLibraryPath != "/media/show/Season 1/ep01.mkv" {
		t.Fatalf("MediaLibraryPath = %q", updated.MediaLibraryPath)
	}
	if updated.MediaLibraryRefreshStatus != RefreshStatusSuccess {
		t.Fatalf("MediaLibraryRefreshStatus = %q", updated.MediaLibraryRefreshStatus)
	}
	if updated.MediaLibraryRefreshedAt == nil {
		t.Fatal("expected MediaLibraryRefreshedAt to be set")
	}
}
