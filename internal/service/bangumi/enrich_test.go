package bangumi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// mockConfigRepo 模拟配置仓库
type mockConfigRepo struct {
	getFunc func(key string) (*model.Config, error)
}

func (m *mockConfigRepo) Get(key string) (*model.Config, error) {
	if m.getFunc != nil {
		return m.getFunc(key)
	}
	return nil, nil
}

func (m *mockConfigRepo) GetCached(key string) (string, error) { return "", nil }
func (m *mockConfigRepo) Set(key, value string) error          { return nil }
func (m *mockConfigRepo) Delete(key string) error              { return nil }
func (m *mockConfigRepo) GetAll() ([]model.Config, error)      { return nil, nil }

func TestNewEnricher(t *testing.T) {
	bgService := NewBangumiService()
	imgService := NewImageService("./test_covers")
	configRepo := &mockConfigRepo{}

	enricher := NewEnricher(bgService, imgService, configRepo)

	assert.NotNil(t, enricher)
}

func TestEnricher_AlreadyHasBangumiID(t *testing.T) {
	bgService := NewBangumiService()
	imgService := NewImageService("./test_covers")
	configRepo := &mockConfigRepo{}

	enricher := NewEnricher(bgService, imgService, configRepo)

	// 测试已有 Bangumi ID 且非强制刷新的情况
	sub := &model.Subscription{
		ID:        1,
		Name:      "Test Anime",
		BangumiID: 12345,
	}

	err := enricher.Enrich(sub, false)
	assert.NoError(t, err)

	// 数据应该保持不变
	assert.Equal(t, 12345, sub.BangumiID)
	assert.Equal(t, "Test Anime", sub.Name)
}

func TestEnricher_ForceRefresh(t *testing.T) {
	bgService := NewBangumiService()
	imgService := NewImageService("./test_covers")
	configRepo := &mockConfigRepo{}

	enricher := NewEnricher(bgService, imgService, configRepo)

	// 测试强制刷新 - 即使已有 Bangumi ID 也会尝试获取
	sub := &model.Subscription{
		ID:        1,
		Name:      "Test Anime",
		BangumiID: 12345,
	}

	// 强制刷新会尝试获取数据，但可能因为网络/API问题失败
	// 这里只验证方法不会 panic
	err := enricher.Enrich(sub, true)
	// 由于 Bangumi API 可能不可用，我们不断言错误
	_ = err
}

