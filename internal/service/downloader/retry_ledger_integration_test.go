package downloader

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

	var completed *model.Download
	var afterCompletion *model.SubscriptionEpisode
	require.Eventually(t, func() bool {
		completed, err = downloadRepo.GetByID(download.ID)
		if err != nil || completed.Status != model.DownloadStatusCompleted {
			return false
		}
		afterCompletion, err = episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
		return err == nil && afterCompletion.Status == model.EpisodeStatusDownloaded
	}, 2*time.Second, 10*time.Millisecond)
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

func TestStatusSyncFailurePersistenceIsAtomicAndRecoverable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "status-sync-atomic.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}))
	sub := model.Subscription{Name: "Status Sync Atomic", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID, Title: "episode", Episode: 1, TorrentURL: "magnet:sync",
		TorrentHash: "sync-atomic-hash", Status: model.DownloadStatusDownloading, RetryCount: 1, MaxRetries: 1,
	}
	require.NoError(t, db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
		ActiveTorrentHash: download.TorrentHash,
	}
	require.NoError(t, db.Create(&ledger).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_status_sync_release BEFORE UPDATE OF status ON subscription_episodes WHEN NEW.status = 'missing' BEGIN SELECT RAISE(ABORT, 'injected status sync release failure'); END;`).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	syncer := NewStatusSync(downloadRepo, nil, episodeservice.NewService(episodeRepo))

	changed, err := syncer.UpdateStatus(persisted, &TorrentInfo{Hash: download.TorrentHash, State: StateError})
	require.Error(t, err)
	assert.False(t, changed)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	reloaded, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, reloaded.Status)
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, after.Status)

	require.NoError(t, db.Exec("DROP TRIGGER fail_status_sync_release").Error)
	changed, err = syncer.UpdateStatus(persisted, &TorrentInfo{Hash: download.TorrentHash, State: StateError})
	require.NoError(t, err)
	assert.True(t, changed)
	reloaded, err = downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusFailed, reloaded.Status)
	after, err = episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, after.Status)
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

func TestDownloadMonitorAddFailurePersistenceIsAtomicAndRecoverable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "monitor-add-atomic.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}))
	sub := model.Subscription{Name: "Monitor Atomic", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID, Title: "episode", Episode: 1, TorrentURL: "magnet:monitor",
		TorrentHash: "monitor-placeholder", Status: model.DownloadStatusPending, RetryCount: 1, MaxRetries: 1,
	}
	require.NoError(t, db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
		ActiveTorrentHash: download.TorrentHash,
	}
	require.NoError(t, db.Create(&ledger).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_monitor_release BEFORE UPDATE OF status ON subscription_episodes WHEN NEW.status = 'missing' BEGIN SELECT RAISE(ABORT, 'injected monitor release failure'); END;`).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	monitor := NewDownloadMonitor(
		db, &retryLedgerQBClient{addErr: errors.New("qB unavailable")}, downloadRepo,
		repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", episodeservice.NewService(episodeRepo),
	)
	monitor.SetNotificationService(nil)

	monitor.processPendingDownloads()
	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusPending, persisted.Status)
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, after.Status)

	require.NoError(t, db.Exec("DROP TRIGGER fail_monitor_release").Error)
	monitor.processPendingDownloads()
	persisted, err = downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusFailed, persisted.Status)
	after, err = episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, after.Status)
}

