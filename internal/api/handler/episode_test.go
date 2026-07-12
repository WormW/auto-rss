package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type episodeAPIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}

func setupEpisodeHandlerTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))

	subscriptionRepo := repository.NewSubscriptionRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	handler := NewEpisodeHandler(subscriptionRepo, episodeRepo, episode.NewService(episodeRepo))
	router := gin.New()
	router.GET("/subscriptions/:id/episodes", handler.List)
	router.PUT("/subscriptions/:id/episodes/status", handler.UpdateStatus)
	router.GET("/subscriptions/:id/episodes/:episode/candidates", handler.ListCandidates)
	router.POST("/subscriptions/:id/episodes/:episode/candidates/:candidate_id/keep", handler.KeepCandidate)
	return db, router
}

func performEpisodeRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeEpisodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) episodeAPIResponse {
	t.Helper()
	var response episodeAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func seedEpisodeSubscription(t *testing.T, db *gorm.DB, total int) model.Subscription {
	t.Helper()
	sub := model.Subscription{Name: "ledger", RssURL: "https://example.test/feed", TotalEpisodes: total}
	require.NoError(t, db.Create(&sub).Error)
	return sub
}

func TestEpisodeHandlerListReturnsLedgerWithBatchedActionCounts(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 2)
	episodes := []model.SubscriptionEpisode{
		{SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
		{SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusMissing, StatusSource: model.EpisodeStatusSourceAutomatic},
	}
	require.NoError(t, db.Create(&episodes).Error)
	for index, status := range []string{
		model.CandidateStatusPending,
		model.CandidateStatusFailed,
		model.CandidateStatusAcceptedCleanupFailed,
		model.CandidateStatusAccepted,
		model.CandidateStatusKeptExisting,
	} {
		require.NoError(t, db.Create(&model.EpisodeResourceCandidate{
			SubscriptionEpisodeID: episodes[0].ID,
			ResourceKey:           fmt.Sprintf("hash:%d", index),
			Status:                status,
		}).Error)
	}

	recorder := performEpisodeRequest(router, http.MethodGet, fmt.Sprintf("/subscriptions/%d/episodes", sub.ID), "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	response := decodeEpisodeResponse(t, recorder)
	var items []repository.EpisodeWithCandidateCount
	require.NoError(t, json.Unmarshal(response.Data, &items))
	require.Len(t, items, 2)
	assert.Equal(t, 1, items[0].Episode)
	assert.EqualValues(t, 3, items[0].ActionRequiredCandidateCount)
	assert.EqualValues(t, 0, items[1].ActionRequiredCandidateCount)
}

func TestEpisodeHandlerUpdateStatusCreatesEpisodesAndRefreshesProgress(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 3)
	require.NoError(t, db.Create(&model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic,
	}).Error)

	recorder := performEpisodeRequest(router, http.MethodPut, fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID), `{"episodes":[2,3],"status":"marked_downloaded"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var ledgers []model.SubscriptionEpisode
	require.NoError(t, db.Where("subscription_id = ?", sub.ID).Order("episode").Find(&ledgers).Error)
	require.Len(t, ledgers, 3)
	assert.Equal(t, model.EpisodeStatusMarkedDownloaded, ledgers[1].Status)
	assert.Equal(t, model.EpisodeStatusSourceUser, ledgers[1].StatusSource)
	assert.Equal(t, model.EpisodeStatusMarkedDownloaded, ledgers[2].Status)
	require.NoError(t, db.First(&sub, sub.ID).Error)
	assert.Equal(t, 3, sub.CurrentEpisode)
	require.NotNil(t, sub.CompletedAt)

	recorder = performEpisodeRequest(router, http.MethodPut, fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID), `{"episodes":[2],"status":"missing"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var reopened model.Subscription
	require.NoError(t, db.First(&reopened, sub.ID).Error)
	assert.Equal(t, 1, reopened.CurrentEpisode)
	assert.Nil(t, reopened.CompletedAt)
}

func TestEpisodeHandlerUpdateStatusRejectsActiveDownload(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)
	for _, status := range []string{
		model.DownloadStatusPending,
		model.DownloadStatusDownloading,
		model.DownloadStatusStalled,
		model.DownloadStatusOrganizing,
	} {
		t.Run(status, func(t *testing.T) {
			require.NoError(t, db.Where("subscription_id = ?", sub.ID).Delete(&model.SubscriptionEpisode{}).Error)
			require.NoError(t, db.Where("subscription_id = ?", sub.ID).Delete(&model.Download{}).Error)
			download := model.Download{SubscriptionID: sub.ID, Episode: 1, Title: status, TorrentURL: "magnet:" + status, TorrentHash: "hash-" + status, Status: status}
			require.NoError(t, db.Create(&download).Error)
			require.NoError(t, db.Create(&model.SubscriptionEpisode{
				SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
				StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
			}).Error)

			recorder := performEpisodeRequest(router, http.MethodPut, fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID), `{"episodes":[1],"status":"missing"}`)
			require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
			response := decodeEpisodeResponse(t, recorder)
			assert.Equal(t, http.StatusConflict, response.Code)
			assert.Equal(t, "active_download_must_be_resolved", response.Reason)

			var persisted model.SubscriptionEpisode
			require.NoError(t, db.Where("subscription_id = ? AND episode = ?", sub.ID, 1).First(&persisted).Error)
			assert.Equal(t, model.EpisodeStatusDownloading, persisted.Status)
			assert.NotNil(t, persisted.ActiveDownloadID)
		})
	}
}

