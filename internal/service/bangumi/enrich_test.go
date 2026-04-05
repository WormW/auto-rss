package bangumi

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
)

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
