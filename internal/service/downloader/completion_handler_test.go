package downloader

import (
	"errors"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

// MockSubscriptionRepository for testing
type mockSubscriptionRepo struct {
	getByIDFunc func(id uint) (*model.Subscription, error)
	updateFunc  func(subscription *model.Subscription) error
}

func (m *mockSubscriptionRepo) Create(subscription *model.Subscription) error {
	return nil
}

func (m *mockSubscriptionRepo) Update(subscription *model.Subscription) error {
	if m.updateFunc != nil {
		return m.updateFunc(subscription)
	}
	return nil
}

func (m *mockSubscriptionRepo) Delete(id uint) error {
	return nil
}

func (m *mockSubscriptionRepo) GetByID(id uint) (*model.Subscription, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(id)
	}
	return nil, nil
}

func (m *mockSubscriptionRepo) GetByRSSURL(rssURL string) (*model.Subscription, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) List(offset, limit int) ([]model.Subscription, int64, error) {
	return nil, 0, nil
}

func (m *mockSubscriptionRepo) GetActiveSubscriptions() ([]model.Subscription, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) UpdateInTx(tx *gorm.DB, subscription *model.Subscription) error {
	return nil
}

// MockQBittorrentClient for testing
type mockQBClient struct {
	getTorrentFilesFunc func(hash string) ([]TorrentFile, error)
	setLocationFunc     func(hash, location string) error
	renameFileFunc      func(hash, oldName, newName string) error
}

func (m *mockQBClient) AddTorrent(url, savePath, category string) (string, error) {
	return "", nil
}

func (m *mockQBClient) AddTorrentFile(filename string, content []byte, savePath, category string) (string, error) {
	return "", nil
}

func (m *mockQBClient) GetTorrentsByCategory(category string) ([]*TorrentInfo, error) {
	return nil, nil
}

func (m *mockQBClient) GetTorrentInfo(hash string) (*TorrentInfo, error) {
	return nil, nil
}

func (m *mockQBClient) GetTorrentFiles(hash string) ([]TorrentFile, error) {
	if m.getTorrentFilesFunc != nil {
		return m.getTorrentFilesFunc(hash)
	}
	return nil, nil
}

func (m *mockQBClient) RenameTorrentFile(hash, oldName, newName string) error {
	if m.renameFileFunc != nil {
		return m.renameFileFunc(hash, oldName, newName)
	}
	return nil
}

func (m *mockQBClient) SetLocation(hash, location string) error {
	if m.setLocationFunc != nil {
		return m.setLocationFunc(hash, location)
	}
	return nil
}

func (m *mockQBClient) DeleteTorrent(hash string, deleteFiles bool) error {
	return nil
}

func (m *mockQBClient) PauseTorrent(hash string) error {
	return nil
}

func (m *mockQBClient) ResumeTorrent(hash string) error {
	return nil
}

func (m *mockQBClient) GetFreeSpace(path string) (int64, error) {
	return 0, nil
}

func (m *mockQBClient) GetTransferInfo() (*TransferInfo, error) {
	return nil, nil
}

func (m *mockQBClient) GetVersion() (string, error) {
	return "", nil
}

func (m *mockQBClient) TestConnection(host, username, password string) error {
	return nil
}

func (m *mockQBClient) SetProxy(proxy string) error {
	return nil
}

func (m *mockQBClient) DownloadTorrentFile(url string) ([]byte, error) {
	return nil, nil
}

func (m *mockQBClient) Login(host, username, password string) error {
	return nil
}

func (m *mockQBClient) SetCategory(hash string, category string) error {
	return nil
}

// TransferInfo for mock
type TransferInfo struct{}

func TestCompletionHandler_HandleComplete_SendsNotification(t *testing.T) {
	mockNotify := &mockNotificationService{}
	mockDownloadRepo := &mockDownloadRepo{}
	mockSubRepo := &mockSubscriptionRepo{}
	mockQB := &mockQBClient{}
	renamerSvc := NewRenameService("")

	// Pass nil for DB - the handler will skip DB operations in test mode
	handler := NewCompletionHandler(mockSubRepo, mockDownloadRepo, mockNotify, renamerSvc, mockQB, nil)

	download := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Title:          "Test Episode",
		Episode:        1,
		Status:         "downloading",
	}
	torrent := &TorrentInfo{
		Hash:     "abc123",
		SavePath: "/downloads/test",
	}
	subscription := &model.Subscription{
		ID:            1,
		Name:          "Test Anime",
		RenameEnabled: false,
		CurrentEpisode: 0,
	}

	err := handler.HandleComplete(download, torrent, subscription)
	if err != nil {
		t.Fatalf("HandleComplete() error = %v", err)
	}

	// Check notification was sent
	if len(mockNotify.sentPayloads) != 1 {
		t.Errorf("Expected 1 notification, got %d", len(mockNotify.sentPayloads))
	}

	// Check DownloadedAt was set
	if download.DownloadedAt == nil {
		t.Error("Expected DownloadedAt to be set")
	}

	// Check FilePath was set
	if download.FilePath != "/downloads/test" {
		t.Errorf("Expected FilePath to be '/downloads/test', got %s", download.FilePath)
	}
}

