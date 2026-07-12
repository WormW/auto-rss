package subscription

import (
	"errors"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/mikan"
	"gorm.io/gorm"
)

// MockBangumiEnricher mock实现
type mockBangumiEnricher struct {
	enrichFunc func(subscription *model.Subscription, force bool) error
}

func (m *mockBangumiEnricher) Enrich(subscription *model.Subscription, force bool) error {
	if m.enrichFunc != nil {
		return m.enrichFunc(subscription, force)
	}
	subscription.BangumiID = 12345
	return nil
}

// MockSubscriptionRepository mock实现
type mockSubscriptionRepo struct {
	subscriptions map[string]*model.Subscription
	createErr     error
	updateErr     error
	listErr       error
}

func (m *mockSubscriptionRepo) Create(sub *model.Subscription) error {
	if m.createErr != nil {
		return m.createErr
	}
	sub.ID = uint(len(m.subscriptions) + 1)
	m.subscriptions[sub.Name] = sub
	return nil
}

func (m *mockSubscriptionRepo) Update(sub *model.Subscription) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.subscriptions[sub.Name] = sub
	return nil
}

func (m *mockSubscriptionRepo) Delete(id uint) error { return nil }
func (m *mockSubscriptionRepo) GetByID(id uint) (*model.Subscription, error) {
	for _, sub := range m.subscriptions {
		if sub.ID == id {
			return sub, nil
		}
	}
	return nil, errors.New("not found")
}
func (m *mockSubscriptionRepo) GetByRSSURL(url string) (*model.Subscription, error) { return nil, nil }
func (m *mockSubscriptionRepo) GetByRSSURLAndSeason(url string, season int) (*model.Subscription, error) {
	for _, sub := range m.subscriptions {
		if sub.RssURL == url && sub.Season == season {
			return sub, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}
func (m *mockSubscriptionRepo) List(offset, limit int) ([]model.Subscription, int64, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	subs := make([]model.Subscription, 0, len(m.subscriptions))
	for _, sub := range m.subscriptions {
		subs = append(subs, *sub)
	}
	return subs, int64(len(subs)), nil
}
func (m *mockSubscriptionRepo) GetActiveSubscriptions() ([]model.Subscription, error) {
	return nil, nil
}
func (m *mockSubscriptionRepo) UpdateInTx(tx *gorm.DB, sub *model.Subscription) error {
	return m.Update(sub)
}
func (m *mockSubscriptionRepo) CreateInTx(_ *gorm.DB, sub *model.Subscription) error {
	return m.Create(sub)
}
func (m *mockSubscriptionRepo) GetSubscriptionsWithDownloadCount() ([]repository.SubscriptionWithStats, error) {
	return nil, nil
}

// MockMikanService mock实现
type mockMikanService struct {
	searchResult *mikan.SearchResult
	searchErr    error
	groups       []*mikan.FansubGroup
	groupsErr    error
}

func (m *mockMikanService) Search(keyword string) (*mikan.SearchResult, error) {
	return m.searchResult, m.searchErr
}

func (m *mockMikanService) GetFansubGroups(animeURL string) ([]*mikan.FansubGroup, error) {
	return m.groups, m.groupsErr
}

func (m *mockMikanService) SetProxy(proxy string) error { return nil }

// MockConfigRepository mock实现
type mockConfigRepo struct{}

func (m *mockConfigRepo) Get(key string) (*model.Config, error) { return nil, nil }
func (m *mockConfigRepo) GetCached(key string) (string, error)  { return "", nil }
func (m *mockConfigRepo) Set(key, value string) error           { return nil }
func (m *mockConfigRepo) Delete(key string) error               { return nil }
func (m *mockConfigRepo) GetAll() ([]model.Config, error)       { return nil, nil }

func TestBatchImporter_Import_CreatesSubscriptions(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Test Anime", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "TestFansub", RSS: "http://rss.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Test Anime", Fansub: "TestFansub"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if !results[0].Success {
		t.Errorf("expected success, got failure: %s", results[0].Message)
	}

	if results[0].Subscription == nil {
		t.Error("expected subscription to be set")
	}
}

func TestBatchImporter_Import_PreservesProvidedRSSURL(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Test Anime", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "TestFansub", RSS: "http://group-rss.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)
	providedRSSURL := "https://mikanani.me/RSS/Bangumi?bangumiId=3026"

	results, err := importer.Import([]ImportItem{
		{Title: "Test Anime", Fansub: "TestFansub", RssURL: providedRSSURL},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("unexpected result: %#v", results)
	}
	if results[0].Subscription == nil {
		t.Fatal("expected subscription to be set")
	}
	if results[0].Subscription.RssURL != providedRSSURL {
		t.Fatalf("RssURL = %q, want %q", results[0].Subscription.RssURL, providedRSSURL)
	}
}

func TestBatchImporter_Import_SkipsExisting(t *testing.T) {
	mockMikan := &mockMikanService{}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{
		subscriptions: map[string]*model.Subscription{
			"Existing Anime": {ID: 1, Name: "Existing Anime"},
		},
	}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Existing Anime"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !results[0].Skipped {
		t.Error("expected item to be skipped")
	}

	if !results[0].Success {
		t.Error("expected skipped item to be marked as success")
	}
}

func TestBatchImporter_Import_MatchesByTitle(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Exact Match Anime", URL: "http://test.com/1"},
						{Title: "Other Anime", URL: "http://test.com/2"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "Fansub", RSS: "http://rss.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Exact Match Anime"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !results[0].Success {
		t.Errorf("expected success, got: %s", results[0].Message)
	}
}

func TestBatchImporter_Import_FallbackToFirstResult(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Different Title", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "Fansub", RSS: "http://rss.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Search Query"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !results[0].Success {
		t.Errorf("expected success with fallback, got: %s", results[0].Message)
	}
}

func TestBatchImporter_Import_SelectsMatchingFansub(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Test Anime", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "OtherFansub", RSS: "http://rss1.test.com"},
			{Name: "PreferredFansub", RSS: "http://rss2.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Test Anime", Fansub: "PreferredFansub"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !results[0].Success {
		t.Errorf("expected success, got: %s", results[0].Message)
	}

	if results[0].Subscription != nil && results[0].Subscription.Fansub != "PreferredFansub" {
		t.Errorf("expected PreferredFansub, got %s", results[0].Subscription.Fansub)
	}
}

func TestBatchImporter_Import_UsesFirstFansubIfNoMatch(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Test Anime", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "FirstFansub", RSS: "http://rss1.test.com"},
			{Name: "SecondFansub", RSS: "http://rss2.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Test Anime", Fansub: "NonExistentFansub"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !results[0].Success {
		t.Errorf("expected success, got: %s", results[0].Message)
	}

	if results[0].Subscription != nil && results[0].Subscription.Fansub != "FirstFansub" {
		t.Errorf("expected FirstFansub, got %s", results[0].Subscription.Fansub)
	}
}

func TestBatchImporter_Import_CallsBangumiEnricher(t *testing.T) {
	enrichCalled := false
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Test Anime", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "Fansub", RSS: "http://rss.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{
		enrichFunc: func(sub *model.Subscription, force bool) error {
			enrichCalled = true
			sub.BangumiID = 99999
			return nil
		},
	}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Test Anime"},
	}

	_, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !enrichCalled {
		t.Error("expected Enrich to be called")
	}
}

func TestBatchImporter_Import_ReturnsDetailedResults(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Anime1", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "Fansub", RSS: "http://rss.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Anime1"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Title != "Anime1" {
		t.Errorf("expected title Anime1, got %s", result.Title)
	}
	if !result.Success {
		t.Errorf("expected success, got failure")
	}
	if result.Message != "导入成功" {
		t.Errorf("expected message '导入成功', got %s", result.Message)
	}
	if result.Subscription == nil {
		t.Error("expected subscription to be set")
	}
}

