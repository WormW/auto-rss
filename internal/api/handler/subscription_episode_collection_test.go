package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type episodeCollectionFixture struct {
	db           *gorm.DB
	handler      *SubscriptionHandler
	subRepo      repository.SubscriptionRepository
	downloadRepo repository.DownloadRepository
	episodeRepo  repository.EpisodeRepository
	qb           *collectEpisodesSmallTorrentQBClient
}

func newEpisodeCollectionFixture(t *testing.T) *episodeCollectionFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.Config{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	qb := &collectEpisodesSmallTorrentQBClient{}
	h := NewSubscriptionHandler(subRepo, downloadRepo, repository.NewConfigRepository(db), qb, t.TempDir(), episodeRepo)
	return &episodeCollectionFixture{
		db:           db,
		handler:      h,
		subRepo:      subRepo,
		downloadRepo: downloadRepo,
		episodeRepo:  episodeRepo,
		qb:           qb,
	}
}

func (fx *episodeCollectionFixture) createSubscription(t *testing.T, watermark *time.Time) model.Subscription {
	t.Helper()
	sub := model.Subscription{
		Name:           "Ledger Show",
		RssURL:         "https://example.com/feed.xml",
		Status:         "active",
		Enabled:        true,
		TotalEpisodes:  12,
		LastRSSPubTime: watermark,
	}
	require.NoError(t, fx.db.Create(&sub).Error)
	return sub
}

func seedDownloadedEpisode(t *testing.T, fx *episodeCollectionFixture, sub model.Subscription, resource model.EpisodeResource) (*model.SubscriptionEpisode, model.Download) {
	t.Helper()
	ledger, claimed, err := fx.episodeRepo.ClaimForDownload(sub.ID, 1, resource)
	require.NoError(t, err)
	require.True(t, claimed)
	downloadedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	download := model.Download{
		SubscriptionID: sub.ID,
		Title:          resource.Title,
		Episode:        1,
		TorrentURL:     resource.URL,
		TorrentHash:    resource.Hash,
		Status:         model.DownloadStatusCompleted,
		Purpose:        model.DownloadPurposeNormal,
		DownloadedAt:   &downloadedAt,
	}
	require.NoError(t, fx.db.Create(&download).Error)
	require.NoError(t, fx.episodeRepo.AttachDownload(ledger.ID, download.ID))
	require.NoError(t, fx.episodeRepo.MarkDownloaded(ledger.ID, download.ID, resource, downloadedAt))
	ledger, err = fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	return ledger, download
}

func performEpisodePreview(t *testing.T, h *SubscriptionHandler, sub model.Subscription, item rss.RSSItem) (*httptest.ResponseRecorder, SubscriptionPreviewItem, map[string]any) {
	t.Helper()
	h.rssParser = &mockRSSParser{items: []rss.RSSItem{item}}
	payload, err := json.Marshal(map[string]any{
		"id":             sub.ID,
		"name":           sub.Name,
		"rss_url":        sub.RssURL,
		"total_episodes": sub.TotalEpisodes,
	})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/subscriptions/preview", h.Preview)
	req := httptest.NewRequest(http.MethodPost, "/subscriptions/preview", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var response struct {
		Data struct {
			Summary map[string]any            `json:"summary"`
			Items   []SubscriptionPreviewItem `json:"items"`
		} `json:"data"`
	}
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Len(t, response.Data.Items, 1)
		return w, response.Data.Items[0], response.Data.Summary
	}
	return w, SubscriptionPreviewItem{}, nil
}

func databaseCounts(t *testing.T, db *gorm.DB) (downloads, ledgers, candidates int64) {
	t.Helper()
	require.NoError(t, db.Model(&model.Download{}).Count(&downloads).Error)
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Count(&ledgers).Error)
	require.NoError(t, db.Model(&model.EpisodeResourceCandidate{}).Count(&candidates).Error)
	return downloads, ledgers, candidates
}

