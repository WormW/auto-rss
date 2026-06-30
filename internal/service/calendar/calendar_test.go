package calendar

import (
	"errors"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/gorm"
)

// MockSubscriptionRepository mocks the SubscriptionRepository interface
type MockSubscriptionRepository struct {
	mock.Mock
}

func (m *MockSubscriptionRepository) Create(sub *model.Subscription) error {
	args := m.Called(sub)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) GetByID(id uint) (*model.Subscription, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) GetByBangumiID(bangumiID string) (*model.Subscription, error) {
	args := m.Called(bangumiID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) GetByRSSURL(rssURL string) (*model.Subscription, error) {
	args := m.Called(rssURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) List(offset, limit int) ([]model.Subscription, int64, error) {
	args := m.Called(offset, limit)
	return args.Get(0).([]model.Subscription), args.Get(1).(int64), args.Error(2)
}

func (m *MockSubscriptionRepository) Update(sub *model.Subscription) error {
	args := m.Called(sub)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) Delete(id uint) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) GetActiveSubscriptions() ([]model.Subscription, error) {
	args := m.Called()
	return args.Get(0).([]model.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) GetByRSSSourceID(rssSourceID uint) ([]model.Subscription, error) {
	args := m.Called(rssSourceID)
	return args.Get(0).([]model.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) SearchByName(name string) ([]model.Subscription, error) {
	args := m.Called(name)
	return args.Get(0).([]model.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) UpdateCurrentEpisode(id uint, episode int) error {
	args := m.Called(id, episode)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) UpdateBangumiCover(id uint, coverURL string) error {
	args := m.Called(id, coverURL)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) UpdateBangumiCoverLocal(id uint, localPath string) error {
	args := m.Called(id, localPath)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) UpdateLastRSSCheck(id uint, checkTime time.Time) error {
	args := m.Called(id, checkTime)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) BatchDelete(ids []uint) error {
	args := m.Called(ids)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) BatchUpdateStatus(ids []uint, status string) error {
	args := m.Called(ids, status)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) GetSubscriptionsWithDownloadCount() ([]repository.SubscriptionWithStats, error) {
	args := m.Called()
	return args.Get(0).([]repository.SubscriptionWithStats), args.Error(1)
}

func (m *MockSubscriptionRepository) UpdateInTx(tx *gorm.DB, sub *model.Subscription) error {
	args := m.Called(tx, sub)
	return args.Error(0)
}

// MockDownloadRepository mocks the DownloadRepository interface
type MockDownloadRepository struct {
	mock.Mock
}

func (m *MockDownloadRepository) Create(download *model.Download) error {
	args := m.Called(download)
	return args.Error(0)
}

func (m *MockDownloadRepository) Update(download *model.Download) error {
	args := m.Called(download)
	return args.Error(0)
}

func (m *MockDownloadRepository) Delete(id uint) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockDownloadRepository) GetByID(id uint) (*model.Download, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Download), args.Error(1)
}

func (m *MockDownloadRepository) GetByHash(hash string) (*model.Download, error) {
	args := m.Called(hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Download), args.Error(1)
}

func (m *MockDownloadRepository) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	args := m.Called(subscriptionID, episode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Download), args.Error(1)
}

func (m *MockDownloadRepository) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	args := m.Called(subscriptionID, episode)
	return args.Get(0).([]model.Download), args.Error(1)
}

func (m *MockDownloadRepository) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	args := m.Called(subscriptionID, limit)
	return args.Get(0).([]model.Download), args.Error(1)
}

func (m *MockDownloadRepository) List(offset, limit int, status string) ([]model.Download, int64, error) {
	args := m.Called(offset, limit, status)
	return args.Get(0).([]model.Download), args.Get(1).(int64), args.Error(2)
}

func (m *MockDownloadRepository) ListBySubscriptionID(subscriptionID uint) ([]model.Download, error) {
	args := m.Called(subscriptionID)
	return args.Get(0).([]model.Download), args.Error(1)
}

func (m *MockDownloadRepository) UpdateStatus(id uint, status string) error {
	args := m.Called(id, status)
	return args.Error(0)
}

