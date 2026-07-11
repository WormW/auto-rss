package downloader

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	episodeservice "github.com/WormW/auto-rss/internal/service/episode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRetryableFailureKeepsEpisodeAttachedThroughRetryCompletion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/retry-completion.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.Config{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	sub := model.Subscription{Name: "Retry Completion", Status: "active", RenameEnabled: false}
	require.NoError(t, db.Create(&sub).Error)
	sub.RenameEnabled = false
	require.NoError(t, db.Model(&sub).Update("rename_enabled", false).Error)
	download := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Retry Completion - 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:retry-completion",
		TorrentHash:    "initial-hash",
		Status:         model.DownloadStatusDownloading,
		MaxRetries:     3,
	}
	require.NoError(t, db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           1,
		Status:            model.EpisodeStatusDownloading,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &download.ID,
		ActiveTorrentHash: download.TorrentHash,
		ActiveTorrentURL:  download.TorrentURL,
		ActiveTitle:       download.Title,
	}
	require.NoError(t, db.Create(&ledger).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	episodeService := episodeservice.NewService(episodeRepo)

	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	statusSync := NewStatusSync(downloadRepo, nil, episodeService)
	changed, err := statusSync.UpdateStatus(persisted, &TorrentInfo{Hash: download.TorrentHash, State: StateError})
	require.NoError(t, err)
	require.True(t, changed)
	afterFailure, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, afterFailure.Status)
	require.NotNil(t, afterFailure.ActiveDownloadID)
	assert.Equal(t, download.ID, *afterFailure.ActiveDownloadID)

	qb := &retryLedgerQBClient{returnHash: "retried-actual-hash"}
	monitor := NewDownloadMonitor(
		db,
		qb,
		downloadRepo,
		repository.NewSubscriptionRepository(db),
		repository.NewConfigRepository(db),
		"",
		episodeService,
	)
	monitor.SetNotificationService(nil)
	monitor.checkDownloads()
	retried, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, retried.Status)
	assert.Equal(t, "retried-actual-hash", retried.TorrentHash)
	require.Len(t, qb.torrents, 1)
	qb.torrents[0].State = StateCompleted
	qb.torrents[0].Progress = 1
	qb.torrents[0].SavePath = "/downloads/retry-completion"
	monitor.checkDownloads()

	completed, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, completed.Status)
	afterCompletion, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, afterCompletion.Status)
	require.NotNil(t, afterCompletion.ActiveDownloadID)
	assert.Equal(t, download.ID, *afterCompletion.ActiveDownloadID)
}

func TestFailureTerminalityUpdatesRealEpisodeLedger(t *testing.T) {
	tests := []struct {
		name         string
		retryCount   int
		maxRetries   int
		lastError    string
		wantStatus   string
		wantAttached bool
	}{
		{name: "retryable", retryCount: 0, maxRetries: 3, wantStatus: model.EpisodeStatusDownloading, wantAttached: true},
		{name: "unlimited", retryCount: 100, maxRetries: 0, wantStatus: model.EpisodeStatusDownloading, wantAttached: true},
		{name: "exhausted", retryCount: 3, maxRetries: 3, wantStatus: model.EpisodeStatusMissing},
		{name: "non_retryable", retryCount: 0, maxRetries: 3, lastError: "invalid torrent", wantStatus: model.EpisodeStatusMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "terminality.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(
				&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{},
			))
			sub := model.Subscription{Name: "Terminality " + tt.name, Status: "active"}
			require.NoError(t, db.Create(&sub).Error)
			download := model.Download{
				SubscriptionID: sub.ID, Title: tt.name, Episode: 1,
				TorrentURL: "magnet:" + tt.name, TorrentHash: "hash-" + tt.name,
				Status: model.DownloadStatusDownloading, RetryCount: tt.retryCount,
				MaxRetries: tt.maxRetries, LastError: tt.lastError,
			}
			require.NoError(t, db.Create(&download).Error)
			require.NoError(t, db.Model(&download).Update("max_retries", tt.maxRetries).Error)
			ledger := model.SubscriptionEpisode{
				SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
				StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
				ActiveTorrentHash: download.TorrentHash,
			}
			require.NoError(t, db.Create(&ledger).Error)
			downloadRepo := repository.NewDownloadRepository(db)
			episodeRepo := repository.NewEpisodeRepository(db)
			persisted, err := downloadRepo.GetByID(download.ID)
			require.NoError(t, err)

			_, err = NewStatusSync(downloadRepo, nil, episodeservice.NewService(episodeRepo)).
				UpdateStatus(persisted, &TorrentInfo{Hash: download.TorrentHash, State: StateError})
			require.NoError(t, err)

			after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, after.Status)
			if tt.wantAttached {
				require.NotNil(t, after.ActiveDownloadID)
				assert.Equal(t, download.ID, *after.ActiveDownloadID)
			} else {
				assert.Nil(t, after.ActiveDownloadID)
			}
		})
	}
}

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
		nil,
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
		nil,
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

func TestDownloadMonitorMarksEpisodeFailedWhenPendingAddExhaustsRetries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/pending-failure.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
	sub := model.Subscription{Name: "Failure Show", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Failure Show - 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:pending-failure",
		Status:         model.DownloadStatusPending,
		RetryCount:     1,
		MaxRetries:     1,
	}
	require.NoError(t, db.Create(&download).Error)
	episodes := &mockEpisodeCompletionService{}
	monitor := NewDownloadMonitor(
		db,
		&retryLedgerQBClient{addErr: errors.New("qB unavailable")},
		repository.NewDownloadRepository(db),
		repository.NewSubscriptionRepository(db),
		repository.NewConfigRepository(db),
		"",
		episodes,
	)
	monitor.SetNotificationService(nil)

	monitor.processPendingDownloads()

	assert.Equal(t, []uint{download.ID}, episodes.failedIDs)
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
	addErr           error
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
	if q.addErr != nil {
		return "", q.addErr
	}
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
