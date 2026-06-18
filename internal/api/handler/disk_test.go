package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/disk"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDiskHandlerTest(t *testing.T) (*gin.Engine, *gorm.DB, repository.DownloadRepository, repository.ConfigRepository, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}, &model.DiskSample{}, &model.DiskCleanupRecord{}); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	downloadRepo := repository.NewDownloadRepository(db)
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	configRepo := repository.NewConfigRepository(db)
	root := t.TempDir()
	setDiskHandlerConfig(t, configRepo, map[string]string{
		"download_path":                 root,
		"disk.cleanup_protect_watching": "false",
	})

	handler := NewDiskHandler(db, downloadRepo, subscriptionRepo, configRepo)
	router := gin.New()
	router.POST("/disk/cleanup", handler.TriggerCleanup)
	router.GET("/disk/history", handler.GetHistory)

	return router, db, downloadRepo, configRepo, root
}

func TestTriggerCleanupReturnsRealCleanupResult(t *testing.T) {
	router, db, downloadRepo, _, root := setupDiskHandlerTest(t)
	oldTime := time.Now().AddDate(0, 0, -40)
	deletePath := filepath.Join(root, "old.mkv")
	if err := os.WriteFile(deletePath, []byte(strings.Repeat("x", 2048)), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	download := model.Download{
		Title:        "old",
		TorrentURL:   "https://example.test/old.torrent",
		TorrentHash:  "handler-real-counts",
		Status:       model.DownloadStatusCompleted,
		FilePath:     deletePath,
		DownloadedAt: &oldTime,
	}
	if err := downloadRepo.Create(&download); err != nil {
		t.Fatalf("create download: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/disk/cleanup", bytes.NewBufferString(`{"strategy":"age","keep_days":30}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Code int                `json:"code"`
		Data disk.CleanupResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != 0 || !response.Data.Cleaned || response.Data.DeletedCount != 1 || response.Data.SkippedCount != 0 || response.Data.FreedBytes != 2048 {
		t.Fatalf("unexpected cleanup response: %#v", response)
	}
	if len(response.Data.Items) != 1 || response.Data.Items[0].Action != "deleted" || response.Data.Items[0].FreedBytes != 2048 {
		t.Fatalf("expected deleted item details, got %#v", response.Data.Items)
	}
	if response.Data.BeforeFreeBytes <= 0 || response.Data.AfterFreeBytes <= 0 {
		t.Fatalf("expected before/after free bytes, got before=%d after=%d", response.Data.BeforeFreeBytes, response.Data.AfterFreeBytes)
	}
	if _, err := os.Stat(deletePath); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted, stat err=%v", err)
	}
	var count int64
	db.Model(&model.Download{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected download record deleted, count=%d", count)
	}
}

func TestTriggerCleanupReportsPartialFailureWithoutDeletingDBRecord(t *testing.T) {
	router, db, downloadRepo, _, root := setupDiskHandlerTest(t)
	oldTime := time.Now().AddDate(0, 0, -40)
	okPath := filepath.Join(root, "ok.mkv")
	if err := os.WriteFile(okPath, []byte(strings.Repeat("o", 1024)), 0600); err != nil {
		t.Fatalf("write deletable fixture: %v", err)
	}
	outsideRoot := t.TempDir()
	outsidePath := filepath.Join(outsideRoot, "outside.mkv")
	if err := os.WriteFile(outsidePath, []byte("keep"), 0600); err != nil {
		t.Fatalf("write outside fixture: %v", err)
	}
	for _, download := range []model.Download{
		{Title: "ok", TorrentURL: "https://example.test/ok.torrent", TorrentHash: "handler-partial-ok", Status: model.DownloadStatusCompleted, FilePath: okPath, DownloadedAt: &oldTime},
		{Title: "outside", TorrentURL: "https://example.test/outside.torrent", TorrentHash: "handler-partial-outside", Status: model.DownloadStatusCompleted, FilePath: outsidePath, DownloadedAt: &oldTime},
	} {
		if err := downloadRepo.Create(&download); err != nil {
			t.Fatalf("create download %s: %v", download.Title, err)
		}
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/disk/cleanup", bytes.NewBufferString(`{"strategy":"age","keep_days":30}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Code int                `json:"code"`
		Data disk.CleanupResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data.DeletedCount != 1 || response.Data.SkippedCount != 1 || response.Data.FreedBytes != 1024 {
		t.Fatalf("expected one delete and one skip, got %#v", response.Data)
	}
	var skipped *disk.CleanupItem
	for i := range response.Data.Items {
		if response.Data.Items[i].Action == "skipped" {
			skipped = &response.Data.Items[i]
		}
	}
	if skipped == nil || !strings.Contains(skipped.Reason, "outside download root") {
		t.Fatalf("expected path-boundary failure in item details, got %#v", response.Data.Items)
	}
	if _, err := os.Stat(okPath); !os.IsNotExist(err) {
		t.Fatalf("expected deletable file removed, stat err=%v", err)
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("expected outside file retained: %v", err)
	}
	var remaining []model.Download
	if err := db.Find(&remaining).Error; err != nil {
		t.Fatalf("list remaining downloads: %v", err)
	}
	if len(remaining) != 1 || remaining[0].TorrentHash != "handler-partial-outside" {
		t.Fatalf("expected only failed cleanup record retained, got %#v", remaining)
	}
}

func TestGetDiskHistoryReturnsCleanupPaginationAndFailureFields(t *testing.T) {
	router, db, _, _, _ := setupDiskHandlerTest(t)
	now := time.Now().UTC()
	records := []model.DiskCleanupRecord{
		{
			Trigger:            "manual",
			Strategy:           "age",
			DownloadPath:       "/downloads",
			DeletedCount:       1,
			SkippedCount:       1,
			FailedCount:        1,
			FailedPaths:        `["/downloads/failed.mkv"]`,
			FreedBytes:         4096,
			BeforeFreeBytes:    10000,
			AfterFreeBytes:     14096,
			MediaLibraryStatus: "unconfigured",
			Message:            `[{"path":"/downloads/failed.mkv","action":"skipped","reason":"permission denied"}]`,
			CreatedAt:          now.Add(-1 * time.Minute),
		},
		{
			Trigger:         "auto",
			Strategy:        "space",
			DownloadPath:    "/downloads",
			DeletedCount:    2,
			FreedBytes:      8192,
			BeforeFreeBytes: 5000,
			AfterFreeBytes:  13192,
			CreatedAt:       now,
		},
	}
	for i := range records {
		if err := db.Create(&records[i]).Error; err != nil {
			t.Fatalf("create cleanup record: %v", err)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/disk/history?page=2&page_size=1", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Cleanup  []modelDiskCleanupRecordDTO `json:"cleanup"`
			List     []modelDiskCleanupRecordDTO `json:"list"`
			Total    int64                       `json:"total"`
			Page     int                         `json:"page"`
			PageSize int                         `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 || resp.Data.Total != 2 || resp.Data.Page != 2 || resp.Data.PageSize != 1 {
		t.Fatalf("unexpected pagination metadata: %#v", resp.Data)
	}
	if len(resp.Data.Cleanup) != 1 || len(resp.Data.List) != 1 {
		t.Fatalf("expected one cleanup item in both aliases, got cleanup=%d list=%d", len(resp.Data.Cleanup), len(resp.Data.List))
	}
	item := resp.Data.Cleanup[0]
	if item.Trigger != "manual" || item.Strategy != "age" || item.DeletedCount != 1 || item.FreedBytes != 4096 {
		t.Fatalf("unexpected cleanup item fields: %#v", item)
	}
	if item.FailedCount != 1 || len(item.FailedPaths) != 1 || item.FailedPaths[0] != "/downloads/failed.mkv" {
		t.Fatalf("unexpected failure fields: %#v", item)
	}
}

func setDiskHandlerConfig(t *testing.T, repo repository.ConfigRepository, values map[string]string) {
	t.Helper()
	for key, value := range values {
		if err := repo.Set(key, value); err != nil {
			t.Fatalf("set config %s: %v", key, err)
		}
	}
}