func (m *MockDownloadRepository) BatchDelete(ids []uint) error {
	args := m.Called(ids)
	return args.Error(0)
}

func (m *MockDownloadRepository) DeleteByStatus(status string) error {
	args := m.Called(status)
	return args.Error(0)
}

func (m *MockDownloadRepository) DeleteAll() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDownloadRepository) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	args := m.Called(limit)
	return args.Get(0).([]model.Download), args.Error(1)
}

func (m *MockDownloadRepository) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	args := m.Called(minRetries, maxRetries)
	return args.Get(0).([]model.Download), args.Error(1)
}

func (m *MockDownloadRepository) CreateInTx(tx *gorm.DB, download *model.Download) error {
	args := m.Called(tx, download)
	return args.Error(0)
}

func (m *MockDownloadRepository) UpdateInTx(tx *gorm.DB, download *model.Download) error {
	args := m.Called(tx, download)
	return args.Error(0)
}

func TestCalendarStructContainsDownloadRepo(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)

	assert.NotNil(t, calendar)
	assert.NotNil(t, calendar.downloadRepo)
}

func TestNewCalendarAcceptsDownloadRepoParameter(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)

	assert.NotNil(t, calendar)
}

func TestGetWeekScheduleSkipsCompletedSubscriptions(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	now := time.Now()
	weekday := int(now.Weekday())
	airDay := string(rune('0' + weekday))

	subscriptions := []model.Subscription{
		{
			ID:             1,
			Name:           "Completed Anime",
			AirDay:         airDay,
			AirTime:        "10:00",
			CurrentEpisode: 12,
			TotalEpisodes:  12,
		},
		{
			ID:             2,
			Name:           "Ongoing Anime",
			AirDay:         airDay,
			AirTime:        "11:00",
			CurrentEpisode: 5,
			TotalEpisodes:  12,
		},
		{
			ID:             3,
			Name:           "Unknown Total Anime",
			AirDay:         airDay,
			AirTime:        "12:00",
			CurrentEpisode: 99,
			TotalEpisodes:  0,
		},
	}

	mockSubRepo.On("GetActiveSubscriptions").Return(subscriptions, nil)
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(2), 6).Return(nil, errors.New("not found"))
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(3), 100).Return(nil, errors.New("not found"))

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)
	schedule, err := calendar.GetWeekSchedule(0)

	assert.NoError(t, err)
	assert.NotNil(t, schedule)
	assert.Len(t, schedule.Days[0].Items, 2)
	assert.Equal(t, "Ongoing Anime", schedule.Days[0].Items[0].Name)
	assert.Equal(t, 6, schedule.Days[0].Items[0].Episode)
	assert.False(t, schedule.Days[0].Items[0].IsCompleted)
	assert.Equal(t, "Unknown Total Anime", schedule.Days[0].Items[1].Name)
	assert.Equal(t, 100, schedule.Days[0].Items[1].Episode)
	assert.False(t, schedule.Days[0].Items[1].IsCompleted)

	mockSubRepo.AssertExpectations(t)
	mockDownloadRepo.AssertExpectations(t)
}

func TestIsDownloadedTrueWhenStatusCompleted(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	// Setup subscription that airs on current day
	now := time.Now()
	weekday := int(now.Weekday())

	subscriptions := []model.Subscription{
		{
			ID:                1,
			Name:              "Test Anime",
			AirDay:            string(rune('0' + weekday)),
			AirTime:           "12:00",
			CurrentEpisode:    5,
			TotalEpisodes:     12,
			BangumiCoverLocal: "/covers/test.jpg",
		},
	}

	mockSubRepo.On("GetActiveSubscriptions").Return(subscriptions, nil)

	// Setup completed download for episode 6
	completedDownload := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Episode:        6,
		Status:         "completed",
	}
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(1), 6).Return(completedDownload, nil)

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)
	schedule, err := calendar.GetWeekSchedule(0)

	assert.NoError(t, err)
	assert.NotNil(t, schedule)

	// Find the item for today
	var foundItem *CalendarItem
	for _, day := range schedule.Days {
		if day.IsToday && len(day.Items) > 0 {
			foundItem = &day.Items[0]
			break
		}
	}

	assert.NotNil(t, foundItem)
	assert.True(t, foundItem.IsDownloaded, "IsDownloaded should be true when download status is 'completed'")

	mockSubRepo.AssertExpectations(t)
	mockDownloadRepo.AssertExpectations(t)
}