func TestBatchImporter_Import_HandlesMikanSearchError(t *testing.T) {
	mockMikan := &mockMikanService{
		searchErr: errors.New("network error"),
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{subscriptions: make(map[string]*model.Subscription)}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Test Anime"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Success {
		t.Error("expected failure due to search error")
	}

	if results[0].Message != "搜索失败: network error" {
		t.Errorf("unexpected error message: %s", results[0].Message)
	}
}

func TestBatchImporter_Import_HandlesRepositoryError(t *testing.T) {
	mockMikan := &mockMikanService{
		searchResult: &mikan.SearchResult{
			Groups: []*mikan.AnimeGroup{
				{
					Items: []*mikan.AnimeItem{
						{Title: "Test Anime", URL: "http://test.com/1"},
					},
				},
			},
		},
		groups: []*mikan.FansubGroup{
			{Name: "Fansub", RSS: "http://rss.test.com"},
		},
	}
	mockEnricher := &mockBangumiEnricher{}
	mockRepo := &mockSubscriptionRepo{
		subscriptions: make(map[string]*model.Subscription),
		createErr:     errors.New("database error"),
	}
	mockConfig := &mockConfigRepo{}

	importer := NewBatchImporter(mockMikan, mockEnricher, mockRepo, mockConfig)

	items := []ImportItem{
		{Title: "Test Anime"},
	}

	results, err := importer.Import(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Success {
		t.Error("expected failure due to repository error")
	}

	if results[0].Message != "创建订阅失败: database error" {
		t.Errorf("unexpected error message: %s", results[0].Message)
	}
}

// 批量操作相关方法（mock实现）

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
