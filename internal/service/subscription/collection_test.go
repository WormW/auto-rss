package subscription

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// mockConfigRepo 模拟配置仓库（从 batch_test.go 复制）
type mockConfigRepo struct {
	getFunc func(key string) (*model.Config, error)
}

func (m *mockConfigRepo) Get(key string) (*model.Config, error) {
	if m.getFunc != nil {
		return m.getFunc(key)
	}
	return nil, nil
}

func (m *mockConfigRepo) GetCached(key string) (string, error) { return "", nil }
func (m *mockConfigRepo) Set(key, value string) error          { return nil }
func (m *mockConfigRepo) Delete(key string) error              { return nil }
func (m *mockConfigRepo) GetAll() ([]model.Config, error)      { return nil, nil }

// mockQBClient 模拟 qBittorrent 客户端
type mockQBClient struct {
	addTorrentFunc      func(url, savePath, category string) (string, error)
	addTorrentFileFunc  func(filename string, content []byte, savePath, category string) (string, error)
	getTorrentsFunc     func(category string) ([]*downloader.TorrentInfo, error)
	setProxyFunc        func(proxyURL string) error
	renameTorrentFileFunc func(hash, oldPath, newPath string) error
	setLocationFunc     func(hash, location string) error
	getTorrentInfoFunc  func(hash string) (*downloader.TorrentInfo, error)
	getTorrentFilesFunc func(hash string) ([]downloader.TorrentFile, error)
}

func (m *mockQBClient) Login(host, username, password string) error { return nil }
func (m *mockQBClient) TestConnection(host, username, password string) error { return nil }
func (m *mockQBClient) AddTorrent(url, savePath, category string) (string, error) {
	if m.addTorrentFunc != nil {
		return m.addTorrentFunc(url, savePath, category)
	}
	return "mock-hash-123", nil
}
func (m *mockQBClient) AddTorrentFile(filename string, content []byte, savePath, category string) (string, error) {
	if m.addTorrentFileFunc != nil {
		return m.addTorrentFileFunc(filename, content, savePath, category)
	}
	return "mock-hash-123", nil
}
func (m *mockQBClient) GetTorrentInfo(hash string) (*downloader.TorrentInfo, error) {
	if m.getTorrentInfoFunc != nil {
		return m.getTorrentInfoFunc(hash)
	}
	return nil, nil
}
func (m *mockQBClient) GetTorrentsByCategory(category string) ([]*downloader.TorrentInfo, error) {
	if m.getTorrentsFunc != nil {
		return m.getTorrentsFunc(category)
	}
	return nil, nil
}
func (m *mockQBClient) SetCategory(hash, category string) error { return nil }
func (m *mockQBClient) SetLocation(hash, location string) error {
	if m.setLocationFunc != nil {
		return m.setLocationFunc(hash, location)
	}
	return nil
}
func (m *mockQBClient) RenameTorrentFile(hash, oldPath, newPath string) error {
	if m.renameTorrentFileFunc != nil {
		return m.renameTorrentFileFunc(hash, oldPath, newPath)
	}
	return nil
}
func (m *mockQBClient) DeleteTorrent(hash string, deleteFiles bool) error { return nil }
func (m *mockQBClient) GetTorrentFiles(hash string) ([]downloader.TorrentFile, error) {
	if m.getTorrentFilesFunc != nil {
		return m.getTorrentFilesFunc(hash)
	}
	return nil, nil
}
func (m *mockQBClient) GetVersion() (string, error) { return "4.0.0", nil }
func (m *mockQBClient) SetProxy(proxyURL string) error {
	if m.setProxyFunc != nil {
		return m.setProxyFunc(proxyURL)
	}
	return nil
}

// mockDownloadRepo 模拟下载仓库
type mockDownloadRepo struct {
	downloads map[uint]*model.Download
	nextID    uint
	hashMap   map[string]*model.Download
}

func newMockDownloadRepo() *mockDownloadRepo {
	return &mockDownloadRepo{
		downloads: make(map[uint]*model.Download),
		hashMap:   make(map[string]*model.Download),
		nextID:    1,
	}
}

func (m *mockDownloadRepo) Create(download *model.Download) error {
	download.ID = m.nextID
	m.downloads[download.ID] = download
	if download.TorrentHash != "" {
		m.hashMap[download.TorrentHash] = download
	}
	m.nextID++
	return nil
}

func (m *mockDownloadRepo) Update(download *model.Download) error {
	m.downloads[download.ID] = download
	if download.TorrentHash != "" {
		m.hashMap[download.TorrentHash] = download
	}
	return nil
}

func (m *mockDownloadRepo) Delete(id uint) error {
	if d, ok := m.downloads[id]; ok {
		delete(m.hashMap, d.TorrentHash)
		delete(m.downloads, id)
	}
	return nil
}

func (m *mockDownloadRepo) GetByID(id uint) (*model.Download, error) {
	if d, ok := m.downloads[id]; ok {
		return d, nil
	}
	return nil, nil
}

func (m *mockDownloadRepo) GetByHash(hash string) (*model.Download, error) {
	if d, ok := m.hashMap[hash]; ok {
		return d, nil
	}
	return nil, nil
}

func (m *mockDownloadRepo) List(offset, limit int, status string) ([]model.Download, int64, error) {
	result := make([]model.Download, 0, len(m.downloads))
	for _, d := range m.downloads {
		if status == "" || d.Status == status {
			result = append(result, *d)
		}
	}
	return result, int64(len(result)), nil
}

