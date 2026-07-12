package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type subscriptionBaselineFixture struct {
	db      *gorm.DB
	handler *SubscriptionHandler
}

func newSubscriptionBaselineFixture(t *testing.T) *subscriptionBaselineFixture {
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
	episodeRepo := repository.NewEpisodeRepository(db)
	h := NewSubscriptionHandler(
		subRepo,
		repository.NewDownloadRepository(db),
		repository.NewConfigRepository(db),
		nil,
		t.TempDir(),
		episodeRepo,
	)
	h.bangumiEnricher = nil
	return &subscriptionBaselineFixture{db: db, handler: h}
}

func performSubscriptionBaselineRequest(t *testing.T, h *SubscriptionHandler, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/subscriptions", h.Create)
	r.PUT("/subscriptions/:id", h.Update)
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSubscriptionHandlerCreateInitializesRSSBaselineAndEpisodeRange(t *testing.T) {
	fx := newSubscriptionBaselineFixture(t)
	w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPost, "/subscriptions", map[string]any{
		"name":           "New Show",
		"rss_url":        "  https://new.example/feed  ",
		"total_episodes": 3,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var sub model.Subscription
	require.NoError(t, fx.db.First(&sub).Error)
	assert.Equal(t, "https://new.example/feed", sub.RssURL)
	assert.True(t, sub.RSSBaselinePending)
	var episodes []model.SubscriptionEpisode
	require.NoError(t, fx.db.Where("subscription_id = ?", sub.ID).Order("episode").Find(&episodes).Error)
	require.Len(t, episodes, 3)
	assert.Equal(t, []int{1, 2, 3}, []int{episodes[0].Episode, episodes[1].Episode, episodes[2].Episode})
}

func TestSubscriptionHandlerCreateCalendarDoesNotSetRSSBaseline(t *testing.T) {
	fx := newSubscriptionBaselineFixture(t)
	w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPost, "/subscriptions", map[string]any{
		"name":           "Calendar Show",
		"rss_url":        "   ",
		"total_episodes": 2,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var sub model.Subscription
	require.NoError(t, fx.db.First(&sub).Error)
	assert.True(t, sub.IsCalendarOnly())
	assert.False(t, sub.RSSBaselinePending)
}

func TestSubscriptionHandlerCreateRejectsInvalidEpisodeTotals(t *testing.T) {
	for _, total := range []int{-1, 10001} {
		t.Run(fmt.Sprintf("total_%d", total), func(t *testing.T) {
			fx := newSubscriptionBaselineFixture(t)
			w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPost, "/subscriptions", map[string]any{
				"name": "Invalid Total", "rss_url": "https://invalid.example/feed", "total_episodes": total,
			})
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var count int64
			require.NoError(t, fx.db.Model(&model.Subscription{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestSubscriptionHandlerUpdateRSSURLControlsBaselineAndTrimsValue(t *testing.T) {
	tests := []struct {
		name        string
		oldURL      string
		oldPending  bool
		updates     map[string]any
		wantURL     string
		wantPending bool
	}{
		{
			name:        "changed source starts baseline",
			oldURL:      "https://old.example/feed",
			updates:     map[string]any{"rss_url": " https://new.example/feed "},
			wantURL:     "https://new.example/feed",
			wantPending: true,
		},
		{
			name:        "same normalized source and rename preserve false",
			oldURL:      "https://same.example/feed",
			updates:     map[string]any{"rss_url": "  https://same.example/feed  ", "name": "Renamed Show"},
			wantURL:     "https://same.example/feed",
			wantPending: false,
		},
		{
			name:        "removing rss becomes calendar without baseline",
			oldURL:      "https://old.example/feed",
			oldPending:  true,
			updates:     map[string]any{"rss_url": "  "},
			wantURL:     "",
			wantPending: false,
		},
		{
			name:        "non source fields preserve pending state",
			oldURL:      "https://same.example/feed",
			oldPending:  true,
			updates:     map[string]any{"fansub": "New Group", "enabled": false},
			wantURL:     "https://same.example/feed",
			wantPending: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newSubscriptionBaselineFixture(t)
			watermark := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
			sub := model.Subscription{
				Name:               "Old Show",
				RssURL:             tt.oldURL,
				SourceType:         "manual",
				Status:             "active",
				Enabled:            true,
				RSSBaselinePending: tt.oldPending,
				LastRSSPubTime:     &watermark,
			}
			require.NoError(t, fx.db.Create(&sub).Error)
			w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPut, fmt.Sprintf("/subscriptions/%d", sub.ID), tt.updates)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			var got model.Subscription
			require.NoError(t, fx.db.First(&got, sub.ID).Error)
			assert.Equal(t, tt.wantURL, got.RssURL)
			assert.Equal(t, tt.wantPending, got.RSSBaselinePending)
			require.NotNil(t, got.LastRSSPubTime)
			assert.Equal(t, watermark, *got.LastRSSPubTime)
			if tt.wantURL == "" {
				assert.True(t, got.IsCalendarOnly())
			}
		})
	}
}

func TestSubscriptionHandlerUpdateExpandsButDoesNotShrinkEpisodeRange(t *testing.T) {
	fx := newSubscriptionBaselineFixture(t)
	sub := model.Subscription{Name: "Range Show", RssURL: "https://range.example/feed", TotalEpisodes: 3}
	require.NoError(t, fx.db.Create(&sub).Error)
	require.NoError(t, repository.NewEpisodeRepository(fx.db).EnsureRange(sub.ID, 3))

	w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPut, fmt.Sprintf("/subscriptions/%d", sub.ID), map[string]any{"total_episodes": 5})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	episodeOperations := 0
	callbackName := "test:no_episode_range_scan_on_shrink"
	countEpisodeOperation := func(tx *gorm.DB) {
		if tx.Statement.Table == (model.SubscriptionEpisode{}).TableName() {
			episodeOperations++
		}
	}
	require.NoError(t, fx.db.Callback().Query().Before("gorm:query").Register(callbackName, countEpisodeOperation))
	require.NoError(t, fx.db.Callback().Create().Before("gorm:create").Register(callbackName, countEpisodeOperation))
	t.Cleanup(func() {
		_ = fx.db.Callback().Query().Remove(callbackName)
		_ = fx.db.Callback().Create().Remove(callbackName)
	})
	w = performSubscriptionBaselineRequest(t, fx.handler, http.MethodPut, fmt.Sprintf("/subscriptions/%d", sub.ID), map[string]any{"total_episodes": 2})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Zero(t, episodeOperations)

	var episodes []model.SubscriptionEpisode
	require.NoError(t, fx.db.Where("subscription_id = ?", sub.ID).Order("episode").Find(&episodes).Error)
	require.Len(t, episodes, 5)
	assert.Equal(t, 4, episodes[3].Episode)
	assert.Equal(t, 5, episodes[4].Episode)
}

func TestSubscriptionHandlerUpdateRejectsInvalidEpisodeTotals(t *testing.T) {
	tests := []struct {
		name  string
		total any
	}{
		{name: "fractional", total: 3.5},
		{name: "negative", total: -1},
		{name: "over limit", total: 10001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newSubscriptionBaselineFixture(t)
			sub := model.Subscription{Name: "Original", RssURL: "https://range.example/feed", TotalEpisodes: 3}
			require.NoError(t, fx.db.Create(&sub).Error)
			require.NoError(t, repository.NewEpisodeRepository(fx.db).EnsureRange(sub.ID, 3))

			w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPut, fmt.Sprintf("/subscriptions/%d", sub.ID), map[string]any{
				"name": "Must Not Persist", "total_episodes": tt.total,
			})
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var got model.Subscription
			require.NoError(t, fx.db.First(&got, sub.ID).Error)
			assert.Equal(t, "Original", got.Name)
			assert.Equal(t, 3, got.TotalEpisodes)
			var episodeCount int64
			require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).Where("subscription_id = ?", sub.ID).Count(&episodeCount).Error)
			assert.EqualValues(t, 3, episodeCount)
		})
	}
}

func TestSubscriptionHandlerCreateRollsBackSubscriptionWhenEpisodeRangeFails(t *testing.T) {
	fx := newSubscriptionBaselineFixture(t)
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER block_create_episode_range
		BEFORE INSERT ON subscription_episodes
		WHEN NEW.episode = 2
		BEGIN SELECT RAISE(ABORT, 'episode range blocked'); END;
	`).Error)

	w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPost, "/subscriptions", map[string]any{
		"name": "Atomic Create", "rss_url": "https://atomic.example/feed", "total_episodes": 3,
	})
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

	var subscriptions, episodes int64
	require.NoError(t, fx.db.Model(&model.Subscription{}).Count(&subscriptions).Error)
	require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).Count(&episodes).Error)
	assert.Zero(t, subscriptions)
	assert.Zero(t, episodes)
}

func TestSubscriptionHandlerUpdateRollsBackAllFieldsWhenEpisodeExpansionFails(t *testing.T) {
	fx := newSubscriptionBaselineFixture(t)
	sub := model.Subscription{
		Name: "Atomic Old", RssURL: "https://old.example/feed", Fansub: "Old Group", TotalEpisodes: 3,
	}
	require.NoError(t, fx.db.Create(&sub).Error)
	require.NoError(t, repository.NewEpisodeRepository(fx.db).EnsureRange(sub.ID, 3))
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER block_update_episode_range
		BEFORE INSERT ON subscription_episodes
		WHEN NEW.episode = 5
		BEGIN SELECT RAISE(ABORT, 'episode expansion blocked'); END;
	`).Error)

	w := performSubscriptionBaselineRequest(t, fx.handler, http.MethodPut, fmt.Sprintf("/subscriptions/%d", sub.ID), map[string]any{
		"name": "Atomic New", "rss_url": "https://new.example/feed", "fansub": "New Group", "total_episodes": 5,
	})
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

	var got model.Subscription
	require.NoError(t, fx.db.First(&got, sub.ID).Error)
	assert.Equal(t, "Atomic Old", got.Name)
	assert.Equal(t, "https://old.example/feed", got.RssURL)
	assert.Equal(t, "Old Group", got.Fansub)
	assert.Equal(t, 3, got.TotalEpisodes)
	assert.False(t, got.RSSBaselinePending)
	var episodes []model.SubscriptionEpisode
	require.NoError(t, fx.db.Where("subscription_id = ?", sub.ID).Order("episode").Find(&episodes).Error)
	require.Len(t, episodes, 3)
}
