package dailystatus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetDailyStatusSeparatesDueChecksCollectionAndCompletion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{}, &model.SubscriptionFeed{}, &model.SubscriptionFeedSeenItem{},
		&model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}, &model.Download{},
	))

	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	day := time.Date(2026, time.September, 28, 0, 0, 0, 0, loc)
	checkedAt := day.Add(10 * time.Hour)
	successAt := checkedAt.Add(time.Minute)
	failedAt := day.Add(11 * time.Hour)
	createdAt := day.Add(9 * time.Hour)
	completedAt := day.Add(12 * time.Hour)
	previousDay := day.Add(-time.Hour)

	sub := model.Subscription{
		Name: "Daily Anime", Season: 1, Status: "active", Enabled: true,
		CurrentEpisode: 1, TotalEpisodes: 12, AirDay: "1", AirTime: "23:00",
		BangumiCoverLocal: "/covers/daily.jpg",
	}
	require.NoError(t, db.Create(&sub).Error)
	paused := model.Subscription{Name: "Paused Anime", Season: 1, Status: "paused", Enabled: false}
	require.NoError(t, db.Create(&paused).Error)

	feeds := []model.SubscriptionFeed{
		{SubscriptionID: sub.ID, Name: "good", RSSURL: "https://example.test/good", RSSURLNormalized: "https://example.test/good", Enabled: true, LastCheckTime: &checkedAt, LastSuccessAt: &successAt},
		{SubscriptionID: sub.ID, Name: "bad", RSSURL: "https://example.test/bad", RSSURLNormalized: "https://example.test/bad", Enabled: true, LastCheckTime: &failedAt, LastError: "timeout"},
	}
	require.NoError(t, db.Create(&feeds).Error)

	episode := model.SubscriptionEpisode{SubscriptionID: sub.ID, Episode: 3, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic}
	require.NoError(t, db.Create(&episode).Error)
	require.NoError(t, db.Create(&model.EpisodeResourceCandidate{SubscriptionEpisodeID: episode.ID, Title: "candidate", Status: model.CandidateStatusPending, CreatedAt: day.Add(8 * time.Hour)}).Error)

	download := model.Download{SubscriptionID: sub.ID, Episode: 2, Title: "Daily Anime 02", TorrentURL: "magnet:?daily", TorrentHash: "daily-hash", Status: model.DownloadStatusDownloading, CreatedAt: createdAt}
	require.NoError(t, db.Create(&download).Error)
	completed := model.Download{SubscriptionID: sub.ID, Episode: 1, Title: "Daily Anime 01", TorrentURL: "magnet:?done", TorrentHash: "done-hash", Status: model.DownloadStatusCompleted, CreatedAt: previousDay, DownloadedAt: &completedAt, RenamedPath: "/library/Daily Anime/01.mkv"}
	require.NoError(t, db.Create(&completed).Error)
	pausedDownload := model.Download{SubscriptionID: paused.ID, Episode: 1, Title: "Paused Anime 01", TorrentURL: "magnet:?paused", TorrentHash: "paused-hash", Status: model.DownloadStatusCompleted, CreatedAt: createdAt, DownloadedAt: &completedAt}
	require.NoError(t, db.Create(&pausedDownload).Error)

	service := New(db, repository.NewSubscriptionRepository(db), repository.NewDownloadRepository(db))
	service.now = func() time.Time { return day.Add(13 * time.Hour) }
	status, err := service.Get(context.Background(), "2026-09-28", "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, "2026-09-28", status.Date)
	require.Len(t, status.DueToday, 1)
	require.Equal(t, "Daily Anime", status.DueToday[0].Name)
	require.Equal(t, 2, status.DueToday[0].Episode)
	require.Len(t, status.CheckedToday, 1)
	require.Equal(t, 2, status.CheckedToday[0].FeedsChecked)
	require.Equal(t, 1, status.CheckedToday[0].FeedsSucceeded)
	require.Equal(t, 1, status.CheckedToday[0].FeedsFailed)
	require.Len(t, status.CollectedToday, 2)
	collected := status.CollectedToday[0]
	if collected.Name == "Paused Anime" {
		collected = status.CollectedToday[1]
	}
	require.Equal(t, "Daily Anime", collected.Name)
	require.Equal(t, 1, collected.DownloadCount)
	require.Equal(t, 1, collected.CandidateCount)
	require.Equal(t, []int{2, 3}, collected.Episodes)
	require.Len(t, status.DownloadedToday, 2, "completed downloads include paused subscriptions")
	require.Len(t, status.Errors, 1)
	require.Equal(t, "feed_check", status.Errors[0].Kind)
}

func TestGetDailyStatusRejectsInvalidDateAndTimezone(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.SubscriptionFeed{}, &model.SubscriptionFeedSeenItem{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}))
	service := New(db, repository.NewSubscriptionRepository(db), repository.NewDownloadRepository(db))
	service.now = func() time.Time { return time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC) }
	_, err = service.Get(context.Background(), "2026-02-30", "UTC")
	require.Error(t, err)
	_, err = service.Get(context.Background(), "2026-09-28", "Mars/Olympus")
	require.Error(t, err)
}

