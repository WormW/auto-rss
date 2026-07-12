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
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/scheduler"
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
	feedRepo     repository.SubscriptionFeedRepository
	configRepo   repository.ConfigRepository
	qb           *collectEpisodesSmallTorrentQBClient
}

type fixtureSubscriptionCollector struct {
	fixture *episodeCollectionFixture
}

func (c *fixtureSubscriptionCollector) CollectSubscription(ctx context.Context, subscriptionID uint) (scheduler.CollectSummary, error) {
	fx := c.fixture
	collector := scheduler.NewScheduler(
		fx.db,
		fx.subRepo,
		fx.feedRepo,
		fx.downloadRepo,
		fx.configRepo,
		"30m",
		fx.handler.rssParser,
		fx.qb,
		episode.NewService(fx.episodeRepo),
	)
	return collector.CollectSubscription(ctx, subscriptionID)
}

func newEpisodeCollectionFixture(t *testing.T) *episodeCollectionFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.SubscriptionFeed{},
		&model.SubscriptionFeedSeenItem{},
		&model.Download{},
		&model.Config{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	feedRepo := repository.NewSubscriptionFeedRepository(db)
	configRepo := repository.NewConfigRepository(db)
	qb := &collectEpisodesSmallTorrentQBClient{}
	h := NewSubscriptionHandler(subRepo, downloadRepo, configRepo, qb, t.TempDir(), episodeRepo)
	fx := &episodeCollectionFixture{
		db:           db,
		handler:      h,
		subRepo:      subRepo,
		downloadRepo: downloadRepo,
		episodeRepo:  episodeRepo,
		feedRepo:     feedRepo,
		configRepo:   configRepo,
		qb:           qb,
	}
	h.collector = &fixtureSubscriptionCollector{fixture: fx}
	h.feedRepo = feedRepo
	return fx
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
	feed := model.SubscriptionFeed{
		SubscriptionID:   sub.ID,
		Name:             "Default",
		RSSURL:           sub.RssURL,
		RSSURLNormalized: sub.RssURL,
		Enabled:          true,
		LastRSSPubTime:   watermark,
	}
	require.NoError(t, fx.feedRepo.Create(&feed))
	return sub
}

func seedDownloadedEpisode(t *testing.T, fx *episodeCollectionFixture, sub model.Subscription, resource model.EpisodeResource) (*model.SubscriptionEpisode, model.Download) {
	return seedDownloadedEpisodeNumber(t, fx, sub, 1, resource)
}

func seedDownloadedEpisodeNumber(t *testing.T, fx *episodeCollectionFixture, sub model.Subscription, episodeNumber int, resource model.EpisodeResource) (*model.SubscriptionEpisode, model.Download) {
	t.Helper()
	ledger, claimed, err := fx.episodeRepo.ClaimForDownload(sub.ID, episodeNumber, resource)
	require.NoError(t, err)
	require.True(t, claimed)
	downloadedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	download := model.Download{
		SubscriptionID: sub.ID,
		Title:          resource.Title,
		Episode:        episodeNumber,
		TorrentURL:     resource.URL,
		TorrentHash:    resource.Hash,
		Status:         model.DownloadStatusCompleted,
		Purpose:        model.DownloadPurposeNormal,
		DownloadedAt:   &downloadedAt,
	}
	require.NoError(t, fx.db.Create(&download).Error)
	require.NoError(t, fx.episodeRepo.AttachDownload(ledger.ID, download.ID))
	require.NoError(t, fx.episodeRepo.MarkDownloaded(ledger.ID, download.ID, resource, downloadedAt))
	ledger, err = fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, episodeNumber)
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

func performEpisodePreviewItems(t *testing.T, h *SubscriptionHandler, sub model.Subscription, items []rss.RSSItem) (*httptest.ResponseRecorder, []SubscriptionPreviewItem, map[string]any) {
	t.Helper()
	h.rssParser = &mockRSSParser{items: items}
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
	}
	return w, response.Data.Items, response.Data.Summary
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