func TestIsDownloadedFalseWhenNoDownloadFound(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	// Setup subscription that airs on current day
	now := time.Now()
	weekday := int(now.Weekday())

	subscriptions := []model.Subscription{
		{
			ID:                1,
			Name:              "Test Anime",
			AirDay:            string(rune('0' + weekday)),
			AirTime:           "12:00",
			CurrentEpisode:    5,
			TotalEpisodes:     12,
			BangumiCoverLocal: "/covers/test.jpg",
		},
	}

	mockSubRepo.On("GetActiveSubscriptions").Return(subscriptions, nil)

	// No download found for episode 6
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(1), 6).Return(nil, errors.New("not found"))

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)
	schedule, err := calendar.GetWeekSchedule(0)

	assert.NoError(t, err)
	assert.NotNil(t, schedule)

	// Find the item for today
	var foundItem *CalendarItem
	for _, day := range schedule.Days {
		if day.IsToday && len(day.Items) > 0 {
			foundItem = &day.Items[0]
			break
		}
	}

	assert.NotNil(t, foundItem)
	assert.False(t, foundItem.IsDownloaded, "IsDownloaded should be false when no download found")

	mockSubRepo.AssertExpectations(t)
	mockDownloadRepo.AssertExpectations(t)
}

func TestIsDownloadedFalseWhenStatusDownloading(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	// Setup subscription that airs on current day
	now := time.Now()
	weekday := int(now.Weekday())

	subscriptions := []model.Subscription{
		{
			ID:                1,
			Name:              "Test Anime",
			AirDay:            string(rune('0' + weekday)),
			AirTime:           "12:00",
			CurrentEpisode:    5,
			TotalEpisodes:     12,
			BangumiCoverLocal: "/covers/test.jpg",
		},
	}

	mockSubRepo.On("GetActiveSubscriptions").Return(subscriptions, nil)

	// Downloading status for episode 6
	downloadingDownload := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Episode:        6,
		Status:         "downloading",
	}
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(1), 6).Return(downloadingDownload, nil)

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)
	schedule, err := calendar.GetWeekSchedule(0)

	assert.NoError(t, err)
	assert.NotNil(t, schedule)

	// Find the item for today
	var foundItem *CalendarItem
	for _, day := range schedule.Days {
		if day.IsToday && len(day.Items) > 0 {
			foundItem = &day.Items[0]
			break
		}
	}

	assert.NotNil(t, foundItem)
	assert.False(t, foundItem.IsDownloaded, "IsDownloaded should be false when download status is 'downloading'")

	mockSubRepo.AssertExpectations(t)
	mockDownloadRepo.AssertExpectations(t)
}

func TestIsDownloadedFalseWhenStatusFailed(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	// Setup subscription that airs on current day
	now := time.Now()
	weekday := int(now.Weekday())

	subscriptions := []model.Subscription{
		{
			ID:                1,
			Name:              "Test Anime",
			AirDay:            string(rune('0' + weekday)),
			AirTime:           "12:00",
			CurrentEpisode:    5,
			TotalEpisodes:     12,
			BangumiCoverLocal: "/covers/test.jpg",
		},
	}

	mockSubRepo.On("GetActiveSubscriptions").Return(subscriptions, nil)

	// Failed status for episode 6
	failedDownload := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Episode:        6,
		Status:         "failed",
	}
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(1), 6).Return(failedDownload, nil)

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)
	schedule, err := calendar.GetWeekSchedule(0)

	assert.NoError(t, err)
	assert.NotNil(t, schedule)

	// Find the item for today
	var foundItem *CalendarItem
	for _, day := range schedule.Days {
		if day.IsToday && len(day.Items) > 0 {
			foundItem = &day.Items[0]
			break
		}
	}

	assert.NotNil(t, foundItem)
	assert.False(t, foundItem.IsDownloaded, "IsDownloaded should be false when download status is 'failed'")

	mockSubRepo.AssertExpectations(t)
	mockDownloadRepo.AssertExpectations(t)
}