func TestCompletionHandler_HandleComplete_UpdatesSubscriptionStats(t *testing.T) {
	mockNotify := &mockNotificationService{}
	mockDownloadRepo := &mockDownloadRepo{}
	mockSubRepo := &mockSubscriptionRepo{}
	mockQB := &mockQBClient{}
	renamerSvc := NewRenameService("")

	// Pass nil for DB - the handler will skip DB operations in test mode
	handler := NewCompletionHandler(mockSubRepo, mockDownloadRepo, mockNotify, renamerSvc, mockQB, nil)

	download := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Title:          "Test Episode",
		Episode:        5,
		Status:         "downloading",
	}
	torrent := &TorrentInfo{
		Hash:     "abc123",
		SavePath: "/downloads/test",
	}
	subscription := &model.Subscription{
		ID:             1,
		Name:           "Test Anime",
		RenameEnabled:  false,
		CurrentEpisode: 3,
	}

	err := handler.HandleComplete(download, torrent, subscription)
	if err != nil {
		t.Fatalf("HandleComplete() error = %v", err)
	}

	// Subscription stats should be updated
	if subscription.CurrentEpisode != 5 {
		t.Errorf("Expected CurrentEpisode to be 5, got %d", subscription.CurrentEpisode)
	}

	if subscription.LastDownloadAt == nil {
		t.Error("Expected LastDownloadAt to be set")
	}
}

func TestCompletionHandler_HandleComplete_WithRenameEnabled(t *testing.T) {
	mockNotify := &mockNotificationService{}
	mockDownloadRepo := &mockDownloadRepo{}
	mockSubRepo := &mockSubscriptionRepo{}
	mockQB := &mockQBClient{
		getTorrentFilesFunc: func(hash string) ([]TorrentFile, error) {
			return []TorrentFile{
				{
					Name: "test_video.mkv",
					Size: 1000000,
				},
			}, nil
		},
	}
	renamerSvc := NewRenameService("")

	// Pass nil for DB - the handler will skip DB operations in test mode
	handler := NewCompletionHandler(mockSubRepo, mockDownloadRepo, mockNotify, renamerSvc, mockQB, nil)

	download := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Title:          "Test Episode",
		Episode:        1,
		Status:         "downloading",
	}
	torrent := &TorrentInfo{
		Hash:     "abc123",
		SavePath: "/downloads/test",
	}
	subscription := &model.Subscription{
		ID:            1,
		Name:          "Test Anime",
		RenameEnabled: true,
		Season:        1,
	}

	err := handler.HandleComplete(download, torrent, subscription)
	if err != nil {
		t.Fatalf("HandleComplete() error = %v", err)
	}

	// Rename should have been attempted (RenamedPath may or may not be set depending on mock behavior)
}

func TestCompletionHandler_HandleComplete_CollectionRename(t *testing.T) {
	mockNotify := &mockNotificationService{}
	mockDownloadRepo := &mockDownloadRepo{}
	mockSubRepo := &mockSubscriptionRepo{}
	mockQB := &mockQBClient{
		getTorrentFilesFunc: func(hash string) ([]TorrentFile, error) {
			return []TorrentFile{
				{Name: "ep01.mkv", Size: 1000000},
				{Name: "ep02.mkv", Size: 1000000},
			}, nil
		},
	}
	renamerSvc := NewRenameService("")

	// Pass nil for DB - the handler will skip DB operations in test mode
	handler := NewCompletionHandler(mockSubRepo, mockDownloadRepo, mockNotify, renamerSvc, mockQB, nil)

	download := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Title:          "Test Collection",
		Episode:        0, // Collection
		Status:         "downloading",
	}
	torrent := &TorrentInfo{
		Hash:     "abc123",
		SavePath: "/downloads/test",
	}
	subscription := &model.Subscription{
		ID:            1,
		Name:          "Test Anime",
		RenameEnabled: true,
	}

	err := handler.HandleComplete(download, torrent, subscription)
	if err != nil {
		t.Fatalf("HandleComplete() error = %v", err)
	}

	// Collection rename should have been attempted
}

func TestCompletionHandler_HandleComplete_NoNotificationService(t *testing.T) {
	mockDownloadRepo := &mockDownloadRepo{}
	mockSubRepo := &mockSubscriptionRepo{}
	mockQB := &mockQBClient{}
	renamerSvc := NewRenameService("")

	// Pass nil for notification service and DB
	handler := NewCompletionHandler(mockSubRepo, mockDownloadRepo, nil, renamerSvc, mockQB, nil)

	download := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Title:          "Test Episode",
		Episode:        1,
		Status:         "downloading",
	}
	torrent := &TorrentInfo{
		Hash:     "abc123",
		SavePath: "/downloads/test",
	}
	subscription := &model.Subscription{
		ID:            1,
		Name:          "Test Anime",
		RenameEnabled: false,
	}

	err := handler.HandleComplete(download, torrent, subscription)
	if err != nil {
		t.Fatalf("HandleComplete() error = %v", err)
	}

	// Should complete without panic even with nil notification service
}

