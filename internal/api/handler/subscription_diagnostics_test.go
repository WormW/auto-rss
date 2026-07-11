package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	episodeservice "github.com/WormW/auto-rss/internal/service/episode"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionDiagnosticsHandler_GetAggregatesHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}, &model.Config{},
	))

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)

	lastCheck := time.Now().Add(-48 * time.Hour)
	sub := model.Subscription{
		Name:           "Test Anime",
		Enabled:        true,
		RssURL:         "",
		Season:         1,
		CurrentEpisode: 3,
		LatestEpisode:  5,
		RenameEnabled:  true,
		LastCheckTime:  &lastCheck,
	}
	require.NoError(t, subRepo.Create(&sub))
	require.NoError(t, downloadRepo.Create(&model.Download{
		SubscriptionID: sub.ID,
		Title:          "Test Anime 04",
		Episode:        4,
		TorrentURL:     "magnet:?xt=urn:btih:1111111111111111111111111111111111111111",
		Status:         model.DownloadStatusFailed,
		LastError:      "qBittorrent add torrent failed",
		MaxRetries:     3,
	}))
	require.NoError(t, downloadRepo.Create(&model.Download{
		SubscriptionID: sub.ID,
		Title:          "Test Anime 03",
		Episode:        3,
		TorrentURL:     "magnet:?xt=urn:btih:2222222222222222222222222222222222222222",
		TorrentHash:    "2222222222222222222222222222222222222222",
		FilePath:       "/path-that-must-not-be-statted/test-anime-03.mkv",
		Status:         model.DownloadStatusCompleted,
	}))

	basePath := t.TempDir()
	require.NoError(t, os.MkdirAll(utils.GenerateDownloadPath(basePath, sub.Name), 0755))
	handler := NewSubscriptionDiagnosticsHandler(subRepo, downloadRepo, configRepo, nil, basePath)
	r := gin.New()
	r.GET("/subscriptions/:id/diagnostics", handler.Get)

	req := httptest.NewRequest(http.MethodGet, "/subscriptions/1/diagnostics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int                             `json:"code"`
		Data SubscriptionDiagnosticsResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "error", resp.Data.Summary.Overall)
	require.Equal(t, 1, resp.Data.Downloads.Failed)
	require.Equal(t, 1, resp.Data.Downloads.Retryable)
	require.Equal(t, []int{4, 5}, resp.Data.Files.MissingEpisodes)
	require.Equal(t, 1, resp.Data.Files.CompletedWithFile)
	require.Equal(t, 0, resp.Data.Files.CompletedMissingFile)
	require.Len(t, resp.Data.Downloads.FailedItems, 1)
	require.Equal(t, "qbittorrent", resp.Data.Downloads.FailedItems[0].Category)

	checkByKey := map[string]SubscriptionDiagnosticCheck{}
	for _, check := range resp.Data.Checks {
		checkByKey[check.Key] = check
	}
	require.Contains(t, checkByKey["files"].Summary, "记录")

	actionByKey := map[string]SubscriptionDiagnosticAction{}
	for _, action := range resp.Data.Actions {
		actionByKey[action.Key] = action
	}
	require.False(t, actionByKey["refresh_rss"].Enabled)
	require.True(t, actionByKey["retry_failed"].Enabled)
	require.True(t, actionByKey["scan_files"].Enabled)
}

func TestSubscriptionDiagnosticsHandler_RetryFailedResetsRetryableDownloads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}, &model.Config{},
	))

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	sub := model.Subscription{Name: "Retry Anime", Enabled: true, Season: 1}
	require.NoError(t, subRepo.Create(&sub))

	failed := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Retry Anime 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:3333333333333333333333333333333333333333",
		TorrentHash:    "old-hash",
		Status:         model.DownloadStatusFailed,
		RetryCount:     5,
		MaxRetries:     5,
		LastError:      "timeout",
	}
	require.NoError(t, downloadRepo.Create(&failed))
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID,
		Episode:        1,
		Status:         model.EpisodeStatusMissing,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, db.Create(&ledger).Error)
	require.NoError(t, downloadRepo.Create(&model.Download{
		SubscriptionID: sub.ID,
		Title:          "Retry Anime 02",
		Episode:        2,
		TorrentHash:    "other-hash",
		Status:         model.DownloadStatusFailed,
	}))

	qb := &mockQBittorrentClient{}
	episodeRepo := repository.NewEpisodeRepository(db)
	handler := NewSubscriptionDiagnosticsHandler(subRepo, downloadRepo, nil, qb, t.TempDir(), episodeservice.NewService(episodeRepo))
	r := gin.New()
	r.POST("/subscriptions/:id/diagnostics/retry-failed", handler.RetryFailed)

	req := httptest.NewRequest(http.MethodPost, "/subscriptions/1/diagnostics/retry-failed", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int                             `json:"code"`
		Data SubscriptionRetryFailedResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Data.Retried)
	require.Equal(t, 1, resp.Data.Skipped)
	require.Equal(t, 0, resp.Data.Failed)
	require.Len(t, resp.Data.Results, 2)
	var retryResult *SubscriptionRetryResult
	for i := range resp.Data.Results {
		if resp.Data.Results[i].ID == failed.ID {
			retryResult = &resp.Data.Results[i]
			break
		}
	}
	require.NotNil(t, retryResult)
	require.Equal(t, model.DownloadStatusPending, retryResult.Status)
	require.Equal(t, "已重置为待下载，等待后台调度", retryResult.Message)

	updated, err := downloadRepo.GetByID(failed.ID)
	require.NoError(t, err)
	require.Equal(t, model.DownloadStatusPending, updated.Status)
	require.Equal(t, 0, updated.RetryCount)
	require.Empty(t, updated.TorrentHash)
	require.Empty(t, updated.LastError)
	require.Equal(t, "user_retry", updated.RetryReason)
	require.True(t, qb.deleteWithPayloadCalled)
	require.Equal(t, "old-hash", qb.deletedHash)
	require.False(t, qb.addCalled, "DownloadMonitor must own the first qBittorrent Add")
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	require.Equal(t, model.EpisodeStatusDownloading, after.Status)
	require.NotNil(t, after.ActiveDownloadID)
	require.Equal(t, failed.ID, *after.ActiveDownloadID)
}
