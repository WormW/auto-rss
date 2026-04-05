package subscription

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/mikan"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// mockSubscriptionRepo 模拟订阅仓库
type mockSubscriptionRepo struct {
	subscriptions map[uint]*model.Subscription
	nextID        uint
}

func newMockSubscriptionRepo() *mockSubscriptionRepo {
	return &mockSubscriptionRepo{
		subscriptions: make(map[uint]*model.Subscription),
		nextID:        1,
	}
}

func (m *mockSubscriptionRepo) Create(sub *model.Subscription) error {
	sub.ID = m.nextID
	m.subscriptions[sub.ID] = sub
	m.nextID++
	return nil
}

func (m *mockSubscriptionRepo) Update(sub *model.Subscription) error {
	m.subscriptions[sub.ID] = sub
	return nil
}

func (m *mockSubscriptionRepo) Delete(id uint) error {
	delete(m.subscriptions, id)
	return nil
}

func (m *mockSubscriptionRepo) GetByID(id uint) (*model.Subscription, error) {
	if sub, ok := m.subscriptions[id]; ok {
		return sub, nil
	}
	return nil, nil
}

func (m *mockSubscriptionRepo) GetByRSSURL(url string) (*model.Subscription, error) {
	for _, sub := range m.subscriptions {
		if sub.RssURL == url {
			return sub, nil
		}
	}
	return nil, nil
}

func (m *mockSubscriptionRepo) List(page, pageSize int) ([]model.Subscription, int64, error) {
	subs := make([]model.Subscription, 0, len(m.subscriptions))
	for _, sub := range m.subscriptions {
		subs = append(subs, *sub)
	}
	return subs, int64(len(subs)), nil
}

func (m *mockSubscriptionRepo) GetActiveSubscriptions() ([]model.Subscription, error) {
	subs := make([]model.Subscription, 0)
	for _, sub := range m.subscriptions {
		if sub.Enabled {
			subs = append(subs, *sub)
		}
	}
	return subs, nil
}

func (m *mockSubscriptionRepo) UpdateInTx(tx *gorm.DB, subscription *model.Subscription) error {
	return m.Update(subscription)
}

// mockBangumiEnricher 模拟 Bangumi 富化器
type mockBangumiEnricher struct{}

func (m *mockBangumiEnricher) Enrich(sub *model.Subscription, force bool) error {
	// 模拟富化：给订阅添加一些 Bangumi 数据
	if sub.BangumiID == 0 {
		sub.BangumiID = 12345
		sub.BangumiScore = 8.5
	}
	return nil
}

func TestNewBatchImporter(t *testing.T) {
	mikanService := mikan.NewMikanService("")
	enricher := &mockBangumiEnricher{}
	repo := newMockSubscriptionRepo()

	importer := NewBatchImporter(mikanService, enricher, repo)

	assert.NotNil(t, importer)
}

func TestBatchImporter_Import_EmptyItems(t *testing.T) {
	mikanService := mikan.NewMikanService("")
	enricher := &mockBangumiEnricher{}
	repo := newMockSubscriptionRepo()

	importer := NewBatchImporter(mikanService, enricher, repo)

	results, err := importer.Import([]RSSAnimeImportItem{})

	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestBatchImporter_Import_Duplicate(t *testing.T) {
	mikanService := mikan.NewMikanService("")
	enricher := &mockBangumiEnricher{}
	repo := newMockSubscriptionRepo()

	// 先创建一个已存在的订阅
	existingSub := &model.Subscription{
		ID:   1,
		Name: "Test Anime",
	}
	repo.Create(existingSub)

	importer := NewBatchImporter(mikanService, enricher, repo)

	items := []RSSAnimeImportItem{
		{Title: "Test Anime"},
	}

	results, err := importer.Import(items)

	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.True(t, results[0].Skipped)
	assert.True(t, results[0].Success)
	assert.Equal(t, "已存在,跳过", results[0].Message)
}

func TestBatchImporter_importItem_NoFansubGroup(t *testing.T) {
	mikanService := mikan.NewMikanService("")
	enricher := &mockBangumiEnricher{}
	repo := newMockSubscriptionRepo()

	importer := NewBatchImporter(mikanService, enricher, repo).(*batchImporter)

	item := RSSAnimeImportItem{
		Title: "不存在的番剧 XYZ123456789",
	}

	existingNames := make(map[string]bool)
	result := importer.importItem(item, existingNames)

	// 由于 Mikan 搜索会失败，验证返回了错误信息
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "搜索失败")
}

func TestImportResult_Struct(t *testing.T) {
	result := ImportResult{
		Title:   "Test",
		Success: true,
		Message: "Success",
		Skipped: false,
	}

	assert.Equal(t, "Test", result.Title)
	assert.True(t, result.Success)
	assert.Equal(t, "Success", result.Message)
	assert.False(t, result.Skipped)
}

func TestRSSAnimeImportItem_Struct(t *testing.T) {
	item := RSSAnimeImportItem{
		Title:      "Test Anime",
		Fansub:     "TestSub",
		RssURL:     "https://example.com/rss",
		SourceID:   1,
		SourceName: "TestSource",
	}

	assert.Equal(t, "Test Anime", item.Title)
	assert.Equal(t, "TestSub", item.Fansub)
	assert.Equal(t, "https://example.com/rss", item.RssURL)
	assert.Equal(t, uint(1), item.SourceID)
	assert.Equal(t, "TestSource", item.SourceName)
}
