package downloader

import (
	"errors"
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
	failingRepo := &failOnceHashCheckpointRepository{
		DownloadRepository: downloadRepo,
		actualHash:         "actual-hash",
		failNextCheckpoint: true,
	}
	qb := &retryLedgerQBClient{returnHash: "actual-hash"}
	monitor := NewDownloadMonitor(
		db,
		qb,
		failingRepo,
		repository.NewSubscriptionRepository(db),
		repository.NewConfigRepository(db),
		"",
	)
	monitor.SetNotificationService(nil)
	monitor.checkDownloads()

	afterFirstRun, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusPending, afterFirstRun.Status)
	assert.Equal(t, "rss-resource-hash", afterFirstRun.TorrentHash)
	assert.Equal(t, []string{pendingDownloadCategory(download.ID)}, qb.addCategories)
	assert.Equal(t, pendingDownloadCategory(download.ID), qb.categoryForHash("actual-hash"))
	assert.Empty(t, qb.setCategoryCalls)

	monitor.checkDownloads()

	afterSecondRun, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, afterSecondRun.Status)
	assert.Equal(t, "actual-hash", afterSecondRun.TorrentHash)
	assert.Equal(t, AutoRssCategory, qb.categoryForHash("actual-hash"))
	assert.Equal(t, []setCategoryCall{{hash: "actual-hash", category: AutoRssCategory}}, qb.setCategoryCalls)
	assert.Contains(t, qb.queryCategories, "")
	assert.Contains(t, qb.queryCategories, AutoRssCategory)
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

	assert.Equal(t, []string{pendingDownloadCategory(download.ID)}, qb.addCategories)
	afterResume, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, afterResume.Status)
	assert.Equal(t, "paused-actual-hash", afterResume.TorrentHash)
	assert.Equal(t, AutoRssCategory, qb.categoryForHash("paused-actual-hash"))
}

func TestParsePendingDownloadCategory(t *testing.T) {
	tests := []struct {
		category string
		wantID   uint
		wantOK   bool
	}{
		{category: "AutoRss:pending:42", wantID: 42, wantOK: true},
		{category: "AutoRss:pending:0"},
		{category: "AutoRss:pending:01"},
		{category: "AutoRss:pending:-1"},
		{category: "AutoRss:pending:+1"},
		{category: "AutoRss:pending:1:extra"},
		{category: "AutoRss:pending: 1"},
		{category: "autorss:pending:1"},
		{category: "AutoRss"},
		{category: "AutoRss:pending:18446744073709551616"},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			id, ok := parsePendingDownloadCategory(tt.category)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

type retryLedgerQBClient struct {
	returnHash       string
	torrents         []*TorrentInfo
	addCategories    []string
	queryCategories  []string
	setCategoryCalls []setCategoryCall
}

type setCategoryCall struct {
	hash     string
	category string
}

func (q *retryLedgerQBClient) Login(string, string, string) error          { return nil }
func (q *retryLedgerQBClient) TestConnection(string, string, string) error { return nil }
func (q *retryLedgerQBClient) AddTorrent(_ string, _ string, category string) (string, error) {
	q.addCategories = append(q.addCategories, category)
	q.torrents = append(q.torrents, &TorrentInfo{Hash: q.returnHash, State: StateDownloading, Category: category})
	return q.returnHash, nil
}
func (q *retryLedgerQBClient) AddTorrentFile(_ string, _ []byte, _ string, category string) (string, error) {
	return q.AddTorrent("", "", category)
}
func (q *retryLedgerQBClient) GetTorrentInfo(string) (*TorrentInfo, error) { return nil, nil }
func (q *retryLedgerQBClient) GetTorrentsByCategory(category string) ([]*TorrentInfo, error) {
	q.queryCategories = append(q.queryCategories, category)
	var result []*TorrentInfo
	for _, torrent := range q.torrents {
		if category == "" || torrent.Category == category {
			result = append(result, torrent)
		}
	}
	return result, nil
}
func (q *retryLedgerQBClient) SetCategory(hash, category string) error {
	q.setCategoryCalls = append(q.setCategoryCalls, setCategoryCall{hash: hash, category: category})
	for _, torrent := range q.torrents {
		if torrent.Hash == hash {
			torrent.Category = category
		}
	}
	return nil
}
func (q *retryLedgerQBClient) SetLocation(string, string) error               { return nil }
func (q *retryLedgerQBClient) RenameTorrentFile(string, string, string) error { return nil }
func (q *retryLedgerQBClient) RemoveTorrentTask(string) error                 { return nil }
func (q *retryLedgerQBClient) DeleteTorrentWithPayload(string) error          { return nil }
func (q *retryLedgerQBClient) GetTorrentFiles(string) ([]TorrentFile, error)  { return nil, nil }
func (q *retryLedgerQBClient) GetVersion() (string, error)                    { return "", nil }
func (q *retryLedgerQBClient) SetProxy(string) error                          { return nil }
func (q *retryLedgerQBClient) DownloadTorrentFile(string) ([]byte, error)     { return nil, nil }

var _ QBittorrentClient = (*retryLedgerQBClient)(nil)

func (q *retryLedgerQBClient) categoryForHash(hash string) string {
	for _, torrent := range q.torrents {
		if torrent.Hash == hash {
			return torrent.Category
		}
	}
	return ""
}

type failOnceHashCheckpointRepository struct {
	repository.DownloadRepository
	actualHash         string
	failNextCheckpoint bool
}

func (r *failOnceHashCheckpointRepository) Update(download *model.Download) error {
	if r.failNextCheckpoint && download.Status == model.DownloadStatusPending && download.TorrentHash == r.actualHash {
		r.failNextCheckpoint = false
		return errors.New("injected hash checkpoint failure")
	}
	return r.DownloadRepository.Update(download)
}