func TestCompletionHandler_HandleComplete_RenameError(t *testing.T) {
	mockNotify := &mockNotificationService{}
	mockDownloadRepo := &mockDownloadRepo{}
	mockSubRepo := &mockSubscriptionRepo{}
	mockQB := &mockQBClient{
		getTorrentFilesFunc: func(hash string) ([]TorrentFile, error) {
			return nil, errors.New("failed to get files")
		},
	}
	renamerSvc := NewRenameService("")

	// Pass nil for DB - the handler will skip DB operations in test mode
	handler := NewCompletionHandler(mockSubRepo, mockDownloadRepo, mockNotify, renamerSvc, mockQB, nil)

	download := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Title:          "Test Episode",
		Episode:        1,
		Status:         "downloading",
	}
	torrent := &TorrentInfo{
		Hash:     "abc123",
		SavePath: "/downloads/test",
	}
	subscription := &model.Subscription{
		ID:            1,
		Name:          "Test Anime",
		RenameEnabled: true,
	}

	// Should not return error even if rename fails
	err := handler.HandleComplete(download, torrent, subscription)
	if err != nil {
		t.Fatalf("HandleComplete() should not fail when rename fails, got error = %v", err)
	}

	// Download should still be marked as complete
	if download.DownloadedAt == nil {
		t.Error("Expected DownloadedAt to be set even when rename fails")
	}
}

func TestCompletionHandler_sendCompletionNotification(t *testing.T) {
	mockNotify := &mockNotificationService{}
	mockDownloadRepo := &mockDownloadRepo{}
	mockSubRepo := &mockSubscriptionRepo{}
	mockQB := &mockQBClient{}
	renamerSvc := NewRenameService("")

	// Pass nil for DB - the handler will skip DB operations in test mode
	handler := NewCompletionHandler(mockSubRepo, mockDownloadRepo, mockNotify, renamerSvc, mockQB, nil)

	// Test single episode notification
	download := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Title:          "Test Episode",
		Episode:        5,
	}
	subscription := &model.Subscription{
		ID:   1,
		Name: "Test Anime",
	}

	handler.(*completionHandler).sendCompletionNotification(download, subscription)

	if len(mockNotify.sentPayloads) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(mockNotify.sentPayloads))
	}

	payload := mockNotify.sentPayloads[0]
	if payload.Event != model.EventDownloadComplete {
		t.Errorf("Expected event %s, got %s", model.EventDownloadComplete, payload.Event)
	}

	// Test collection notification
	mockNotify.sentPayloads = nil
	download.Episode = 0
	handler.(*completionHandler).sendCompletionNotification(download, subscription)

	if len(mockNotify.sentPayloads) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(mockNotify.sentPayloads))
	}
}

func TestIsConflictError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"409 error", errors.New("HTTP 409: conflict"), true},
		{"conflict error", errors.New("conflict detected"), true},
		{"other error", errors.New("network error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConflictError(tt.err)
			if got != tt.expected {
				t.Errorf("isConflictError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestContainsSubstring(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "foo", false},
		{"", "foo", false},
		{"foo", "", true},
		{"HTTP 409: conflict", "409", true},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.substr, func(t *testing.T) {
			got := containsSubstring(tt.s, tt.substr)
			if got != tt.expected {
				t.Errorf("containsSubstring(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.expected)
			}
		})
	}
}

func TestLastIndexOf(t *testing.T) {
	tests := []struct {
		s        string
		c        byte
		expected int
	}{
		{"path/to/file", '/', 7},
		{"file", '/', -1},
		{"/", '/', 0},
		{"", '/', -1},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			got := lastIndexOf(tt.s, tt.c)
			if got != tt.expected {
				t.Errorf("lastIndexOf(%q, %c) = %d, want %d", tt.s, tt.c, got, tt.expected)
			}
		})
	}
}


// 批量操作相关方法（mock实现）

// 批量操作相关方法（mock实现）
func (m *mockSubscriptionRepo) BatchUpdateEnabled(ids []uint, enabled bool) error {
	return nil
}

func (m *mockSubscriptionRepo) BatchDelete(ids []uint) error {
	return nil
}

func (m *mockSubscriptionRepo) BatchUpdateGroup(ids []uint, groupID *uint) error {
	return nil
}

// 分组管理相关方法（mock实现）
func (m *mockSubscriptionRepo) CreateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *mockSubscriptionRepo) UpdateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *mockSubscriptionRepo) DeleteGroup(id uint) error {
	return nil
}

func (m *mockSubscriptionRepo) GetGroupByID(id uint) (*model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) ListGroups() ([]model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) GetDefaultGroup() (*model.SubscriptionGroup, error) {
	return nil, nil
}

// 统计相关方法（mock实现）
func (m *mockSubscriptionRepo) GetStatistics() (*repository.SubscriptionStatistics, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) GetWeeklyUpdates() (int64, error) {
	return 0, nil
}

func (m *mockSubscriptionRepo) GetSubscriptionsWithDownloadCount() ([]repository.SubscriptionWithStats, error) {
	return nil, nil
}
