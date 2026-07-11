package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// mockSubscriptionRepo is a mock implementation of SubscriptionRepository for testing
type mockSubscriptionRepo struct {
	createFunc            func(subscription *model.Subscription) error
	updateFunc            func(subscription *model.Subscription) error
	deleteFunc            func(id uint) error
	getByIDFunc           func(id uint) (*model.Subscription, error)
	getByRSSURLFunc       func(rssURL string) (*model.Subscription, error)
	getByRSSURLSeasonFunc func(rssURL string, season int) (*model.Subscription, error)
	listFunc              func(offset, limit int) ([]model.Subscription, int64, error)
	getActiveFunc         func() ([]model.Subscription, error)
	updateInTxFunc        func(tx *gorm.DB, subscription *model.Subscription) error
	getWithCountFunc      func() ([]repository.SubscriptionWithStats, error)

	listCalls    int
	getByIDCalls int
	createCalls  int
}

type mockRSSParser struct {
	items      []rss.RSSItem
	err        error
	fetchCalls int
}

type smartFetchConfigRepo struct {
	values map[string]string
}

func (m *mockRSSParser) FetchAndParse(rssURL string) ([]rss.RSSItem, error) {
	m.fetchCalls++
	return m.items, m.err
}

func (m *mockRSSParser) FetchAndParseWithTimeout(rssURL string, timeout time.Duration) ([]rss.RSSItem, error) {
	m.fetchCalls++
	return m.items, m.err
}

func (m *mockRSSParser) Parse(feed interface{}) ([]rss.RSSItem, error) {
	return m.items, m.err
}

func (m *mockRSSParser) ExtractFansub(title string) string {
	return ""
}

func (m *mockRSSParser) ExtractEpisode(title string) int {
	return 0
}

func (m *mockRSSParser) SetProxy(proxyURL string) error {
	return nil
}

func (r *smartFetchConfigRepo) Get(key string) (*model.Config, error) {
	if value, ok := r.values[key]; ok {
		return &model.Config{Key: key, Value: value}, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *smartFetchConfigRepo) GetCached(key string) (string, error) {
	cfg, err := r.Get(key)
	if err != nil {
		return "", err
	}
	return cfg.Value, nil
}

func (r *smartFetchConfigRepo) Set(key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *smartFetchConfigRepo) Delete(key string) error {
	delete(r.values, key)
	return nil
}

func (r *smartFetchConfigRepo) GetAll() ([]model.Config, error) {
	configs := make([]model.Config, 0, len(r.values))
	for key, value := range r.values {
		configs = append(configs, model.Config{Key: key, Value: value})
	}
	return configs, nil
}

func (m *mockSubscriptionRepo) Create(subscription *model.Subscription) error {
	m.createCalls++
	if m.createFunc != nil {
		return m.createFunc(subscription)
	}
	return nil
}

func (m *mockSubscriptionRepo) CreateInTx(_ *gorm.DB, subscription *model.Subscription) error {
	return m.Create(subscription)
}

func (m *mockSubscriptionRepo) Update(subscription *model.Subscription) error {
	if m.updateFunc != nil {
		return m.updateFunc(subscription)
	}
	return nil
}

func (m *mockSubscriptionRepo) UpdateRSSWatermark(_ uint, _ string, _ *time.Time) error {
	return nil
}

func (m *mockSubscriptionRepo) Delete(id uint) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(id)
	}
	return nil
}

