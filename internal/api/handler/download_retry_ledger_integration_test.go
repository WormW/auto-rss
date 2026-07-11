package handler

import (
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

func TestManualRetryKeepsEpisodeAttachedThroughCompletion(t *testing.T) {
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

	qb := &mockQBittorrentClient{}
	handler := NewDownloadHandler(downloadRepo, qb, nil)
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