func TestIsDownloadedFalseWhenStatusPending(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	// Setup subscription that airs on current day
	now := time.Now()
	weekday := int(now.Weekday())

	subscriptions := []model.Subscription{
		{
			ID:                1,
			Name:              "Test Anime",
			AirDay:            string(rune('0' + weekday)),
			AirTime:           "12:00",
			CurrentEpisode:    5,
			TotalEpisodes:     12,
			BangumiCoverLocal: "/covers/test.jpg",
		},
	}

	mockSubRepo.On("GetActiveSubscriptions").Return(subscriptions, nil)

	// Pending status for episode 6
	pendingDownload := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Episode:        6,
		Status:         "pending",
	}
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(1), 6).Return(pendingDownload, nil)

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)
	schedule, err := calendar.GetWeekSchedule(0)

	assert.NoError(t, err)
	assert.NotNil(t, schedule)

	// Find the item for today
	var foundItem *CalendarItem
	for _, day := range schedule.Days {
		if day.IsToday && len(day.Items) > 0 {
			foundItem = &day.Items[0]
			break
		}
	}

	assert.NotNil(t, foundItem)
	assert.False(t, foundItem.IsDownloaded, "IsDownloaded should be false when download status is 'pending'")

	mockSubRepo.AssertExpectations(t)
	mockDownloadRepo.AssertExpectations(t)
}

func TestIsDownloadedFalseWhenStatusStalled(t *testing.T) {
	mockSubRepo := new(MockSubscriptionRepository)
	mockDownloadRepo := new(MockDownloadRepository)

	// Setup subscription that airs on current day
	now := time.Now()
	weekday := int(now.Weekday())

	subscriptions := []model.Subscription{
		{
			ID:                1,
			Name:              "Test Anime",
			AirDay:            string(rune('0' + weekday)),
			AirTime:           "12:00",
			CurrentEpisode:    5,
			TotalEpisodes:     12,
			BangumiCoverLocal: "/covers/test.jpg",
		},
	}

	mockSubRepo.On("GetActiveSubscriptions").Return(subscriptions, nil)

	// Stalled status for episode 6
	stalledDownload := &model.Download{
		ID:             1,
		SubscriptionID: 1,
		Episode:        6,
		Status:         "stalled",
	}
	mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(1), 6).Return(stalledDownload, nil)

	calendar := NewCalendar(mockSubRepo, mockDownloadRepo)
	schedule, err := calendar.GetWeekSchedule(0)

	assert.NoError(t, err)
	assert.NotNil(t, schedule)

	// Find the item for today
	var foundItem *CalendarItem
	for _, day := range schedule.Days {
		if day.IsToday && len(day.Items) > 0 {
			foundItem = &day.Items[0]
			break
		}
	}

	assert.NotNil(t, foundItem)
	assert.False(t, foundItem.IsDownloaded, "IsDownloaded should be false when download status is 'stalled'")

	mockSubRepo.AssertExpectations(t)
	mockDownloadRepo.AssertExpectations(t)
}

// 批量操作相关方法（mock实现）

// 批量操作相关方法（mock实现）
func (m *MockSubscriptionRepository) BatchUpdateEnabled(ids []uint, enabled bool) error {
	return nil
}

func (m *MockSubscriptionRepository) BatchUpdateGroup(ids []uint, groupID *uint) error {
	return nil
}

// 分组管理相关方法（mock实现）
func (m *MockSubscriptionRepository) CreateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *MockSubscriptionRepository) UpdateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *MockSubscriptionRepository) DeleteGroup(id uint) error {
	return nil
}

func (m *MockSubscriptionRepository) GetGroupByID(id uint) (*model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) ListGroups() ([]model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetDefaultGroup() (*model.SubscriptionGroup, error) {
	return nil, nil
}

// 统计相关方法（mock实现）
func (m *MockSubscriptionRepository) GetStatistics() (*repository.SubscriptionStatistics, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetWeeklyUpdates() (int64, error) {
	return 0, nil
}