func TestSubscriptionPreviewUsesReadOnlyEpisodeDecisions(t *testing.T) {
	oldResource := model.EpisodeResource{Hash: "old-hash", URL: "magnet:?xt=urn:btih:old", Title: "Ledger Show 01 old"}
	tests := []struct {
		name       string
		setup      func(*testing.T, *episodeCollectionFixture, model.Subscription)
		item       rss.RSSItem
		wantAction string
		wantReason string
	}{
		{
			name: "owned episode with different resource requires manual review",
			setup: func(t *testing.T, fx *episodeCollectionFixture, sub model.Subscription) {
				seedDownloadedEpisode(t, fx, sub, oldResource)
			},
			item:       rss.RSSItem{Title: "Ledger Show 01 new", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:new", TorrentHash: "new-hash"},
			wantAction: "manual_review",
			wantReason: "episode_already_owned_different_resource",
		},
		{
			name: "known resource is duplicate",
			setup: func(t *testing.T, fx *episodeCollectionFixture, sub model.Subscription) {
				seedDownloadedEpisode(t, fx, sub, oldResource)
			},
			item:       rss.RSSItem{Title: oldResource.Title, Episode: 1, TorrentURL: oldResource.URL, TorrentHash: oldResource.Hash},
			wantAction: "duplicate",
			wantReason: "resource_already_known",
		},
		{
			name: "ignored episode stays skipped",
			setup: func(t *testing.T, fx *episodeCollectionFixture, sub model.Subscription) {
				_, err := fx.episodeRepo.ObserveEpisode(sub.ID, 1)
				require.NoError(t, err)
				require.NoError(t, fx.episodeRepo.SetStatus(sub.ID, []int{1}, model.EpisodeStatusIgnored, model.EpisodeStatusSourceUser))
			},
			item:       rss.RSSItem{Title: "Ledger Show 01 ignored", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:ignored", TorrentHash: "ignored-hash"},
			wantAction: "skip",
			wantReason: "ignored",
		},
		{
			name:       "missing episode is downloadable without claiming",
			item:       rss.RSSItem{Title: "Ledger Show 01 missing", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:missing", TorrentHash: "missing-hash"},
			wantAction: "download",
			wantReason: "episode_missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newEpisodeCollectionFixture(t)
			sub := fx.createSubscription(t, nil)
			if tt.setup != nil {
				tt.setup(t, fx, sub)
			}
			beforeDownloads, beforeLedgers, beforeCandidates := databaseCounts(t, fx.db)
			w, item, summary := performEpisodePreview(t, fx.handler, sub, tt.item)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, tt.wantAction, item.Action)
			assert.Equal(t, tt.wantReason, item.Reason)
			assert.Zero(t, item.CandidateID)
			assert.Zero(t, item.ExistingDownloadID)
			assert.Equal(t, float64(0), summary["replace_items"])
			if tt.wantAction == "manual_review" {
				assert.Equal(t, float64(1), summary["manual_review_items"])
			}
			afterDownloads, afterLedgers, afterCandidates := databaseCounts(t, fx.db)
			assert.Equal(t, beforeDownloads, afterDownloads)
			assert.Equal(t, beforeLedgers, afterLedgers)
			assert.Equal(t, beforeCandidates, afterCandidates)
		})
	}
}

func runEpisodeCollectionTask(t *testing.T, h *SubscriptionHandler, sub *model.Subscription) (*task.Task, error) {
	t.Helper()
	manager := task.GetManager()
	require.False(t, manager.IsRunning())
	started, err := manager.StartTask(task.TaskTypeCollect, sub.ID, "episode ledger test", func(ctx context.Context, current *task.Task) error {
		return h.doCollectEpisodes(ctx, current, sub)
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return !manager.IsRunning()
	}, 3*time.Second, 10*time.Millisecond)
	if started.Error != "" {
		return started, errors.New(started.Error)
	}
	return started, nil
}

func requireCollectionResult(t *testing.T, completed *task.Task, collected, candidates, deleted int) {
	t.Helper()
	result, ok := completed.Result.(gin.H)
	require.True(t, ok, "unexpected result: %#v", completed.Result)
	assert.Equal(t, collected, result["collected"])
	assert.Equal(t, candidates, result["candidates"])
	assert.Equal(t, deleted, result["deleted"])
}

func TestCollectEpisodesPersistsCandidateWithoutReplacingOwnedDownload(t *testing.T) {
	fx := newEpisodeCollectionFixture(t)
	sub := fx.createSubscription(t, nil)
	oldResource := model.EpisodeResource{Hash: "owned-old-hash", URL: "magnet:?xt=urn:btih:owned-old", Title: "Ledger Show 01 old"}
	oldLedger, oldDownload := seedDownloadedEpisode(t, fx, sub, oldResource)
	pubTime := time.Now().UTC().Truncate(time.Second)
	fx.handler.rssParser = &mockRSSParser{items: []rss.RSSItem{{
		Title: "Ledger Show 01 new", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:owned-new", TorrentHash: "owned-new-hash", PubTime: pubTime,
	}}}

	first, err := runEpisodeCollectionTask(t, fx.handler, &sub)
	require.NoError(t, err)
	requireCollectionResult(t, first, 0, 1, 0)
	second, err := runEpisodeCollectionTask(t, fx.handler, &sub)
	require.NoError(t, err)
	requireCollectionResult(t, second, 0, 0, 0)

	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	assert.Equal(t, oldDownload.ID, downloads[0].ID)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, oldLedger.ID, ledger.ID)
	assert.Equal(t, model.EpisodeStatusDownloaded, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, oldDownload.ID, *ledger.ActiveDownloadID)
	assert.Equal(t, oldResource.Hash, ledger.ActiveTorrentHash)
	candidates, err := fx.episodeRepo.ListCandidates(ledger.ID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, model.CandidateStatusPending, candidates[0].Status)
	assert.Zero(t, fx.qb.addCalls)
}