func (m *mockDownloadRepo) ListBySubscriptionID(subID uint) ([]model.Download, error) {
	result := make([]model.Download, 0)
	for _, d := range m.downloads {
		if d.SubscriptionID == subID {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (m *mockDownloadRepo) ListByStatus(status string) ([]model.Download, error) {
	return m.ListBySubscriptionID(0)
}

func (m *mockDownloadRepo) ListByHashList(hashes []string) ([]model.Download, error) {
	result := make([]model.Download, 0)
	for _, d := range m.downloads {
		for _, h := range hashes {
			if d.TorrentHash == h {
				result = append(result, *d)
				break
			}
		}
	}
	return result, nil
}

func (m *mockDownloadRepo) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	for _, d := range m.downloads {
		if d.SubscriptionID == subscriptionID && d.Episode == episode {
			return d, nil
		}
	}
	return nil, nil
}

func (m *mockDownloadRepo) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	return m.ListBySubscriptionID(subscriptionID)
}

func (m *mockDownloadRepo) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	return m.ListBySubscriptionID(subscriptionID)
}

func (m *mockDownloadRepo) UpdateStatus(id uint, status string) error {
	if d, ok := m.downloads[id]; ok {
		d.Status = status
		m.downloads[id] = d
	}
	return nil
}

func (m *mockDownloadRepo) BatchDelete(ids []uint) error {
	for _, id := range ids {
		m.Delete(id)
	}
	return nil
}

func (m *mockDownloadRepo) DeleteByStatus(status string) error {
	for id, d := range m.downloads {
		if d.Status == status {
			m.Delete(id)
		}
	}
	return nil
}

func (m *mockDownloadRepo) DeleteAll() error {
	m.downloads = make(map[uint]*model.Download)
	m.hashMap = make(map[string]*model.Download)
	return nil
}

func (m *mockDownloadRepo) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	result := make([]model.Download, 0)
	for _, d := range m.downloads {
		if d.Status == "failed" {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (m *mockDownloadRepo) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	result := make([]model.Download, 0)
	for _, d := range m.downloads {
		if d.RetryCount >= minRetries && d.RetryCount <= maxRetries {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (m *mockDownloadRepo) CreateInTx(tx *gorm.DB, download *model.Download) error {
	return m.Create(download)
}

func (m *mockDownloadRepo) UpdateInTx(tx *gorm.DB, download *model.Download) error {
	return m.Update(download)
}

func TestNewCollectionDownloader(t *testing.T) {
	qbClient := &mockQBClient{}
	downloadRepo := newMockDownloadRepo()
	configRepo := &mockConfigRepo{}

	downloader := NewCollectionDownloader(qbClient, downloadRepo, configRepo, "/downloads")

	assert.NotNil(t, downloader)
}

func TestCollectionDownloader_Download_NoCollectionTorrent(t *testing.T) {
	qbClient := &mockQBClient{}
	downloadRepo := newMockDownloadRepo()
	configRepo := &mockConfigRepo{}

	downloader := NewCollectionDownloader(qbClient, downloadRepo, configRepo, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "", // 没有合集种子
	}

	result, err := downloader.Download(sub)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestCollectionDownloader_Download_NoQBClient(t *testing.T) {
	downloadRepo := newMockDownloadRepo()
	configRepo := &mockConfigRepo{}

	downloader := NewCollectionDownloader(nil, downloadRepo, configRepo, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:abc123",
	}

	result, err := downloader.Download(sub)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestCollectionDownloader_Download_MagnetLink(t *testing.T) {
	qbClient := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "abc123hash", nil
		},
	}
	downloadRepo := newMockDownloadRepo()
	configRepo := &mockConfigRepo{}

	downloader := NewCollectionDownloader(qbClient, downloadRepo, configRepo, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:abc123",
		Fansub:            "TestSub",
	}

	result, err := downloader.Download(sub)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "abc123hash", result.TorrentHash)
	assert.Equal(t, 0, result.Episode) // 0 表示合集
	assert.Contains(t, result.Title, "合集")
}

func TestCollectionDownloader_Download_ExistingHash(t *testing.T) {
	qbClient := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "existing-hash", nil
		},
	}
	downloadRepo := newMockDownloadRepo()
	configRepo := &mockConfigRepo{}

	// 先创建一个已存在的下载记录
	existingDownload := &model.Download{
		ID:            1,
		TorrentHash:   "existing-hash",
		SubscriptionID: 1,
	}
	downloadRepo.Create(existingDownload)

	downloader := NewCollectionDownloader(qbClient, downloadRepo, configRepo, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:existing",
	}

	result, err := downloader.Download(sub)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, uint(1), result.ID) // 应该返回已存在的记录
}

func TestCollectionDownloader_DownloadAsync(t *testing.T) {
	qbClient := &mockQBClient{
		addTorrentFunc: func(url, savePath, category string) (string, error) {
			return "async-hash", nil
		},
	}
	downloadRepo := newMockDownloadRepo()
	configRepo := &mockConfigRepo{}

	downloader := NewCollectionDownloader(qbClient, downloadRepo, configRepo, "/downloads")

	sub := &model.Subscription{
		ID:                1,
		Name:              "Test Anime",
		CollectionTorrent: "magnet:?xt=urn:btih:async",
	}

	// 异步下载不应该阻塞
	downloader.DownloadAsync(sub)

	// 给 goroutine 一点时间执行
	// 注意：这里不验证结果，只是确保不会 panic
}
