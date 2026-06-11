package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionDiagnosticsHandler_GetAggregatesHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))

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
		Status:         model.DownloadStatusCompleted,
	}))

	handler := NewSubscriptionDiagnosticsHandler(subRepo, downloadRepo, configRepo, nil, t.TempDir())
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
	require.Len(t, resp.Data.Downloads.FailedItems, 1)
	require.Equal(t, "qbittorrent", resp.Data.Downloads.FailedItems[0].Category)

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
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))

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
	require.NoError(t, downloadRepo.Create(&model.Download{
		SubscriptionID: sub.ID,
		Title:          "Retry Anime 02",
		Episode:        2,
		TorrentHash:    "other-hash",
		Status:         model.DownloadStatusFailed,
	}))

	handler := NewSubscriptionDiagnosticsHandler(subRepo, downloadRepo, nil, nil, t.TempDir())
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

	updated, err := downloadRepo.GetByID(failed.ID)
	require.NoError(t, err)
	require.Equal(t, model.DownloadStatusPending, updated.Status)
	require.Equal(t, 0, updated.RetryCount)
	require.Empty(t, updated.TorrentHash)
	require.Empty(t, updated.LastError)
	require.Equal(t, "user_retry", updated.RetryReason)
}