func TestDownloadMonitorCleansRetryCheckpointBeforeAdd(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cleanup-before-add.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
	sub := model.Subscription{Name: "Cleanup Show", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID, Title: "Cleanup Show - 01", TorrentURL: "magnet:cleanup",
		TorrentHash: "old-cleanup-hash", Status: model.DownloadStatusRetryCleanup,
	}
	require.NoError(t, db.Create(&download).Error)
	qb := &retryLedgerQBClient{returnHash: "new-cleanup-hash"}
	monitor := NewDownloadMonitor(db, qb, repository.NewDownloadRepository(db), repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", nil)
	monitor.SetNotificationService(nil)

	monitor.processPendingDownloads()

	require.Equal(t, []string{"delete", "add"}, qb.operations)
	require.Equal(t, []string{"old-cleanup-hash"}, qb.deletedHashes)
	persisted, err := repository.NewDownloadRepository(db).GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	assert.Equal(t, "new-cleanup-hash", persisted.TorrentHash)
}

func TestDownloadMonitorRetriesCleanupAfterDeleteAndFinalizeFailures(t *testing.T) {
	t.Run("delete failure", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "delete-failure.db")), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
		sub := model.Subscription{Name: "Delete Failure", Status: "active"}
		require.NoError(t, db.Create(&sub).Error)
		download := model.Download{SubscriptionID: sub.ID, Title: "one", TorrentURL: "magnet:one", TorrentHash: "old-delete", Status: model.DownloadStatusRetryCleanup}
		require.NoError(t, db.Create(&download).Error)
		qb := &retryLedgerQBClient{returnHash: "new-delete", deleteErr: errors.New("temporary delete failure")}
		monitor := NewDownloadMonitor(db, qb, repository.NewDownloadRepository(db), repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", nil)
		monitor.SetNotificationService(nil)

		monitor.processPendingDownloads()
		persisted, reloadErr := repository.NewDownloadRepository(db).GetByID(download.ID)
		require.NoError(t, reloadErr)
		assert.Equal(t, model.DownloadStatusRetryCleanup, persisted.Status)
		assert.Equal(t, "old-delete", persisted.TorrentHash)
		assert.NotContains(t, qb.operations, "add")

		qb.deleteErr = nil
		monitor.processPendingDownloads()
		persisted, reloadErr = repository.NewDownloadRepository(db).GetByID(download.ID)
		require.NoError(t, reloadErr)
		assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
		assert.Equal(t, []string{"old-delete", "old-delete"}, qb.deletedHashes)
	})

	t.Run("database finalize failure", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "finalize-failure.db")), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
		sub := model.Subscription{Name: "Finalize Failure", Status: "active"}
		require.NoError(t, db.Create(&sub).Error)
		download := model.Download{SubscriptionID: sub.ID, Title: "one", TorrentURL: "magnet:one", TorrentHash: "old-finalize", Status: model.DownloadStatusRetryCleanup}
		require.NoError(t, db.Create(&download).Error)
		require.NoError(t, db.Exec(`CREATE TRIGGER fail_cleanup_finalize BEFORE UPDATE OF status ON downloads WHEN NEW.status = 'pending' BEGIN SELECT RAISE(ABORT, 'injected finalize failure'); END;`).Error)
		qb := &retryLedgerQBClient{returnHash: "new-finalize"}
		monitor := NewDownloadMonitor(db, qb, repository.NewDownloadRepository(db), repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", nil)
		monitor.SetNotificationService(nil)

		monitor.processPendingDownloads()
		persisted, reloadErr := repository.NewDownloadRepository(db).GetByID(download.ID)
		require.NoError(t, reloadErr)
		assert.Equal(t, model.DownloadStatusRetryCleanup, persisted.Status)
		assert.Equal(t, "old-finalize", persisted.TorrentHash)
		assert.NotContains(t, qb.operations, "add")

		require.NoError(t, db.Exec("DROP TRIGGER fail_cleanup_finalize").Error)
		monitor.processPendingDownloads()
		persisted, reloadErr = repository.NewDownloadRepository(db).GetByID(download.ID)
		require.NoError(t, reloadErr)
		assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
		assert.Equal(t, []string{"old-finalize", "old-finalize"}, qb.deletedHashes)
	})
}

