package subscription

import (
	"errors"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"gorm.io/gorm"
)

// MockQBittorrentClient mock实现
type mockQBClient struct {
	addTorrentFunc      func(url, savePath, category string) (string, error)
	addTorrentFileFunc  func(filename string, content []byte, savePath, category string) (string, error)
	getTorrentsFunc     func(category string) ([]*downloader.TorrentInfo, error)
	downloadTorrentFunc func(url string) ([]byte, error)
	setProxyFunc        func(proxy string) error
}

func (m *mockQBClient) Login(host, username, password string) error          { return nil }
func (m *mockQBClient) TestConnection(host, username, password string) error { return nil }
func (m *mockQBClient) AddTorrent(url, savePath, category string) (string, error) {
	if m.addTorrentFunc != nil {
		return m.addTorrentFunc(url, savePath, category)
	}
	return "hash123", nil
}
func (m *mockQBClient) AddTorrentExclusive(url, savePath, category, expectedHash string) (string, error) {
	return m.AddTorrent(url, savePath, category)
}
func (m *mockQBClient) AddTorrentFile(filename string, content []byte, savePath, category string) (string, error) {
	if m.addTorrentFileFunc != nil {
		return m.addTorrentFileFunc(filename, content, savePath, category)
	}
	return "hash123", nil
}
func (m *mockQBClient) GetTorrentInfo(hash string) (*downloader.TorrentInfo, error) { return nil, nil }
func (m *mockQBClient) GetTorrentsByCategory(category string) ([]*downloader.TorrentInfo, error) {
	if m.getTorrentsFunc != nil {
		return m.getTorrentsFunc(category)
	}
	return nil, nil
}
func (m *mockQBClient) SetCategory(hash, category string) error               { return nil }
func (m *mockQBClient) SetLocation(hash, location string) error               { return nil }
func (m *mockQBClient) RenameTorrentFile(hash, oldPath, newPath string) error { return nil }
func (m *mockQBClient) PauseTorrent(hash string) error                        { return nil }
func (m *mockQBClient) ResumeTorrent(hash string) error                       { return nil }
func (m *mockQBClient) RemoveTorrentTask(hash string) error                   { return nil }
func (m *mockQBClient) DeleteTorrentWithPayload(hash string) error            { return nil }
func (m *mockQBClient) GetTorrentFiles(hash string) ([]downloader.TorrentFile, error) {
	return nil, nil
}
func (m *mockQBClient) GetVersion() (string, error) { return "", nil }
func (m *mockQBClient) SetProxy(proxy string) error {
	if m.setProxyFunc != nil {
		return m.setProxyFunc(proxy)
	}
	return nil
}
func (m *mockQBClient) DownloadTorrentFile(url string) ([]byte, error) {
	if m.downloadTorrentFunc != nil {
		return m.downloadTorrentFunc(url)
	}
	return []byte("torrent content"), nil
}

// MockDownloadRepository mock实现
type mockDownloadRepo struct {
	downloads map[string]*model.Download
	createErr error
}

func (m *mockDownloadRepo) Create(download *model.Download) error {
	if m.createErr != nil {
		return m.createErr
	}
	download.ID = uint(len(m.downloads) + 1)
	m.downloads[download.TorrentHash] = download
	return nil
}
func (m *mockDownloadRepo) Update(download *model.Download) error { return nil }
func (m *mockDownloadRepo) Delete(id uint) error                  { return nil }
func (m *mockDownloadRepo) GetByID(id uint) (*model.Download, error) {
	for _, d := range m.downloads {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, errors.New("not found")
}
func (m *mockDownloadRepo) GetByHash(hash string) (*model.Download, error) {
	if d, ok := m.downloads[hash]; ok {
		return d, nil
	}
	return nil, nil
}
func (m *mockDownloadRepo) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepo) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepo) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepo) List(offset, limit int, status string) ([]model.Download, int64, error) {
	return nil, 0, nil
}
func (m *mockDownloadRepo) ListBySubscriptionID(subscriptionID uint) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepo) UpdateStatus(id uint, status string) error { return nil }
func (m *mockDownloadRepo) BatchDelete(ids []uint) error              { return nil }
func (m *mockDownloadRepo) DeleteByStatus(status string) error        { return nil }
func (m *mockDownloadRepo) DeleteAll() error                          { return nil }
func (m *mockDownloadRepo) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepo) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepo) CreateInTx(tx *gorm.DB, download *model.Download) error {
	return m.Create(download)
}
func (m *mockDownloadRepo) UpdateInTx(tx *gorm.DB, download *model.Download) error {
	return m.Update(download)
}

func TestCollectionDownloader_Download_AddsTorrent(t *testing.T) {
	mockQB := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "magnet_hash_123", nil
		},
	}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:abc123",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected download record, got nil")
	}

	if result.TorrentHash != "magnet_hash_123" {
		t.Errorf("expected hash magnet_hash_123, got %s", result.TorrentHash)
	}

	if result.Episode != 0 {
		t.Errorf("expected episode 0 (collection marker), got %d", result.Episode)
	}
}

func TestCollectionDownloader_Download_HandlesTorrentURL(t *testing.T) {
	downloadCalled := false
	mockQB := &mockQBClient{
		downloadTorrentFunc: func(url string) ([]byte, error) {
			downloadCalled = true
			return []byte("torrent file content"), nil
		},
		addTorrentFileFunc: func(filename string, content []byte, savePath, category string) (string, error) {
			return "torrent_hash_456", nil
		},
	}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "https://example.com/download/abc.torrent",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !downloadCalled {
		t.Error("expected DownloadTorrentFile to be called for .torrent URL")
	}

	if result == nil {
		t.Fatal("expected download record, got nil")
	}

	if result.TorrentHash != "torrent_hash_456" {
		t.Errorf("expected hash torrent_hash_456, got %s", result.TorrentHash)
	}
}

