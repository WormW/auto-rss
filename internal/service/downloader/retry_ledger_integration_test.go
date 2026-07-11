package downloader

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadMonitorOwnsFirstAddAndReplacesRSSHash(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/retry-ledger.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.Config{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	sub := model.Subscription{Name: "Retry Anime", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Retry Anime - 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:retry-ledger-hash",
		TorrentHash:    "rss-resource-hash",
		Status:         model.DownloadStatusPending,
		Purpose:        model.DownloadPurposeNormal,
		MaxRetries:     5,
	}
	require.NoError(t, db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           1,
		Status:            model.EpisodeStatusDownloading,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &download.ID,
		ActiveTorrentHash: "rss-resource-hash",
		ActiveTorrentURL:  download.TorrentURL,
		ActiveTitle:       download.Title,
	}
	require.NoError(t, db.Create(&ledger).Error)

	downloadRepo := repository.NewDownloadRepository(db)
	qb := &retryLedgerQBClient{returnHash: "retry-success-hash"}
	monitor := NewDownloadMonitor(
		db,
		qb,
		downloadRepo,
		repository.NewSubscriptionRepository(db),
		repository.NewConfigRepository(db),
		"",
	)
	monitor.SetNotificationService(nil)
	monitor.checkDownloads()

	after, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, after.Status)
	assert.Equal(t, "retry-success-hash", after.TorrentHash)
	assert.Equal(t, []string{AutoRssCategory}, qb.addCategories)
	afterLedger, err := repository.NewEpisodeRepository(db).GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, afterLedger.Status)
	require.NotNil(t, afterLedger.ActiveDownloadID)
	assert.Equal(t, download.ID, *afterLedger.ActiveDownloadID)
}

func TestDownloadMonitorLeavesPendingOutboxUntouchedWhileDownloadsPaused(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/paused-outbox.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
	sub := model.Subscription{Name: "Paused Anime", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Paused Anime - 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:paused-outbox-hash",
		TorrentHash:    "paused-rss-hash",
		Status:         model.DownloadStatusPending,
		Purpose:        model.DownloadPurposeNormal,
	}
	require.NoError(t, db.Create(&download).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	qb := &retryLedgerQBClient{returnHash: "paused-actual-hash"}
	monitor := NewDownloadMonitor(
		db,
		qb,
		downloadRepo,
		repository.NewSubscriptionRepository(db),
		repository.NewConfigRepository(db),
		"",
	)
	monitor.SetNotificationService(nil)
	paused := true
	monitor.downloadsPaused = func() bool { return paused }

	monitor.checkDownloads()

	assert.Empty(t, qb.addCategories)
	afterPause, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusPending, afterPause.Status)
	assert.Equal(t, "paused-rss-hash", afterPause.TorrentHash)

	paused = false
	monitor.checkDownloads()

	assert.Equal(t, []string{AutoRssCategory}, qb.addCategories)
	afterResume, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, afterResume.Status)
	assert.Equal(t, "paused-actual-hash", afterResume.TorrentHash)
}

type retryLedgerQBClient struct {
	returnHash    string
	torrents      []*TorrentInfo
	addCategories []string
}

func (q *retryLedgerQBClient) Login(string, string, string) error          { return nil }
func (q *retryLedgerQBClient) TestConnection(string, string, string) error { return nil }
func (q *retryLedgerQBClient) AddTorrent(_ string, _ string, category string) (string, error) {
	q.addCategories = append(q.addCategories, category)
	q.torrents = []*TorrentInfo{{Hash: q.returnHash, State: StateDownloading}}
	return q.returnHash, nil
}
func (q *retryLedgerQBClient) AddTorrentFile(string, []byte, string, string) (string, error) {
	return q.returnHash, nil
}
func (q *retryLedgerQBClient) GetTorrentInfo(string) (*TorrentInfo, error) { return nil, nil }
func (q *retryLedgerQBClient) GetTorrentsByCategory(string) ([]*TorrentInfo, error) {
	return q.torrents, nil
}
func (q *retryLedgerQBClient) SetCategory(string, string) error               { return nil }
func (q *retryLedgerQBClient) SetLocation(string, string) error               { return nil }
func (q *retryLedgerQBClient) RenameTorrentFile(string, string, string) error { return nil }
func (q *retryLedgerQBClient) RemoveTorrentTask(string) error                 { return nil }
func (q *retryLedgerQBClient) DeleteTorrentWithPayload(string) error          { return nil }
func (q *retryLedgerQBClient) GetTorrentFiles(string) ([]TorrentFile, error)  { return nil, nil }
func (q *retryLedgerQBClient) GetVersion() (string, error)                    { return "", nil }
func (q *retryLedgerQBClient) SetProxy(string) error                          { return nil }
func (q *retryLedgerQBClient) DownloadTorrentFile(string) ([]byte, error)     { return nil, nil }

var _ QBittorrentClient = (*retryLedgerQBClient)(nil)
