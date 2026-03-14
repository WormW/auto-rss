package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadStatusAlignment(t *testing.T) {
	// Setup test dependencies
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}

	downloadRepo := repository.NewDownloadRepository(db)
	handler := NewDownloadHandler(downloadRepo, nil)

	// Create test download with known status
	testDownload := model.Download{
		Title:       "test download",
		Status:      "stalled",
		TorrentURL:  "magnet:test",
		TorrentHash: "test-hash-1",
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	r := gin.Default()
	r.GET("/api/downloads", handler.List)
	req, _ := http.NewRequest("GET", "/api/downloads?status=stalled", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []model.Download `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify status is returned exactly as stored
	if len(resp.Data.List) != 1 || resp.Data.List[0].Status != "stalled" {
		t.Fatalf("expected 1 stalled download, got %d items (first status: %q)", len(resp.Data.List), resp.Data.List[0].Status)
	}
}