func TestCollectEpisodesCreatesOnePendingOutboxAndNeverAddsToQBittorrent(t *testing.T) {
	fx := newEpisodeCollectionFixture(t)
	sub := fx.createSubscription(t, nil)
	fx.handler.rssParser = &mockRSSParser{items: []rss.RSSItem{{
		Title: "Ledger Show 02", Episode: 2, TorrentURL: "magnet:?xt=urn:btih:episode-two", TorrentHash: "episode-two-hash", PubTime: time.Now().UTC().Truncate(time.Second),
	}}}

	first, err := runEpisodeCollectionTask(t, fx.handler, &sub)
	require.NoError(t, err)
	requireCollectionResult(t, first, 1, 0, 0)
	second, err := runEpisodeCollectionTask(t, fx.handler, &sub)
	require.NoError(t, err)
	requireCollectionResult(t, second, 0, 0, 0)

	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	assert.Equal(t, model.DownloadStatusPending, downloads[0].Status)
	assert.Equal(t, model.DownloadPurposeNormal, downloads[0].Purpose)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, downloads[0].ID, *ledger.ActiveDownloadID)
	assert.Zero(t, fx.qb.addCalls)
}

func TestCollectEpisodesHandlesEmptyFeedAndObservesBeforeFiltering(t *testing.T) {
	t.Run("empty feed", func(t *testing.T) {
		fx := newEpisodeCollectionFixture(t)
		sub := fx.createSubscription(t, nil)
		fx.handler.rssParser = &mockRSSParser{}

		completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
		require.NoError(t, err)
		requireCollectionResult(t, completed, 0, 0, 0)
	})

	t.Run("filtered item still updates ledger and terminal watermark", func(t *testing.T) {
		fx := newEpisodeCollectionFixture(t)
		sub := fx.createSubscription(t, nil)
		sub.FilterRules = "+1080p"
		pubTime := time.Now().UTC().Truncate(time.Second)
		fx.handler.rssParser = &mockRSSParser{items: []rss.RSSItem{{
			Title: "Ledger Show 04 [720p]", Episode: 4, TorrentURL: "magnet:?xt=urn:btih:filtered", TorrentHash: "filtered-hash", PubTime: pubTime,
		}}}

		completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
		require.NoError(t, err)
		requireCollectionResult(t, completed, 0, 0, 0)
		ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 4)
		require.NoError(t, err)
		assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
		persisted, err := fx.subRepo.GetByID(sub.ID)
		require.NoError(t, err)
		assert.Equal(t, 4, persisted.LatestEpisode)
		require.NotNil(t, persisted.LastRSSPubTime)
		assert.Equal(t, pubTime, *persisted.LastRSSPubTime)
		var downloads int64
		require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
		assert.Zero(t, downloads)
	})
}