func TestEpisodeDecisionClosesItem(t *testing.T) {
	tests := []struct {
		action string
		reason string
		want   bool
	}{
		{action: "download", want: true},
		{action: "candidate", want: true},
		{action: "ignored", want: true},
		{action: "skip", reason: "resource_already_known", want: true},
		{action: "skip", reason: "unsupported_episode_status", want: true},
		{action: "skip", reason: "resource_identity_missing", want: false},
		{action: "skip", reason: "non_positive_relative_episode", want: false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, episodeDecisionClosesItem(tt.action, tt.reason), "%s/%s", tt.action, tt.reason)
	}
}

func TestPreviewAndCollectUseSameEpisodeClosingRules(t *testing.T) {
	t.Run("episode zero is skipped consistently", func(t *testing.T) {
		fx := newEpisodeCollectionFixture(t)
		sub := fx.createSubscription(t, nil)
		items := []rss.RSSItem{{
			Title: "Ledger Show episode zero", Episode: 0, TorrentURL: "magnet:?xt=urn:btih:episode-zero", TorrentHash: "episode-zero",
		}}

		w, preview, summary := performEpisodePreviewItems(t, fx.handler, sub, items)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, preview, 1)
		assert.Equal(t, "skip", preview[0].Action)
		assert.Equal(t, "non_positive_relative_episode", preview[0].Reason)
		assert.Equal(t, float64(0), summary["download_items"])

		fx.handler.rssParser = &mockRSSParser{items: items}
		completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
		require.NoError(t, err)
		requireCollectionResult(t, completed, 0, 0, 0)
		var downloads, ledgers int64
		require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
		require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).Count(&ledgers).Error)
		assert.Zero(t, downloads)
		assert.Zero(t, ledgers)
	})

	t.Run("hash only resource stays open for executable URL", func(t *testing.T) {
		fx := newEpisodeCollectionFixture(t)
		sub := fx.createSubscription(t, nil)
		items := []rss.RSSItem{
			{Title: "Ledger Show 01 hash only", Episode: 1, TorrentHash: "hash-only"},
			{Title: "Ledger Show 01 valid", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:executable", TorrentHash: "executable"},
		}

		w, preview, summary := performEpisodePreviewItems(t, fx.handler, sub, items)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, preview, 2)
		assert.Equal(t, "skip", preview[0].Action)
		assert.Equal(t, "torrent_url_missing", preview[0].Reason)
		assert.Equal(t, "download", preview[1].Action)
		assert.Equal(t, float64(1), summary["download_items"])

		fx.handler.rssParser = &mockRSSParser{items: items}
		completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
		require.NoError(t, err)
		requireCollectionResult(t, completed, 1, 0, 0)
		var downloads []model.Download
		require.NoError(t, fx.db.Find(&downloads).Error)
		require.Len(t, downloads, 1)
		assert.Equal(t, "executable", downloads[0].TorrentHash)
		assert.NotEmpty(t, downloads[0].TorrentURL)
	})

	t.Run("known resource closes before different resource", func(t *testing.T) {
		fx := newEpisodeCollectionFixture(t)
		sub := fx.createSubscription(t, nil)
		known := model.EpisodeResource{Hash: "known-a", URL: "magnet:?xt=urn:btih:known-a", Title: "Ledger Show 01 A"}
		seedDownloadedEpisode(t, fx, sub, known)
		items := []rss.RSSItem{
			{Title: known.Title, Episode: 1, TorrentURL: known.URL, TorrentHash: known.Hash},
			{Title: "Ledger Show 01 B", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:known-b", TorrentHash: "known-b"},
		}

		w, preview, summary := performEpisodePreviewItems(t, fx.handler, sub, items)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, preview, 2)
		assert.Equal(t, "duplicate", preview[0].Action)
		assert.Equal(t, "skip", preview[1].Action)
		assert.Equal(t, "同一集已有更靠前的 RSS 条目", preview[1].Reason)
		assert.Equal(t, float64(0), summary["manual_review_items"])

		fx.handler.rssParser = &mockRSSParser{items: items}
		completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
		require.NoError(t, err)
		requireCollectionResult(t, completed, 0, 1, 0)
		var candidates int64
		require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidates).Error)
		assert.EqualValues(t, 1, candidates)
	})

	t.Run("ignored closes all later resources", func(t *testing.T) {
		fx := newEpisodeCollectionFixture(t)
		sub := fx.createSubscription(t, nil)
		_, err := fx.episodeRepo.ObserveEpisode(sub.ID, 1)
		require.NoError(t, err)
		require.NoError(t, fx.episodeRepo.SetStatus(sub.ID, []int{1}, model.EpisodeStatusIgnored, model.EpisodeStatusSourceUser))
		items := []rss.RSSItem{
			{Title: "Ledger Show 01 A", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:ignored-a", TorrentHash: "ignored-a"},
			{Title: "Ledger Show 01 B", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:ignored-b", TorrentHash: "ignored-b"},
		}

		w, preview, summary := performEpisodePreviewItems(t, fx.handler, sub, items)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, preview, 2)
		assert.Equal(t, "skip", preview[0].Action)
		assert.Equal(t, "ignored", preview[0].Reason)
		assert.Equal(t, "同一集已有更靠前的 RSS 条目", preview[1].Reason)
		assert.Equal(t, float64(0), summary["manual_review_items"])

		fx.handler.rssParser = &mockRSSParser{items: items}
		completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
		require.NoError(t, err)
		requireCollectionResult(t, completed, 0, 0, 0)
	})

	t.Run("missing identity stays open for valid resource", func(t *testing.T) {
		fx := newEpisodeCollectionFixture(t)
		sub := fx.createSubscription(t, nil)
		items := []rss.RSSItem{
			{Title: "Ledger Show 01 no identity", Episode: 1},
			{Title: "Ledger Show 01 valid", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:valid", TorrentHash: "valid-hash"},
		}

		w, preview, summary := performEpisodePreviewItems(t, fx.handler, sub, items)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, preview, 2)
		assert.Equal(t, "skip", preview[0].Action)
		assert.Equal(t, "resource_identity_missing", preview[0].Reason)
		assert.Equal(t, "download", preview[1].Action)
		assert.Equal(t, float64(1), summary["download_items"])

		fx.handler.rssParser = &mockRSSParser{items: items}
		completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
		require.NoError(t, err)
		requireCollectionResult(t, completed, 1, 0, 0)
		var downloads []model.Download
		require.NoError(t, fx.db.Find(&downloads).Error)
		require.Len(t, downloads, 1)
		assert.Equal(t, "valid-hash", downloads[0].TorrentHash)
	})
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
	result, ok := completed.Result.(scheduler.CollectSummary)
	require.True(t, ok, "unexpected result: %#v", completed.Result)
	assert.Equal(t, collected, result.DownloadsCreated)
	assert.Equal(t, candidates, result.CandidatesCreated)
	assert.Zero(t, deleted)
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

func TestCollectEpisodesRecoversFailedDownloadWithSameTorrentHash(t *testing.T) {
	fx := newEpisodeCollectionFixture(t)
	sub := fx.createSubscription(t, nil)
	resource := model.EpisodeResource{
		Hash:  "recover-failed-hash",
		URL:   "magnet:?xt=urn:btih:recover-failed-hash",
		Title: "Ledger Show 03",
	}
	_, err := fx.episodeRepo.ObserveEpisode(sub.ID, 3)
	require.NoError(t, err)
	failed := model.Download{
		SubscriptionID: sub.ID,
		Title:          resource.Title,
		Episode:        3,
		TorrentURL:     resource.URL,
		TorrentHash:    resource.Hash,
		Status:         model.DownloadStatusFailed,
		RetryCount:     5,
		MaxRetries:     5,
		LastError:      "previous qBittorrent failure",
		ErrorMessage:   "previous qBittorrent failure",
	}
	require.NoError(t, fx.db.Create(&failed).Error)
	fx.handler.rssParser = &mockRSSParser{items: []rss.RSSItem{{
		Title: resource.Title, Episode: 3, TorrentURL: resource.URL, TorrentHash: resource.Hash,
		PubTime: time.Now().UTC().Truncate(time.Second),
	}}}

	completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
	require.NoError(t, err)
	result, ok := completed.Result.(scheduler.CollectSummary)
	require.True(t, ok)
	assert.Zero(t, result.DownloadsCreated)
	assert.Equal(t, 1, result.DownloadsRecovered)
	assert.Zero(t, result.FeedErrors)

	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	assert.Equal(t, failed.ID, downloads[0].ID)
	assert.Equal(t, model.DownloadStatusRetryCleanup, downloads[0].Status)
	assert.Zero(t, downloads[0].RetryCount)
	assert.Empty(t, downloads[0].LastError)
	assert.Empty(t, downloads[0].ErrorMessage)

	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 3)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, failed.ID, *ledger.ActiveDownloadID)
	assert.Equal(t, resource.Hash, ledger.ActiveTorrentHash)
	feeds, err := fx.feedRepo.ListBySubscription(sub.ID)
	require.NoError(t, err)
	require.Len(t, feeds, 1)
	assert.Empty(t, feeds[0].LastError)
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
		require.NoError(t, fx.subRepo.Update(&sub))
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
		feeds, err := fx.feedRepo.ListBySubscription(sub.ID)
		require.NoError(t, err)
		require.Len(t, feeds, 1)
		require.NotNil(t, feeds[0].LastRSSPubTime)
		assert.Equal(t, pubTime, *feeds[0].LastRSSPubTime)
		var downloads int64
		require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
		assert.Zero(t, downloads)
	})
}

