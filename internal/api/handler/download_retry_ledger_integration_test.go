package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	episodeservice "github.com/WormW/auto-rss/internal/service/episode"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTerminalManualRetryDoesNotStealEpisodeFromOtherDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manual-retry-conflict.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{},
	))
	sub := model.Subscription{Name: "Retry Conflict", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	oldDownload := model.Download{
		SubscriptionID: sub.ID, Title: "old terminal", Episode: 1,
		TorrentURL: "magnet:old", TorrentHash: "old-hash", Status: model.DownloadStatusFailed,
		RetryCount: 3, MaxRetries: 3, LastError: "retry exhausted",
	}
	require.NoError(t, db.Create(&oldDownload).Error)
	otherDownload := model.Download{
		SubscriptionID: sub.ID, Title: "new owner", Episode: 1,
		TorrentURL: "magnet:new", TorrentHash: "new-hash", Status: model.DownloadStatusDownloading,
	}
	require.NoError(t, db.Create(&otherDownload).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &otherDownload.ID,
		ActiveTorrentHash: otherDownload.TorrentHash, ActiveTorrentURL: otherDownload.TorrentURL,
		ActiveTitle: otherDownload.Title,
	}
	require.NoError(t, db.Create(&ledger).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	episodeService := episodeservice.NewService(episodeRepo)
	qb := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, qb, nil, episodeService)
	router := gin.New()
	router.POST("/downloads/:id/retry", handler.Retry)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/downloads/%d/retry", oldDownload.ID),
		nil,
	))

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	persistedOld, err := downloadRepo.GetByID(oldDownload.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusFailed, persistedOld.Status)
	assert.Equal(t, 3, persistedOld.RetryCount)
	assert.Equal(t, "old-hash", persistedOld.TorrentHash)
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, after.Status)
	require.NotNil(t, after.ActiveDownloadID)
	assert.Equal(t, otherDownload.ID, *after.ActiveDownloadID)
	assert.False(t, qb.deleteWithPayloadCalled)
}

func TestManualRetryOwnedEpisodeGuardsDoNotDeletePayload(t *testing.T) {
	for _, status := range []string{
		model.EpisodeStatusDownloaded,
		model.EpisodeStatusIgnored,
		model.EpisodeStatusMarkedDownloaded,
	} {
		t.Run(status, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manual-retry-guard.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(
				&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{},
			))
			sub := model.Subscription{Name: "Retry Guard " + status, Status: "active"}
			require.NoError(t, db.Create(&sub).Error)
			download := model.Download{
				SubscriptionID: sub.ID, Title: status, Episode: 1,
				TorrentURL: "magnet:" + status, TorrentHash: "hash-" + status,
				Status: model.DownloadStatusFailed, RetryCount: 3, MaxRetries: 3,
			}
			require.NoError(t, db.Create(&download).Error)
			ledger := model.SubscriptionEpisode{
				SubscriptionID: sub.ID, Episode: 1, Status: status,
				StatusSource: model.EpisodeStatusSourceUser,
			}
			require.NoError(t, db.Create(&ledger).Error)
			downloadRepo := repository.NewDownloadRepository(db)
			episodeRepo := repository.NewEpisodeRepository(db)
			qb := &mockQBittorrentClient{}
			handler := NewDownloadHandler(downloadRepo, qb, nil, episodeservice.NewService(episodeRepo))
			router := gin.New()
			router.POST("/downloads/:id/retry", handler.Retry)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(
				http.MethodPost, fmt.Sprintf("/downloads/%d/retry", download.ID), nil,
			))

			require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
			assert.False(t, qb.deleteWithPayloadCalled)
			persisted, err := downloadRepo.GetByID(download.ID)
			require.NoError(t, err)
			assert.Equal(t, model.DownloadStatusFailed, persisted.Status)
			assert.Equal(t, "hash-"+status, persisted.TorrentHash)
			after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
			require.NoError(t, err)
			assert.Equal(t, status, after.Status)
		})
	}
}