type attachFailingEpisodeRepository struct {
	repository.EpisodeRepository
}

func (r *attachFailingEpisodeRepository) AttachDownloadInTx(*gorm.DB, uint, uint) error {
	return errors.New("attach failed")
}

func TestCollectEpisodesReleasesClaimAndKeepsWatermarkOnCreateOrAttachFailure(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *episodeCollectionFixture, model.Subscription)
		wrap  func(repository.EpisodeRepository) repository.EpisodeRepository
		hash  string
	}{
		{
			name: "create failure",
			setup: func(t *testing.T, fx *episodeCollectionFixture, _ model.Subscription) {
				require.NoError(t, fx.db.Create(&model.Download{SubscriptionID: 999, Title: "collision", Episode: 99, TorrentURL: "magnet:?xt=urn:btih:collision", TorrentHash: "collision-hash", Status: model.DownloadStatusPending}).Error)
			},
			hash: "collision-hash",
		},
		{
			name: "attach failure",
			wrap: func(repo repository.EpisodeRepository) repository.EpisodeRepository {
				return &attachFailingEpisodeRepository{EpisodeRepository: repo}
			},
			hash: "attach-failure-hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newEpisodeCollectionFixture(t)
			oldWatermark := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
			sub := fx.createSubscription(t, &oldWatermark)
			if tt.setup != nil {
				tt.setup(t, fx, sub)
			}
			if tt.wrap != nil {
				wrapped := tt.wrap(fx.episodeRepo)
				fx.handler = NewSubscriptionHandler(fx.subRepo, fx.downloadRepo, repository.NewConfigRepository(fx.db), fx.qb, t.TempDir(), wrapped)
			}
			fx.handler.rssParser = &mockRSSParser{items: []rss.RSSItem{{
				Title: "Ledger Show 03", Episode: 3, TorrentURL: "magnet:?xt=urn:btih:" + tt.hash, TorrentHash: tt.hash, PubTime: oldWatermark.Add(time.Hour),
			}}}

			_, err := runEpisodeCollectionTask(t, fx.handler, &sub)
			require.Error(t, err)
			ledger, ledgerErr := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 3)
			require.NoError(t, ledgerErr)
			assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
			assert.Nil(t, ledger.ActiveDownloadID)
			assert.Empty(t, ledger.ActiveTorrentHash)
			var count int64
			require.NoError(t, fx.db.Model(&model.Download{}).Where("subscription_id = ?", sub.ID).Count(&count).Error)
			assert.Zero(t, count)
			persisted, getErr := fx.subRepo.GetByID(sub.ID)
			require.NoError(t, getErr)
			require.NotNil(t, persisted.LastRSSPubTime)
			assert.Equal(t, oldWatermark, *persisted.LastRSSPubTime)
			assert.Zero(t, fx.qb.addCalls)
		})
	}
}

func TestSubscriptionPreviewAndCollectRejectMissingEpisodeService(t *testing.T) {
	parser := &mockRSSParser{items: []rss.RSSItem{{Title: "Ledger Show 01", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:one", TorrentHash: "one"}}}
	sub := model.Subscription{ID: 1, Name: "Ledger Show", RssURL: "https://example.com/feed.xml"}
	repo := &mockSubscriptionRepo{getByIDFunc: func(uint) (*model.Subscription, error) { return &sub, nil }}
	h := NewSubscriptionHandler(repo, &mockDownloadRepo{}, nil, nil, "")
	h.rssParser = parser

	w, _, _ := performEpisodePreview(t, h, sub, parser.items[0])
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Zero(t, parser.fetchCalls)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/subscriptions/:id/collect", h.CollectEpisodes)
	req := httptest.NewRequest(http.MethodPost, "/subscriptions/1/collect", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.False(t, task.GetManager().IsRunning())

	err := h.doCollectEpisodes(context.Background(), nil, &sub)
	require.ErrorContains(t, err, "episode service")
}