func TestCollectEpisodesRollsBackClaimAndKeepsWatermarkOnCreateOrAttachFailure(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *episodeCollectionFixture, model.Subscription)
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
			setup: func(t *testing.T, fx *episodeCollectionFixture, _ model.Subscription) {
				require.NoError(t, fx.db.Exec(`
					CREATE TRIGGER fail_download_attach
					BEFORE UPDATE OF active_download_id ON subscription_episodes
					WHEN NEW.active_download_id IS NOT NULL
					BEGIN
						SELECT RAISE(FAIL, 'attach failed');
					END;
				`).Error)
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
			fx.handler.rssParser = &mockRSSParser{items: []rss.RSSItem{{
				Title: "Ledger Show 03", Episode: 3, TorrentURL: "magnet:?xt=urn:btih:" + tt.hash, TorrentHash: tt.hash, PubTime: oldWatermark.Add(time.Hour),
			}}}

			completed, err := runEpisodeCollectionTask(t, fx.handler, &sub)
			require.ErrorContains(t, err, "all subscription feeds failed")
			if tt.name == "create failure" {
				require.ErrorContains(t, err, "Default: torrent hash collision-hash already belongs to download")
			} else {
				require.ErrorContains(t, err, "Default: failed to attach download to episode")
			}
			result, ok := completed.Result.(scheduler.CollectSummary)
			require.True(t, ok)
			assert.Equal(t, 1, result.FeedErrors)
			ledger, ledgerErr := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 3)
			require.NoError(t, ledgerErr)
			assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
			var count int64
			require.NoError(t, fx.db.Model(&model.Download{}).Where("subscription_id = ?", sub.ID).Count(&count).Error)
			assert.Zero(t, count)
			feeds, getErr := fx.feedRepo.ListBySubscription(sub.ID)
			require.NoError(t, getErr)
			require.Len(t, feeds, 1)
			require.NotNil(t, feeds[0].LastRSSPubTime)
			assert.Equal(t, oldWatermark, *feeds[0].LastRSSPubTime)
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
	require.ErrorContains(t, err, "subscription feed collector")
}