func TestManualRetrySaveFailureDoesNotDeletePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manual-retry-save-failure.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{},
	))
	sub := model.Subscription{Name: "Retry Save Failure", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID, Title: "save failure", Episode: 1,
		TorrentURL: "magnet:save-failure", TorrentHash: "save-failure-hash",
		Status: model.DownloadStatusFailed, RetryCount: 3, MaxRetries: 3,
	}
	require.NoError(t, db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusMissing,
		StatusSource: model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, db.Create(&ledger).Error)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_handler_requeue
		BEFORE UPDATE OF status ON downloads
		WHEN NEW.status = 'pending'
		BEGIN
			SELECT RAISE(ABORT, 'injected handler requeue failure');
		END;
	`).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	qb := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, qb, nil, episodeservice.NewService(episodeRepo))
	router := gin.New()
	router.POST("/downloads/:id/retry", handler.Retry)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, fmt.Sprintf("/downloads/%d/retry", download.ID), nil,
	))

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	assert.False(t, qb.deleteWithPayloadCalled)
	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusFailed, persisted.Status)
	assert.Equal(t, "save-failure-hash", persisted.TorrentHash)
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, after.Status)
	assert.Nil(t, after.ActiveDownloadID)
}

func TestTerminalManualRetryReclaimsEpisodeThroughCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manual-retry-ledger.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	sub := model.Subscription{Name: "Manual Retry", Status: "active", RenameEnabled: false}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, db.Model(&sub).Update("rename_enabled", false).Error)
	sub.RenameEnabled = false
	download := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Manual Retry - 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:manual-retry",
		TorrentHash:    "manual-old-hash",
		Status:         model.DownloadStatusDownloading,
		RetryCount:     3,
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
	_, err = downloader.NewStatusSync(downloadRepo, nil, episodeService).
		UpdateStatus(persisted, &downloader.TorrentInfo{Hash: download.TorrentHash, State: downloader.StateError})
	require.NoError(t, err)
	failedLedger, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	require.Equal(t, model.EpisodeStatusMissing, failedLedger.Status)
	require.Nil(t, failedLedger.ActiveDownloadID)

	deleteObservedCommittedRequeue := false
	qb := &mockQBittorrentClient{deleteTorrentFunc: func(string, bool) error {
		persisted, downloadErr := downloadRepo.GetByID(download.ID)
		attached, ledgerErr := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
		deleteObservedCommittedRequeue = downloadErr == nil && ledgerErr == nil &&
			persisted.Status == model.DownloadStatusPending && persisted.TorrentHash == "" &&
			attached.Status == model.EpisodeStatusDownloading && attached.ActiveDownloadID != nil &&
			*attached.ActiveDownloadID == download.ID
		return nil
	}}
	handler := NewDownloadHandler(downloadRepo, qb, nil, episodeService)
	router := gin.New()
	router.POST("/downloads/:id/retry", handler.Retry)
	req := httptest.NewRequest(http.MethodPost, "/downloads/1/retry", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	retried, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusPending, retried.Status)
	assert.Empty(t, retried.TorrentHash)
	assert.False(t, qb.addCalled)
	assert.True(t, deleteObservedCommittedRequeue)
	afterRetry, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, afterRetry.Status)
	require.NotNil(t, afterRetry.ActiveDownloadID)
	assert.Equal(t, download.ID, *afterRetry.ActiveDownloadID)

	retried.Status = model.DownloadStatusDownloading
	retried.TorrentHash = "manual-retried-hash"
	require.NoError(t, downloadRepo.Update(retried))
	completion := downloader.NewCompletionHandler(
		nil,
		downloadRepo,
		nil,
		downloader.NewRenameService(""),
		qb,
		db,
		episodeService,
	)
	require.NoError(t, completion.HandleComplete(
		retried,
		&downloader.TorrentInfo{Hash: retried.TorrentHash, SavePath: "/downloads/manual-retry"},
		&sub,
	))

	completed, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, completed.Status)
	afterCompletion, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, afterCompletion.Status)
	require.NotNil(t, afterCompletion.ActiveDownloadID)
	assert.Equal(t, download.ID, *afterCompletion.ActiveDownloadID)
}