func (m *mockSubscriptionRepo) GetByID(id uint) (*model.Subscription, error) {
	m.getByIDCalls++
	if m.getByIDFunc != nil {
		return m.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}

func (m *mockSubscriptionRepo) GetByRSSURL(rssURL string) (*model.Subscription, error) {
	if m.getByRSSURLFunc != nil {
		return m.getByRSSURLFunc(rssURL)
	}
	return nil, errors.New("not found")
}

func (m *mockSubscriptionRepo) GetByRSSURLAndSeason(rssURL string, season int) (*model.Subscription, error) {
	if m.getByRSSURLSeasonFunc != nil {
		return m.getByRSSURLSeasonFunc(rssURL, season)
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *mockSubscriptionRepo) List(offset, limit int) ([]model.Subscription, int64, error) {
	m.listCalls++
	if m.listFunc != nil {
		return m.listFunc(offset, limit)
	}
	return []model.Subscription{}, 0, nil
}

func (m *mockSubscriptionRepo) GetActiveSubscriptions() ([]model.Subscription, error) {
	if m.getActiveFunc != nil {
		return m.getActiveFunc()
	}
	return nil, nil
}

func (m *mockSubscriptionRepo) UpdateInTx(tx *gorm.DB, subscription *model.Subscription) error {
	if m.updateInTxFunc != nil {
		return m.updateInTxFunc(tx, subscription)
	}
	return nil
}

func (m *mockSubscriptionRepo) GetSubscriptionsWithDownloadCount() ([]repository.SubscriptionWithStats, error) {
	if m.getWithCountFunc != nil {
		return m.getWithCountFunc()
	}
	return nil, errors.New("not implemented")
}

func TestSubscriptionHandler_List(t *testing.T) {
	tests := []struct {
		name       string
		mockGet    func() ([]repository.SubscriptionWithStats, error)
		wantStatus int
		wantCount  int
	}{
		{
			name: "success with subscriptions",
			mockGet: func() ([]repository.SubscriptionWithStats, error) {
				return []repository.SubscriptionWithStats{
					{Subscription: model.Subscription{ID: 1, Name: "Anime 1"}, DownloadingCount: 2},
					{Subscription: model.Subscription{ID: 2, Name: "Anime 2"}, DownloadingCount: 0},
				}, nil
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name: "empty list",
			mockGet: func() ([]repository.SubscriptionWithStats, error) {
				return []repository.SubscriptionWithStats{}, nil
			},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name: "repository error",
			mockGet: func() ([]repository.SubscriptionWithStats, error) {
				return nil, errors.New("database error")
			},
			wantStatus: http.StatusInternalServerError,
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockSubscriptionRepo{getWithCountFunc: tt.mockGet}
			handler := NewSubscriptionHandler(mockRepo, nil, nil, nil, "")

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.GET("/subscriptions", handler.List)

			req := httptest.NewRequest("GET", "/subscriptions", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK && tt.wantCount > 0 {
				var resp struct {
					Code int `json:"code"`
					Data struct {
						List []repository.SubscriptionWithStats `json:"list"`
					} `json:"data"`
				}
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, 0, resp.Code)
				assert.Len(t, resp.Data.List, tt.wantCount)
			}
		})
	}
}

func TestSubscriptionHandler_GetByID(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		mockGet    func(id uint) (*model.Subscription, error)
		wantStatus int
	}{
		{
			name: "success",
			id:   "1",
			mockGet: func(id uint) (*model.Subscription, error) {
				assert.Equal(t, uint(1), id)
				return &model.Subscription{
					ID:   1,
					Name: "Test Subscription",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			mockGet:    nil,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "not found",
			id:   "999",
			mockGet: func(id uint) (*model.Subscription, error) {
				return nil, errors.New("not found")
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockSubscriptionRepo{getByIDFunc: tt.mockGet}
			handler := NewSubscriptionHandler(mockRepo, nil, nil, nil, "")

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.GET("/subscriptions/:id", handler.GetByID)

			req := httptest.NewRequest("GET", "/subscriptions/"+tt.id, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestSubscriptionHandler_ListSmartFetchStatus(t *testing.T) {
	smartDisabled := false
	mockRepo := &mockSubscriptionRepo{
		getWithCountFunc: func() ([]repository.SubscriptionWithStats, error) {
			return []repository.SubscriptionWithStats{
				{
					Subscription: model.Subscription{
						ID:                 1,
						Name:               "Smart Disabled",
						TotalEpisodes:      12,
						CurrentEpisode:     12,
						AirDay:             "1",
						SmartFetchEnabled:  &smartDisabled,
						SmartFetchOverride: "never",
					},
				},
			}, nil
		},
	}
	handler := NewSubscriptionHandler(
		mockRepo,
		&mockDownloadRepo{},
		&smartFetchConfigRepo{values: map[string]string{"smart_fetch.enabled": "true"}},
		nil,
		"",
	)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/subscriptions/smart-fetch/status", handler.ListSmartFetchStatus)

	req := httptest.NewRequest(http.MethodGet, "/subscriptions/smart-fetch/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []SubscriptionSmartFetchStatus `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data.List, 1)
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, uint(1), resp.Data.List[0].SubscriptionID)
	assert.False(t, resp.Data.List[0].SmartFetchEnabled)
	assert.True(t, resp.Data.List[0].ShouldFetch)
	assert.Equal(t, "smart_fetch_disabled", resp.Data.List[0].Reason)
	assert.NotEmpty(t, resp.Data.List[0].Explanation)
}

func TestSubscriptionHandler_PreviewAppliesRules(t *testing.T) {
	fx := newEpisodeCollectionFixture(t)
	handler := fx.handler
	handler.downloadPath = "/downloads"
	handler.rssParser = &mockRSSParser{items: []rss.RSSItem{
		{
			Title:       "[ANi] Test Anime - 01 [1080p][CHS]",
			Episode:     1,
			Fansub:      "ANi",
			Language:    rss.LangCHS,
			TorrentURL:  "magnet:?xt=urn:btih:1111111111111111111111111111111111111111",
			TorrentHash: "1111111111111111111111111111111111111111",
			PubDate:     "Sun, 24 May 2026 10:00:00 +0800",
		},
		{
			Title:       "[ANi] Test Anime - 02 [720p][CHS]",
			Episode:     2,
			Fansub:      "ANi",
			Language:    rss.LangCHS,
			TorrentURL:  "magnet:?xt=urn:btih:2222222222222222222222222222222222222222",
			TorrentHash: "2222222222222222222222222222222222222222",
			PubDate:     "Sun, 24 May 2026 10:05:00 +0800",
		},
	}}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/subscriptions/preview", handler.Preview)

	body := map[string]interface{}{
		"name":         "Test Anime",
		"rss_url":      "http://example.com/feed.xml",
		"season":       1,
		"filter_rules": "+1080p\n-720p",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/subscriptions/preview", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Summary struct {
				TotalItems    int `json:"total_items"`
				DownloadItems int `json:"download_items"`
				SkippedItems  int `json:"skipped_items"`
			} `json:"summary"`
			Items []SubscriptionPreviewItem `json:"items"`
		} `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, 2, resp.Data.Summary.TotalItems)
	assert.Equal(t, 1, resp.Data.Summary.DownloadItems)
	assert.Equal(t, 1, resp.Data.Summary.SkippedItems)
	require.Len(t, resp.Data.Items, 2)
	assert.Equal(t, "download", resp.Data.Items[0].Action)
	assert.Equal(t, "skip", resp.Data.Items[1].Action)
	assert.NotEmpty(t, resp.Data.Items[0].RenamePreview)
}

func TestSubscriptionHandler_CollectEpisodesSmallTorrentGuard(t *testing.T) {
	const defaultThreshold = int64(50 * 1024 * 1024)

	tests := []struct {
		name           string
		sizeBytes      int64
		configValue    string
		existingOwned  bool
		wantDownloads  int64
		wantCandidates int64
	}{
		{
			name:          "unknown size continues",
			sizeBytes:     0,
			wantDownloads: 1,
		},
		{
			name:          "under threshold skips with default",
			sizeBytes:     defaultThreshold - 1,
			wantDownloads: 0,
		},
		{
			name:          "over threshold continues",
			sizeBytes:     defaultThreshold,
			wantDownloads: 1,
		},
		{
			name:          "zero config bypasses guard",
			sizeBytes:     defaultThreshold - 1,
			configValue:   "0",
			wantDownloads: 1,
		},
		{
			name:          "under threshold different resource keeps owned download",
			sizeBytes:     defaultThreshold - 1,
			existingOwned: true,
			wantDownloads: 1,
		},
		{
			name:           "over threshold different resource creates candidate",
			sizeBytes:      defaultThreshold,
			existingOwned:  true,
			wantDownloads:  1,
			wantCandidates: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newEpisodeCollectionFixture(t)
			sub := fx.createSubscription(t, nil)
			if tt.configValue != "" {
				require.NoError(t, fx.handler.configRepo.Set("min_torrent_size_bytes", tt.configValue))
			}
			if tt.existingOwned {
				seedDownloadedEpisode(t, fx, sub, model.EpisodeResource{
					Hash: "existing-hash", URL: "magnet:?xt=urn:btih:existing", Title: "Existing Anime 01",
				})
			}

			fx.handler.rssParser = &mockRSSParser{items: []rss.RSSItem{
				{
					Title:       "Test Anime 01",
					Episode:     1,
					Fansub:      "ANi",
					TorrentURL:  "magnet:?xt=urn:btih:newhash",
					TorrentHash: "new-hash",
					SizeBytes:   tt.sizeBytes,
					PubTime:     time.Now(),
				},
			}}

			err := fx.handler.doCollectEpisodes(context.Background(), nil, &sub)
			require.NoError(t, err)
			var downloadCount int64
			require.NoError(t, fx.db.Model(&model.Download{}).Where("subscription_id = ?", sub.ID).Count(&downloadCount).Error)
			assert.Equal(t, tt.wantDownloads, downloadCount)
			var candidateCount int64
			require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
			assert.Equal(t, tt.wantCandidates, candidateCount)
			assert.Zero(t, fx.qb.addCalls)
		})
	}
}

type collectEpisodesSmallTorrentQBClient struct {
	addCalls int
}

func (m *collectEpisodesSmallTorrentQBClient) Login(host, username, password string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) TestConnection(host, username, password string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) AddTorrent(torrentURL string, savePath string, category string) (string, error) {
	m.addCalls++
	return "added-hash", nil
}
func (m *collectEpisodesSmallTorrentQBClient) AddTorrentFile(filename string, fileContent []byte, savePath string, category string) (string, error) {
	m.addCalls++
	return "added-file-hash", nil
}
func (m *collectEpisodesSmallTorrentQBClient) GetTorrentInfo(hash string) (*downloader.TorrentInfo, error) {
	return nil, nil
}
func (m *collectEpisodesSmallTorrentQBClient) GetTorrentsByCategory(category string) ([]*downloader.TorrentInfo, error) {
	return nil, nil
}
func (m *collectEpisodesSmallTorrentQBClient) SetCategory(hash string, category string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) SetLocation(hash string, location string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) RenameTorrentFile(hash string, oldPath string, newPath string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) RemoveTorrentTask(hash string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) DeleteTorrentWithPayload(hash string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) GetTorrentFiles(hash string) ([]downloader.TorrentFile, error) {
	return nil, nil
}
func (m *collectEpisodesSmallTorrentQBClient) GetVersion() (string, error) {
	return "", nil
}
func (m *collectEpisodesSmallTorrentQBClient) SetProxy(proxyURL string) error {
	return nil
}
func (m *collectEpisodesSmallTorrentQBClient) DownloadTorrentFile(url string) ([]byte, error) {
	return []byte("torrent content"), nil
}

func TestSubscriptionHandler_Create(t *testing.T) {
	tests := []struct {
		name               string
		body               map[string]interface{}
		mockGetByURL       func(rssURL string) (*model.Subscription, error)
		mockGetByURLSeason func(rssURL string, season int) (*model.Subscription, error)
		mockCreate         func(sub *model.Subscription) error
		wantStatus         int
		wantCreated        bool
	}{
		{
			name: "success",
			body: map[string]interface{}{
				"name":    "New Anime",
				"rss_url": "http://example.com/rss",
				"season":  1,
			},
			mockGetByURL: func(rssURL string) (*model.Subscription, error) {
				return nil, gorm.ErrRecordNotFound // URL not in use
			},
			mockCreate: func(sub *model.Subscription) error {
				sub.ID = 1
				return nil
			},
			wantStatus:  http.StatusOK,
			wantCreated: true,
		},
		{
			name: "duplicate rss url",
			body: map[string]interface{}{
				"name":    "New Anime",
				"rss_url": "http://example.com/rss",
			},
			mockGetByURLSeason: func(rssURL string, season int) (*model.Subscription, error) {
				return &model.Subscription{ID: 1, RssURL: rssURL}, nil
			},
			mockCreate:  nil,
			wantStatus:  http.StatusConflict,
			wantCreated: false,
		},
		{
			name: "calendar only subscription",
			body: map[string]interface{}{
				"name":           "Calendar Anime",
				"source_type":    "calendar",
				"air_day":        "3",
				"air_time":       "23:30",
				"total_episodes": 12,
			},
			mockCreate: func(sub *model.Subscription) error {
				require.True(t, sub.IsCalendarOnly())
				assert.Equal(t, "calendar", sub.SourceType)
				assert.Equal(t, "never", sub.SmartFetchOverride)
				assert.Equal(t, "3", sub.AirDay)
				assert.Equal(t, "23:30", sub.AirTime)
				sub.ID = 1
				return nil
			},
			wantStatus:  http.StatusOK,
			wantCreated: true,
		},
		{
			name: "invalid body",
			body: map[string]interface{}{
				"name": 123, // wrong type
			},
			mockGetByURL: nil,
			mockCreate:   nil,
			wantStatus:   http.StatusBadRequest,
			wantCreated:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockSubscriptionRepo{
				getByRSSURLFunc:       tt.mockGetByURL,
				getByRSSURLSeasonFunc: tt.mockGetByURLSeason,
				createFunc:            tt.mockCreate,
			}
			handler := NewSubscriptionHandler(mockRepo, nil, nil, nil, "")

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/subscriptions", handler.Create)

			bodyJSON, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/subscriptions", bytes.NewBuffer(bodyJSON))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantCreated {
				assert.GreaterOrEqual(t, mockRepo.createCalls, 1)
			}
		})
	}
}

func TestSubscriptionHandler_CollectEpisodesSkipsCalendarOnly(t *testing.T) {
	parser := &mockRSSParser{}
	handler := NewSubscriptionHandler(&mockSubscriptionRepo{}, &mockDownloadRepo{}, nil, nil, "")
	handler.rssParser = parser

	err := handler.doCollectEpisodes(context.Background(), nil, &model.Subscription{
		ID:         1,
		Name:       "Calendar Anime",
		SourceType: "calendar",
		Enabled:    true,
		AirDay:     "3",
		AirTime:    "23:30",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, parser.fetchCalls)
}

// 批量操作相关方法（mock实现）
func (m *mockSubscriptionRepo) BatchUpdateEnabled(ids []uint, enabled bool) error {
	return nil
}

func (m *mockSubscriptionRepo) BatchDelete(ids []uint) error {
	return nil
}

func (m *mockSubscriptionRepo) BatchUpdateGroup(ids []uint, groupID *uint) error {
	return nil
}

// 分组管理相关方法（mock实现）
func (m *mockSubscriptionRepo) CreateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *mockSubscriptionRepo) UpdateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *mockSubscriptionRepo) DeleteGroup(id uint) error {
	return nil
}

func (m *mockSubscriptionRepo) GetGroupByID(id uint) (*model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) ListGroups() ([]model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) GetDefaultGroup() (*model.SubscriptionGroup, error) {
	return nil, nil
}

// 统计相关方法（mock实现）
func (m *mockSubscriptionRepo) GetStatistics() (*repository.SubscriptionStatistics, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) GetWeeklyUpdates() (int64, error) {
	return 0, nil
}

func (m *mockSubscriptionRepo) SearchSubscriptions(query string, groupID *uint, tagIDs []uint, enabled *bool, offset, limit int) ([]model.Subscription, int64, error) {
	return nil, 0, nil
}

func (m *mockSubscriptionRepo) CreateTag(tag *model.SubscriptionTag) error {
	return nil
}

func (m *mockSubscriptionRepo) UpdateTag(tag *model.SubscriptionTag) error {
	return nil
}

func (m *mockSubscriptionRepo) DeleteTag(id uint) error {
	return nil
}

func (m *mockSubscriptionRepo) GetTagByID(id uint) (*model.SubscriptionTag, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) GetTagByName(name string) (*model.SubscriptionTag, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) ListTags() ([]model.SubscriptionTag, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) AddTagsToSubscription(subscriptionID uint, tagIDs []uint) error {
	return nil
}

func (m *mockSubscriptionRepo) RemoveTagsFromSubscription(subscriptionID uint, tagIDs []uint) error {
	return nil
}

func (m *mockSubscriptionRepo) GetSubscriptionTags(subscriptionID uint) ([]model.SubscriptionTag, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) GetSubscriptionsByTag(tagID uint) ([]model.Subscription, error) {
	return nil, nil
}