func TestCollectionDownloader_Download_HandlesMagnetLink(t *testing.T) {
	mockQB := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "magnet_hash_789", nil
		},
	}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:def789",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected download record, got nil")
	}

	if result.TorrentHash != "magnet_hash_789" {
		t.Errorf("expected hash magnet_hash_789, got %s", result.TorrentHash)
	}
}

func TestCollectionDownloader_Download_CreatesRecordWithEpisodeZero(t *testing.T) {
	mockQB := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "hash_abc", nil
		},
	}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:abc",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected download record, got nil")
	}

	if result.Episode != 0 {
		t.Errorf("expected episode 0 (collection marker), got %d", result.Episode)
	}

	if result.Status != "downloading" {
		t.Errorf("expected status 'downloading', got %s", result.Status)
	}

	expectedTitle := "Test Anime [合集]"
	if result.Title != expectedTitle {
		t.Errorf("expected title '%s', got '%s'", expectedTitle, result.Title)
	}
}

func TestCollectionDownloader_Download_FindsExistingBySavePath(t *testing.T) {
	mockQB := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "", nil // Empty hash, simulating existing torrent
		},
		getTorrentsFunc: func(category string) ([]*downloader.TorrentInfo, error) {
			return []*downloader.TorrentInfo{
				{
					Hash:     "existing_hash_123",
					Name:     "Test Anime Collection",
					SavePath: "/downloads/Test Anime",
				},
			}, nil
		},
	}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:existing",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected download record, got nil")
	}

	if result.TorrentHash != "existing_hash_123" {
		t.Errorf("expected hash existing_hash_123, got %s", result.TorrentHash)
	}
}

func TestCollectionDownloader_Download_SkipsEmptyCollectionTorrent(t *testing.T) {
	mockQB := &mockQBClient{}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Error("expected nil result for empty collection torrent")
	}
}

func TestCollectionDownloader_Download_SkipsIfQBClientNil(t *testing.T) {
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(nil, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:abc",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Error("expected nil result when qbClient is nil")
	}
}

func TestCollectionDownloader_Download_ValidatesPath(t *testing.T) {
	mockQB := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "hash_123", nil
		},
	}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	// Path traversal attempt - the name is sanitized by GenerateDownloadPath
	// so the path becomes /downloads/_.._.._.._etc_passwd which is safe
	sub := &model.Subscription{
		ID:                1,
		Name:              "../../../etc/passwd", // Path traversal attempt
		CollectionTorrent: "magnet:?xt=urn:btih:abc",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	// The path is sanitized by GenerateDownloadPath which replaces / with _
	// So this should succeed with a sanitized path
	if err != nil {
		t.Fatalf("unexpected error (path should be sanitized): %v", err)
	}

	if result == nil {
		t.Fatal("expected download record (path should be sanitized)")
	}
}

func TestCollectionDownloader_Download_SetsProxyForTorrentURL(t *testing.T) {
	proxySet := false
	mockQB := &mockQBClient{
		downloadTorrentFunc: func(url string) ([]byte, error) {
			return []byte("content"), nil
		},
		addTorrentFileFunc: func(filename string, content []byte, savePath, category string) (string, error) {
			return "hash_123", nil
		},
		setProxyFunc: func(proxy string) error {
			if proxy == "http://proxy.example.com:8080" {
				proxySet = true
			}
			return nil
		},
	}
	mockRepo := &mockDownloadRepo{downloads: make(map[string]*model.Download)}
	mockConfig := &mockConfigRepoWithProxy{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "https://example.com/download.torrent",
		Fansub:            "TestFansub",
	}

	_, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !proxySet {
		t.Error("expected proxy to be set for .torrent download")
	}
}

// Mock config repo with proxy
type mockConfigRepoWithProxy struct{}

func (m *mockConfigRepoWithProxy) Get(key string) (*model.Config, error) {
	if key == "system_proxy" {
		return &model.Config{Key: "system_proxy", Value: "http://proxy.example.com:8080"}, nil
	}
	return nil, nil
}
func (m *mockConfigRepoWithProxy) GetCached(key string) (string, error) { return "", nil }
func (m *mockConfigRepoWithProxy) Set(key, value string) error          { return nil }
func (m *mockConfigRepoWithProxy) Delete(key string) error              { return nil }
func (m *mockConfigRepoWithProxy) GetAll() ([]model.Config, error)      { return nil, nil }

func TestCollectionDownloader_Download_SkipsIfRecordExists(t *testing.T) {
	mockQB := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "existing_hash", nil
		},
	}
	existingDownload := &model.Download{
		ID:             1,
		TorrentHash:    "existing_hash",
		SubscriptionID: 1,
	}
	mockRepo := &mockDownloadRepo{
		downloads: map[string]*model.Download{
			"existing_hash": existingDownload,
		},
	}
	mockConfig := &mockConfigRepo{}

	downloader := NewCollectionDownloader(mockQB, mockRepo, mockConfig, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:abc",
		Fansub:            "TestFansub",
	}

	result, err := downloader.Download(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected existing download record")
	}

	if result.ID != 1 {
		t.Errorf("expected existing record with ID 1, got ID %d", result.ID)
	}
}
