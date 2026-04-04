package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockQBittorrentClient 模拟 qBittorrent 客户端
type mockQBittorrentClient struct {
	deleteTorrentFunc func(hash string, deleteFiles bool) error
	addTorrentFunc    func(torrentURL, downloadPath, category string) (string, error)
	deleteCalled      bool
	addCalled         bool
	deletedHash       string
	addedURL          string
	addedPath         string
}

func (m *mockQBittorrentClient) DeleteTorrent(hash string, deleteFiles bool) error {
	m.deleteCalled = true
	m.deletedHash = hash
	if m.deleteTorrentFunc != nil {
		return m.deleteTorrentFunc(hash, deleteFiles)
	}
	return nil
}

func (m *mockQBittorrentClient) AddTorrent(torrentURL, downloadPath, category string) (string, error) {
	m.addCalled = true
	m.addedURL = torrentURL
	m.addedPath = downloadPath
	if m.addTorrentFunc != nil {
		return m.addTorrentFunc(torrentURL, downloadPath, category)
	}
	return "new-hash-123", nil
}

func (m *mockQBittorrentClient) GetTorrentInfo(hash string) (map[string]interface{}, error) {
	return nil, nil
}

func setupRetryTest(t *testing.T) (*gorm.DB, repository.DownloadRepository, repository.ConfigRepository) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}

	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)

	// Set default download path
	if err := configRepo.Set("download_path", "/downloads"); err != nil {
		t.Fatalf("failed to set download_path: %v", err)
	}

	return db, downloadRepo, configRepo
}

func TestRetryHandler_FetchesDownloadByID(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test",
		TorrentHash:    "test-hash-1",
		SubscriptionID: sub.ID,
		RetryCount:     3,
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify download was fetched and updated
	updated, err := downloadRepo.GetByID(testDownload.ID)
	if err != nil {
		t.Fatalf("failed to get updated download: %v", err)
	}

	if updated.RetryCount != 0 {
		t.Errorf("expected RetryCount to be reset to 0, got %d", updated.RetryCount)
	}
}

func TestRetryHandler_Returns404IfDownloadNotFound(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Setup gin test context - request non-existent download
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/999/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "Download not found" {
		t.Errorf("expected 'Download not found' message, got %q", resp.Message)
	}
}

func TestRetryHandler_CallsDeleteTorrentWhenHashExists(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download with hash
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test",
		TorrentHash:    "test-hash-abc123",
		SubscriptionID: sub.ID,
		RetryCount:     2,
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify DeleteTorrent was called
	if !mockQB.deleteCalled {
		t.Error("expected DeleteTorrent to be called")
	}

	if mockQB.deletedHash != "test-hash-abc123" {
		t.Errorf("expected deleted hash to be 'test-hash-abc123', got %q", mockQB.deletedHash)
	}
}

func TestRetryHandler_ResetsRetryFields(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download with retry history
	now := model.Now()
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test",
		TorrentHash:    "test-hash-1",
		SubscriptionID: sub.ID,
		RetryCount:     5,
		MaxRetries:     10,
		NextRetryAt:    &now,
		LastError:      "previous error",
		RetryReason:    "auto_retry",
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify fields were reset
	updated, err := downloadRepo.GetByID(testDownload.ID)
	if err != nil {
		t.Fatalf("failed to get updated download: %v", err)
	}

	if updated.RetryCount != 0 {
		t.Errorf("expected RetryCount to be 0, got %d", updated.RetryCount)
	}

	if updated.RetryReason != "user_retry" {
		t.Errorf("expected RetryReason to be 'user_retry', got %q", updated.RetryReason)
	}

	if updated.NextRetryAt != nil {
		t.Error("expected NextRetryAt to be nil")
	}

	if updated.LastError != "" {
		t.Errorf("expected LastError to be empty, got %q", updated.LastError)
	}

	if updated.Status != "downloading" {
		t.Errorf("expected Status to be 'downloading', got %q", updated.Status)
	}

	if updated.TorrentHash != "new-hash-123" {
		t.Errorf("expected TorrentHash to be updated to 'new-hash-123', got %q", updated.TorrentHash)
	}
}