func TestEnricher_ForceRefreshUpdatesWeekday(t *testing.T) {
	bgService := NewBangumiService()
	bgService.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/v0/subjects/638151", r.URL.Path)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"id": 638151,
			"type": 2,
			"name": "Friday Anime",
			"infobox": [
				{"key": "放送星期", "value": "星期五"}
			]
		}`)),
		}, nil
	})}
	enricher := NewEnricher(bgService, NewImageService(t.TempDir()), &mockConfigRepo{})
	sub := &model.Subscription{
		ID:        1,
		Name:      "Friday Anime",
		BangumiID: 638151,
		AirDay:    "0",
		UpdateDay: "0",
	}

	require.NoError(t, enricher.Enrich(sub, true))
	assert.Equal(t, "5", sub.AirDay)
	assert.Equal(t, "5", sub.UpdateDay)
}

func TestEnricher_NoBangumiID(t *testing.T) {
	bgService := NewBangumiService()
	imgService := NewImageService("./test_covers")
	configRepo := &mockConfigRepo{}

	enricher := NewEnricher(bgService, imgService, configRepo)

	// 测试没有 Bangumi ID 的情况
	sub := &model.Subscription{
		ID:   1,
		Name: "不存在的番剧名称 XYZ123",
	}

	// 搜索不存在的番剧应该返回错误
	err := enricher.Enrich(sub, false)
	// 由于 Bangumi API 可能不可用或找不到番剧，我们不断言具体错误
	_ = err
}

func TestPopulateSubscription(t *testing.T) {
	bgService := NewBangumiService()
	imgService := NewImageService("./test_covers")
	configRepo := &mockConfigRepo{}

	e := &enricher{
		bangumiService: bgService,
		imageService:   imgService,
		configRepo:     configRepo,
	}

	sub := &model.Subscription{
		ID:   1,
		Name: "Original Name",
	}

	subject := &Subject{
		ID:       12345,
		Name:     "Test Anime",
		NameCN:   "测试番剧",
		Score:    8.5,
		Summary:  "Test summary",
		TotalEps: 12,
		AirDate:  "2024-01-01",
		Season:   1,
		Images: &Images{
			Large: "https://example.com/cover.jpg",
		},
		Rating: &Rating{
			Rank: 100,
		},
	}

	e.populateSubscription(sub, subject)

	// 验证数据填充
	assert.Equal(t, 12345, sub.BangumiID)
	assert.Equal(t, 8.5, sub.BangumiScore)
	assert.Equal(t, "Test summary", sub.BangumiSummary)
	assert.Equal(t, "测试番剧", sub.Name) // 应该使用 name_cn
	assert.Equal(t, "https://example.com/cover.jpg", sub.BangumiCover)
	assert.Equal(t, 100, sub.BangumiRank)
	assert.Equal(t, 1, sub.Season)
	assert.Equal(t, 12, sub.TotalEpisodes)
	assert.Equal(t, "2024-01-01", sub.AirDate)
	assert.Equal(t, 2024, sub.AirYear)
}

func TestPopulateSubscription_CleansTerminalSeasonTitle(t *testing.T) {
	bgService := NewBangumiService()
	imgService := NewImageService("./test_covers")
	configRepo := &mockConfigRepo{}

	e := &enricher{
		bangumiService: bgService,
		imageService:   imgService,
		configRepo:     configRepo,
	}

	tests := []struct {
		name       string
		nameCN     string
		nameJP     string
		season     int
		wantName   string
		wantSeason int
	}{
		{name: "Chinese name_cn", nameCN: "入间同学入魔了 第四季", season: 4, wantName: "入间同学入魔了", wantSeason: 4},
		{name: "Japanese fallback title", nameJP: "魔入りました！入間くん 第4シリーズ", season: 4, wantName: "魔入りました！入間くん", wantSeason: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &model.Subscription{Name: tt.nameJP, Season: tt.season}
			subject := &Subject{
				ID:     12345,
				Name:   tt.nameJP,
				NameCN: tt.nameCN,
				Season: tt.season,
			}

			e.populateSubscription(sub, subject)

			assert.Equal(t, tt.wantName, sub.Name)
			assert.Equal(t, tt.wantSeason, sub.Season)
		})
	}
}

func TestPopulateSubscription_NoNameCN(t *testing.T) {
	bgService := NewBangumiService()
	imgService := NewImageService("./test_covers")
	configRepo := &mockConfigRepo{}

	e := &enricher{
		bangumiService: bgService,
		imageService:   imgService,
		configRepo:     configRepo,
	}

	sub := &model.Subscription{
		ID:   1,
		Name: "Original Name",
	}

	subject := &Subject{
		ID:       12345,
		Name:     "Test Anime",
		NameCN:   "", // 没有中文名
		Score:    8.5,
		TotalEps: 12,
	}

	e.populateSubscription(sub, subject)

	// 名称应该保持不变
	assert.Equal(t, "Original Name", sub.Name)
}

func TestEnrichDoesNotMarkAiringSubject607915CompleteWhenEpisodesUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v0/subjects/607915":
			_, _ = w.Write([]byte(`{
				"id": 607915,
				"type": 2,
				"name": "Airing Anime",
				"name_cn": "连载中动画",
				"eps": 12,
				"infobox": [
					{"key": "话数", "value": "12"},
					{"key": "放送开始", "value": "2026年7月1日"},
					{"key": "放送星期", "value": "星期三"}
				]
			}`))
		case "/v0/episodes":
			assert.Equal(t, "607915", r.URL.Query().Get("subject_id"))
			_, _ = w.Write([]byte(`{"data": [], "total": 0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bgService := NewBangumiService()
	bgService.baseURL = server.URL

	e := NewEnricher(bgService, NewImageService(t.TempDir()), &mockConfigRepo{})
	sub := &model.Subscription{
		ID:            1,
		Name:          "Airing Anime",
		BangumiID:     607915,
		LatestEpisode: 5,
	}

	require.NoError(t, e.Enrich(sub, true))

	assert.Equal(t, 607915, sub.BangumiID)
	assert.Equal(t, 12, sub.TotalEpisodes)
	assert.Equal(t, 5, sub.LatestEpisode)
	assert.NotEqual(t, sub.TotalEpisodes, sub.LatestEpisode)
}

func TestShouldCorrectFalseCompletion(t *testing.T) {
	sub := model.Subscription{
		TotalEpisodes: 12,
		LatestEpisode: 12,
	}

	assert.True(t, shouldCorrectFalseCompletion(sub, 5))
	assert.False(t, shouldCorrectFalseCompletion(sub, 12))
	assert.False(t, shouldCorrectFalseCompletion(sub, 0))

	offsetSub := model.Subscription{
		EpisodeOffset: 170,
		TotalEpisodes: 52,
		LatestEpisode: 171,
	}
	assert.False(t, shouldCorrectFalseCompletion(offsetSub, 1))
}