func TestDownloadMonitorUsesUniqueRetryPlaceholders(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "unique-placeholders.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
	sub := model.Subscription{Name: "Parallel Cleanup", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	downloads := []model.Download{
		{SubscriptionID: sub.ID, Title: "one", TorrentURL: "magnet:one", TorrentHash: "old-unique-one", Status: model.DownloadStatusRetryCleanup},
		{SubscriptionID: sub.ID, Title: "two", TorrentURL: "magnet:two", TorrentHash: "old-unique-two", Status: model.DownloadStatusRetryCleanup},
	}
	require.NoError(t, db.Create(&downloads).Error)
	qb := &retryLedgerQBClient{deleteErr: nil}
	monitor := NewDownloadMonitor(db, qb, repository.NewDownloadRepository(db), repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", nil)
	monitor.SetNotificationService(nil)

	monitor.processRetryCleanupDownloads()

	for i := range downloads {
		persisted, reloadErr := repository.NewDownloadRepository(db).GetByID(downloads[i].ID)
		require.NoError(t, reloadErr)
		assert.Equal(t, model.DownloadStatusPending, persisted.Status)
		assert.Equal(t, retryTorrentPlaceholder(downloads[i].ID), persisted.TorrentHash)
	}
}

func TestDownloadMonitorExclusivelyClaimsRetryCleanup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "exclusive-cleanup.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
	download := model.Download{Title: "exclusive", TorrentURL: "magnet:exclusive", TorrentHash: "old-exclusive", Status: model.DownloadStatusRetryCleanup}
	require.NoError(t, db.Create(&download).Error)
	qb := &blockingCleanupQBClient{
		retryLedgerQBClient: retryLedgerQBClient{returnHash: "new-exclusive"},
		started:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	newMonitor := func() *DownloadMonitor {
		monitor := NewDownloadMonitor(db, qb, repository.NewDownloadRepository(db), repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", nil)
		monitor.SetNotificationService(nil)
		return monitor
	}
	done := make(chan struct{})
	go func() {
		newMonitor().processRetryCleanupDownloads()
		close(done)
	}()
	<-qb.started
	newMonitor().processRetryCleanupDownloads()
	close(qb.release)
	<-done

	qb.mu.Lock()
	deleteCalls := qb.deleteCalls
	qb.mu.Unlock()
	assert.Equal(t, 1, deleteCalls)
}

func TestDownloadMonitorRecoversStaleRetryCleanupClaim(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stale-cleanup.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))
	download := model.Download{Title: "stale", TorrentURL: "magnet:stale", TorrentHash: "old-stale", Status: model.DownloadStatusRetryCleanupProcessing}
	require.NoError(t, db.Create(&download).Error)
	require.NoError(t, db.Model(&download).Update("updated_at", time.Now().Add(-retryCleanupLeaseTimeout-time.Minute)).Error)
	qb := &retryLedgerQBClient{}
	monitor := NewDownloadMonitor(db, qb, repository.NewDownloadRepository(db), repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", nil)
	monitor.SetNotificationService(nil)

	monitor.processRetryCleanupDownloads()

	persisted, err := repository.NewDownloadRepository(db).GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusPending, persisted.Status)
	assert.Equal(t, retryTorrentPlaceholder(download.ID), persisted.TorrentHash)
	assert.Equal(t, []string{"old-stale"}, qb.deletedHashes)
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
	deletedHashes    []string
	deleteErr        error
	operations       []string
}

type blockingCleanupQBClient struct {
	retryLedgerQBClient
	mu          sync.Mutex
	deleteCalls int
	started     chan struct{}
	release     chan struct{}
}

func (q *blockingCleanupQBClient) DeleteTorrentWithPayload(hash string) error {
	q.mu.Lock()
	q.deleteCalls++
	call := q.deleteCalls
	q.mu.Unlock()
	if call == 1 {
		close(q.started)
		<-q.release
	}
	return q.retryLedgerQBClient.DeleteTorrentWithPayload(hash)
}

type setCategoryCall struct {
	hash     string
	category string
}

func (q *retryLedgerQBClient) Login(string, string, string) error          { return nil }
func (q *retryLedgerQBClient) TestConnection(string, string, string) error { return nil }
func (q *retryLedgerQBClient) AddTorrent(_ string, _ string, category string) (string, error) {
	q.operations = append(q.operations, "add")
	q.addCategories = append(q.addCategories, category)
	if q.addErr != nil {
		return "", q.addErr
	}
	q.torrents = append(q.torrents, &TorrentInfo{Hash: q.returnHash, State: StateDownloading, Category: category})
	return q.returnHash, nil
}
func (q *retryLedgerQBClient) AddTorrentExclusive(url, savePath, category, expectedHash string) (string, error) {
	return q.AddTorrent(url, savePath, category)
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
func (q *retryLedgerQBClient) PauseTorrent(string) error                      { return nil }
func (q *retryLedgerQBClient) ResumeTorrent(string) error                     { return nil }
func (q *retryLedgerQBClient) RemoveTorrentTask(string) error                 { return nil }
func (q *retryLedgerQBClient) DeleteTorrentWithPayload(hash string) error {
	q.operations = append(q.operations, "delete")
	q.deletedHashes = append(q.deletedHashes, hash)
	return q.deleteErr
}
func (q *retryLedgerQBClient) GetTorrentFiles(string) ([]TorrentFile, error) { return nil, nil }
func (q *retryLedgerQBClient) GetVersion() (string, error)                   { return "", nil }
func (q *retryLedgerQBClient) SetProxy(string) error                         { return nil }
func (q *retryLedgerQBClient) DownloadTorrentFile(string) ([]byte, error)    { return nil, nil }

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