func TestEpisodeHandlerUpdateStatusValidation(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)
	path := fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID)
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{`},
		{name: "missing episodes", body: `{"status":"ignored"}`},
		{name: "empty episodes", body: `{"episodes":[],"status":"ignored"}`},
		{name: "unknown status", body: `{"episodes":[1],"status":"downloaded"}`},
		{name: "zero episode", body: `{"episodes":[0],"status":"ignored"}`},
		{name: "negative episode", body: `{"episodes":[-1],"status":"ignored"}`},
		{name: "duplicate episode", body: `{"episodes":[1,1],"status":"ignored"}`},
		{name: "episode above limit", body: fmt.Sprintf(`{"episodes":[%d],"status":"ignored"}`, model.MaxSubscriptionEpisodes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performEpisodeRequest(router, http.MethodPut, path, test.body)
			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		})
	}

	for _, path := range []string{
		"/subscriptions/not-an-id/episodes/status",
		"/subscriptions/0/episodes/status",
	} {
		recorder := performEpisodeRequest(router, http.MethodPut, path, `{"episodes":[1],"status":"ignored"}`)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestEpisodeHandlerCandidateListAndKeepAreStrictlyScopedAndIdempotent(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 2)
	otherSub := seedEpisodeSubscription(t, db, 1)
	episodes := []model.SubscriptionEpisode{
		{SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
		{SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
		{SubscriptionID: otherSub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
	}
	require.NoError(t, db.Create(&episodes).Error)
	candidates := []model.EpisodeResourceCandidate{
		{SubscriptionEpisodeID: episodes[0].ID, ResourceKey: "hash:one", TorrentHash: "one", Status: model.CandidateStatusPending},
		{SubscriptionEpisodeID: episodes[1].ID, ResourceKey: "hash:two", TorrentHash: "two", Status: model.CandidateStatusPending},
		{SubscriptionEpisodeID: episodes[2].ID, ResourceKey: "hash:other", TorrentHash: "other", Status: model.CandidateStatusPending},
	}
	require.NoError(t, db.Create(&candidates).Error)

	listPath := fmt.Sprintf("/subscriptions/%d/episodes/1/candidates", sub.ID)
	recorder := performEpisodeRequest(router, http.MethodGet, listPath, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	response := decodeEpisodeResponse(t, recorder)
	var listed []model.EpisodeResourceCandidate
	require.NoError(t, json.Unmarshal(response.Data, &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, candidates[0].ID, listed[0].ID)

	keepPath := fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/keep", sub.ID, candidates[0].ID)
	for range 2 {
		recorder = performEpisodeRequest(router, http.MethodPost, keepPath, "")
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		response = decodeEpisodeResponse(t, recorder)
		var kept model.EpisodeResourceCandidate
		require.NoError(t, json.Unmarshal(response.Data, &kept))
		assert.Equal(t, model.CandidateStatusKeptExisting, kept.Status)
	}

	wrongPaths := []string{
		fmt.Sprintf("/subscriptions/%d/episodes/2/candidates/%d/keep", sub.ID, candidates[0].ID),
		fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/keep", otherSub.ID, candidates[0].ID),
		fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/keep", sub.ID, candidates[2].ID),
	}
	for _, wrongPath := range wrongPaths {
		recorder = performEpisodeRequest(router, http.MethodPost, wrongPath, "")
		assert.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
	}

	var untouched model.EpisodeResourceCandidate
	require.NoError(t, db.First(&untouched, candidates[1].ID).Error)
	assert.Equal(t, model.CandidateStatusPending, untouched.Status)
}

func TestEpisodeHandlerMapsUnknownResourcesMalformedIDsAndRepositoryErrors(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "unknown subscription list", method: http.MethodGet, path: "/subscriptions/99999/episodes", status: http.StatusNotFound},
		{name: "unknown subscription status", method: http.MethodPut, path: "/subscriptions/99999/episodes/status", body: `{"episodes":[1],"status":"ignored"}`, status: http.StatusNotFound},
		{name: "unknown episode candidates", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/1/candidates", sub.ID), status: http.StatusNotFound},
		{name: "malformed subscription", method: http.MethodGet, path: "/subscriptions/nope/episodes", status: http.StatusBadRequest},
		{name: "malformed episode", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/nope/candidates", sub.ID), status: http.StatusBadRequest},
		{name: "zero episode", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/0/candidates", sub.ID), status: http.StatusBadRequest},
		{name: "episode above limit", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/%d/candidates", sub.ID, model.MaxSubscriptionEpisodes+1), status: http.StatusBadRequest},
		{name: "malformed candidate", method: http.MethodPost, path: fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/nope/keep", sub.ID), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performEpisodeRequest(router, test.method, test.path, test.body)
			assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
		})
	}

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	recorder := performEpisodeRequest(router, http.MethodGet, fmt.Sprintf("/subscriptions/%d/episodes", sub.ID), "")
	assert.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
}