func TestRetryHandler_CallsAddTorrent(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test-retry-url",
		TorrentHash:    "old-hash",
		SubscriptionID: sub.ID,
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify AddTorrent was called with correct parameters
	if !mockQB.addCalled {
		t.Error("expected AddTorrent to be called")
	}

	if mockQB.addedURL != "magnet:test-retry-url" {
		t.Errorf("expected added URL to be 'magnet:test-retry-url', got %q", mockQB.addedURL)
	}

	// Should contain the anime name in the path
	if mockQB.addedPath == "" {
		t.Error("expected download path to be set")
	}
}

func TestRetryHandler_OnAddTorrentSuccess_UpdatesStatusToDownloading(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{
		addTorrentFunc: func(torrentURL, downloadPath, category string) (string, error) {
			return "success-hash-xyz", nil
		},
	}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test",
		TorrentHash:    "old-hash",
		SubscriptionID: sub.ID,
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify status updated to downloading
	updated, err := downloadRepo.GetByID(testDownload.ID)
	if err != nil {
		t.Fatalf("failed to get updated download: %v", err)
	}

	if updated.Status != "downloading" {
		t.Errorf("expected Status to be 'downloading', got %q", updated.Status)
	}

	if updated.TorrentHash != "success-hash-xyz" {
		t.Errorf("expected TorrentHash to be 'success-hash-xyz', got %q", updated.TorrentHash)
	}
}

func TestRetryHandler_OnAddTorrentFailure_KeepsFailedStatus(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{
		addTorrentFunc: func(torrentURL, downloadPath, category string) (string, error) {
			return "", errors.New("qBittorrent rejected torrent")
		},
	}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test",
		TorrentHash:    "old-hash",
		SubscriptionID: sub.ID,
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response - should still return 200 but with failure indication
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify status stays failed and error is recorded
	updated, err := downloadRepo.GetByID(testDownload.ID)
	if err != nil {
		t.Fatalf("failed to get updated download: %v", err)
	}

	if updated.Status != "failed" {
		t.Errorf("expected Status to remain 'failed', got %q", updated.Status)
	}

	if updated.LastError != "qBittorrent rejected torrent" {
		t.Errorf("expected LastError to be set, got %q", updated.LastError)
	}
}

func TestRetryHandler_InvalidID_Returns400(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Setup gin test context - request with invalid ID
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/invalid/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRetryHandler_NoQBClient_SkipsTorrentOperations(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	// Pass nil as qbClient
	handler := NewDownloadHandler(downloadRepo, nil, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test",
		TorrentHash:    "test-hash-1",
		SubscriptionID: sub.ID,
		RetryCount:     3,
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify fields were still reset even without qbClient
	updated, err := downloadRepo.GetByID(testDownload.ID)
	if err != nil {
		t.Fatalf("failed to get updated download: %v", err)
	}

	if updated.RetryCount != 0 {
		t.Errorf("expected RetryCount to be reset to 0, got %d", updated.RetryCount)
	}

	if updated.Status != "pending" {
		t.Errorf("expected Status to be 'pending' when no qbClient, got %q", updated.Status)
	}
}

func TestRetryHandler_DeleteTorrentError_Ignored(t *testing.T) {
	_, downloadRepo, configRepo := setupRetryTest(t)
	mockQB := &mockQBittorrentClient{
		deleteTorrentFunc: func(hash string, deleteFiles bool) error {
			return errors.New("torrent not found in qBittorrent")
		},
	}
	handler := NewDownloadHandler(downloadRepo, mockQB, configRepo)

	// Create test subscription first
	sub := &model.Subscription{
		Name: "Test Anime",
	}
	db := downloadRepo.(*repository.DownloadRepository).GetDB()
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// Create test download
	testDownload := model.Download{
		Title:          "test download",
		Status:         "failed",
		TorrentURL:     "magnet:test",
		TorrentHash:    "test-hash-1",
		SubscriptionID: sub.ID,
	}
	if err := downloadRepo.Create(&testDownload); err != nil {
		t.Fatalf("failed to create test download: %v", err)
	}

	// Setup gin test context
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/downloads/:id/retry", handler.Retry)
	req, _ := http.NewRequest("POST", "/api/downloads/1/retry", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validate response - should succeed even if delete fails
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify retry still proceeded
	if !mockQB.deleteCalled {
		t.Error("expected DeleteTorrent to be called")
	}
}
