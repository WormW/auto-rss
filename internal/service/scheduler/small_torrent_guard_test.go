package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestProcessDownloadItemSmallTorrentGuard(t *testing.T) {
	tests := []struct {
		name              string
		sizeBytes         int64
		configuredMinimum string
		wantAdded         bool
		wantDownloads     int64
	}{
		{
			name:          "no size bypasses guard",
			sizeBytes:     0,
			wantAdded:     true,
			wantDownloads: 1,
		},
		{
			name:          "under default threshold skips",
			sizeBytes:     defaultMinTorrentSizeBytes - 1,
			wantAdded:     false,
			wantDownloads: 0,
		},
		{
			name:          "over default threshold downloads",
			sizeBytes:     defaultMinTorrentSizeBytes,
			wantAdded:     true,
			wantDownloads: 1,
		},
		{
			name:              "zero config disables guard",
			sizeBytes:         1,
			configuredMinimum: "0",
			wantAdded:         true,
			wantDownloads:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupSmallTorrentGuardDB(t)
			downloadRepo := repository.NewDownloadRepository(db)
			qb := &smallTorrentGuardQBClient{}
			configRepo := &smallTorrentGuardConfigRepo{values: map[string]string{}}
			if tt.configuredMinimum != "" {
				configRepo.values[minTorrentSizeBytesConfigKey] = tt.configuredMinimum
			}

			s := &scheduler{
				db:           db,
				downloadRepo: downloadRepo,
				configRepo:   configRepo,
				qbClient:     qb,
			}
			sub := &model.Subscription{
				ID:        1,
				Name:      "Guarded Anime",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			}
			item := &rss.RSSItem{
				Title:       "Guarded Anime - 01",
				TorrentURL:  "https://example.test/guarded-01.torrent",
				TorrentHash: "guarded-01",
				Episode:     1,
				SizeBytes:   tt.sizeBytes,
			}

			created, err := s.processDownloadItem(sub, item, 0)
			if err != nil {
				t.Fatalf("processDownloadItem() error = %v", err)
			}
			if created != tt.wantAdded {
				t.Fatalf("created = %v, want %v", created, tt.wantAdded)
			}
			if qb.addCalls != boolToInt(tt.wantAdded) {
				t.Fatalf("qBittorrent add calls = %d, want %d", qb.addCalls, boolToInt(tt.wantAdded))
			}

			var count int64
			if err := db.Model(&model.Download{}).Count(&count).Error; err != nil {
				t.Fatalf("count downloads: %v", err)
			}
			if count != tt.wantDownloads {
				t.Fatalf("download count = %d, want %d", count, tt.wantDownloads)
			}
		})
	}
}

func TestProcessDownloadItemSmallTorrentGuardLeavesReplacedDownloadIntact(t *testing.T) {
	db := setupSmallTorrentGuardDB(t)
	downloadRepo := repository.NewDownloadRepository(db)
	qb := &smallTorrentGuardQBClient{}
	s := &scheduler{
		db:           db,
		downloadRepo: downloadRepo,
		configRepo:   &smallTorrentGuardConfigRepo{values: map[string]string{}},
		qbClient:     qb,
	}
	existing := &model.Download{
		SubscriptionID: 1,
		Title:          "Guarded Anime - 01",
		TorrentURL:     "https://example.test/guarded-01-old.torrent",
		TorrentHash:    "guarded-01-old",
		Status:         model.DownloadStatusDownloading,
	}
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("create existing download: %v", err)
	}

	created, err := s.processDownloadItem(
		&model.Subscription{ID: 1, Name: "Guarded Anime"},
		&rss.RSSItem{
			Title:       "Guarded Anime - 01 v2",
			TorrentURL:  "https://example.test/guarded-01-v2.torrent",
			TorrentHash: "guarded-01-v2",
			Episode:     1,
			SizeBytes:   defaultMinTorrentSizeBytes - 1,
		},
		existing.ID,
	)
	if err != nil {
		t.Fatalf("processDownloadItem() error = %v", err)
	}
	if created {
		t.Fatal("processDownloadItem() created under-threshold replacement, want skip")
	}
	if qb.addCalls != 0 {
		t.Fatalf("qBittorrent add calls = %d, want 0", qb.addCalls)
	}

	var downloads []model.Download
	if err := db.Order("id").Find(&downloads).Error; err != nil {
		t.Fatalf("list downloads: %v", err)
	}
	if len(downloads) != 1 {
		t.Fatalf("download count = %d, want 1", len(downloads))
	}
	if downloads[0].ID != existing.ID || downloads[0].Status != model.DownloadStatusDownloading {
		t.Fatalf("existing download changed = id:%d status:%s", downloads[0].ID, downloads[0].Status)
	}
}

func setupSmallTorrentGuardDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type smallTorrentGuardQBClient struct {
	addCalls int
}

func (m *smallTorrentGuardQBClient) Login(host, username, password string) error { return nil }
func (m *smallTorrentGuardQBClient) TestConnection(host, username, password string) error {
	return nil
}
func (m *smallTorrentGuardQBClient) AddTorrent(torrentURL string, savePath string, category string) (string, error) {
	m.addCalls++
	return "added-hash", nil
}
func (m *smallTorrentGuardQBClient) AddTorrentFile(filename string, fileContent []byte, savePath string, category string) (string, error) {
	return "added-file-hash", nil
}
func (m *smallTorrentGuardQBClient) GetTorrentInfo(hash string) (*downloader.TorrentInfo, error) {
	return nil, nil
}
func (m *smallTorrentGuardQBClient) GetTorrentsByCategory(category string) ([]*downloader.TorrentInfo, error) {
	return nil, nil
}
func (m *smallTorrentGuardQBClient) SetCategory(hash string, category string) error { return nil }
func (m *smallTorrentGuardQBClient) SetLocation(hash string, location string) error { return nil }
func (m *smallTorrentGuardQBClient) RenameTorrentFile(hash string, oldPath string, newPath string) error {
	return nil
}
func (m *smallTorrentGuardQBClient) RemoveTorrentTask(hash string) error        { return nil }
func (m *smallTorrentGuardQBClient) DeleteTorrentWithPayload(hash string) error { return nil }
func (m *smallTorrentGuardQBClient) GetTorrentFiles(hash string) ([]downloader.TorrentFile, error) {
	return nil, nil
}
func (m *smallTorrentGuardQBClient) GetVersion() (string, error)    { return "", nil }
func (m *smallTorrentGuardQBClient) SetProxy(proxyURL string) error { return nil }
func (m *smallTorrentGuardQBClient) DownloadTorrentFile(url string) ([]byte, error) {
	return nil, nil
}

type smallTorrentGuardConfigRepo struct {
	values map[string]string
}

func (r *smallTorrentGuardConfigRepo) Get(key string) (*model.Config, error) {
	if value, ok := r.values[key]; ok {
		return &model.Config{Key: key, Value: value}, nil
	}
	return nil, errors.New("not found")
}
func (r *smallTorrentGuardConfigRepo) GetCached(key string) (string, error) {
	cfg, err := r.Get(key)
	if err != nil {
		return "", err
	}
	return cfg.Value, nil
}
func (r *smallTorrentGuardConfigRepo) Set(key, value string) error {
	r.values[key] = value
	return nil
}
func (r *smallTorrentGuardConfigRepo) Delete(key string) error {
	delete(r.values, key)
	return nil
}
func (r *smallTorrentGuardConfigRepo) GetAll() ([]model.Config, error) {
	configs := make([]model.Config, 0, len(r.values))
	for key, value := range r.values {
		configs = append(configs, model.Config{Key: key, Value: value})
	}
	return configs, nil
}