func TestDailyStatusOffsetsPaginationAndRetainedChecks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.SubscriptionFeed{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	// A subscription beyond the first repository page must still contribute.
	for i := 0; i < repository.MaxPageSize; i++ {
		require.NoError(t, db.Create(&model.Subscription{Name: "other", Status: "paused"}).Error)
	}
	sub := model.Subscription{Name: "Offset Anime", Status: "active", Enabled: true, EpisodeOffset: 100, AirDay: "1", TotalEpisodes: 12}
	require.NoError(t, db.Create(&sub).Error)
	stamp := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	previous := stamp.Add(-24 * time.Hour)
	ledger := model.SubscriptionEpisode{SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic}
	require.NoError(t, db.Create(&ledger).Error)
	require.NoError(t, db.Create(&model.EpisodeResourceCandidate{SubscriptionEpisodeID: ledger.ID, Status: model.CandidateStatusPending, CreatedAt: stamp}).Error)
	require.NoError(t, db.Create(&model.Download{SubscriptionID: sub.ID, Episode: 101, Title: "ep 1", TorrentHash: "ep1", CreatedAt: stamp}).Error)
	feed := model.SubscriptionFeed{SubscriptionID: sub.ID, Name: "disabled after check", RSSURL: "https://example.test/feed", RSSURLNormalized: "https://example.test/feed", LastCheckTime: &stamp, LastSuccessAt: &stamp, LastError: "later failure"}
	require.NoError(t, db.Create(&feed).Error)
	require.NoError(t, db.Model(&feed).Update("enabled", false).Error)
	service := New(db, repository.NewSubscriptionRepository(db), repository.NewDownloadRepository(db))
	status, err := service.Get(context.Background(), "2026-09-28", "UTC")
	require.NoError(t, err)
	require.Len(t, status.DueToday, 1)
	require.Equal(t, 1, status.DueToday[0].Episode)
	require.Len(t, status.CollectedToday, 1)
	require.Equal(t, []int{1, 2}, status.CollectedToday[0].Episodes)
	require.Len(t, status.CheckedToday, 1)
	require.Equal(t, 0, status.CheckedToday[0].FeedsSucceeded)
	require.Equal(t, 1, status.CheckedToday[0].FeedsFailed)
	// Do not represent a success timestamp from a previous day as today's success.
	require.NoError(t, db.Model(&feed).Updates(map[string]any{"last_success_at": previous, "last_error": "timeout"}).Error)
	status, err = service.Get(context.Background(), "2026-09-27", "UTC")
	require.NoError(t, err)
	require.Empty(t, status.CheckedToday)
	require.NotEmpty(t, status.Notes, "retained state cannot reconstruct historical checks")
}

func TestScheduleForDayConvertsMidnightAcrossTimezones(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	sunday := time.Date(2026, 9, 27, 0, 0, 0, 0, loc)
	sub := model.Subscription{AirDay: "1", AirTime: "00:30", AirTimezone: "JST"}
	air, due := scheduleForDay(sub, sub.AirDay, sunday)
	require.True(t, due)
	require.Equal(t, "23:30", air)
	_, due = scheduleForDay(sub, sub.AirDay, sunday.AddDate(0, 0, 1))
	require.False(t, due)
	sub.AirDate = "2026-10-05"
	_, due = scheduleForDay(sub, sub.AirDay, sunday)
	require.False(t, due, "future premieres should not be due this week")
	sub.AirTime = ""
	_, due = scheduleForDay(sub, sub.AirDay, sunday)
	require.False(t, due, "without a time, keep the configured weekday")
}

func TestDailyStatusKeepsDuplicateNamesInStableOrder(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.SubscriptionFeed{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	stamp := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for id := uint(1); id <= 12; id++ {
		require.NoError(t, db.Create(&model.Subscription{ID: id, Name: "Same title", AirDay: "1"}).Error)
		require.NoError(t, db.Create(&model.SubscriptionFeed{SubscriptionID: id, Name: "Same feed", LastCheckTime: &stamp, LastSuccessAt: &stamp}).Error)
		require.NoError(t, db.Create(&model.Download{SubscriptionID: id, Title: "Same title", TorrentHash: fmt.Sprint(id), Episode: 1, Status: model.DownloadStatusCompleted, CreatedAt: stamp, DownloadedAt: &stamp}).Error)
	}
	service := New(db, repository.NewSubscriptionRepository(db), repository.NewDownloadRepository(db))
	for run := 0; run < 3; run++ {
		status, err := service.Get(context.Background(), "2026-09-28", "UTC")
		require.NoError(t, err)
		require.Len(t, status.CheckedToday, 12)
		require.Len(t, status.CollectedToday, 12)
		for i := range status.CheckedToday {
			require.Equal(t, uint(i+1), status.CheckedToday[i].SubscriptionID)
			require.Equal(t, uint(i+1), status.CollectedToday[i].SubscriptionID)
			require.Equal(t, uint(i+1), status.DownloadedToday[i].SubscriptionID)
		}
	}
}
