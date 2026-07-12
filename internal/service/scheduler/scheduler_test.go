package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type schedulerLedgerFixture struct {
	db             *gorm.DB
	scheduler      *scheduler
	downloadRepo   repository.DownloadRepository
	episodeRepo    repository.EpisodeRepository
	episodeService *episode.Service
	parser         *schedulerRSSParser
	qb             *schedulerQBClient
	feedRepo       repository.SubscriptionFeedRepository
}

func newSchedulerLedgerFixture(t *testing.T, items []rss.RSSItem) *schedulerLedgerFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
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

	subscriptionRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)
	require.NoError(t, configRepo.Set("smart_fetch.enabled", "false"))
	episodeRepo := repository.NewEpisodeRepository(db)
	feedRepo := repository.NewSubscriptionFeedRepository(db)
	episodeService := episode.NewService(episodeRepo)
	parser := &schedulerRSSParser{items: items}
	qb := &schedulerQBClient{}
	created := NewScheduler(db, subscriptionRepo, feedRepo, downloadRepo, configRepo, "30m", parser, qb, episodeService)

	return &schedulerLedgerFixture{
		db:             db,
		scheduler:      created.(*scheduler),
		downloadRepo:   downloadRepo,
		episodeRepo:    episodeRepo,
		episodeService: episodeService,
		parser:         parser,
		qb:             qb,
		feedRepo:       feedRepo,
	}
}

func (f *schedulerLedgerFixture) seedFeed(
	t *testing.T,
	subscriptionID uint,
	name, rssURL string,
	offset int,
	baselinePending bool,
) model.SubscriptionFeed {
	t.Helper()
	feed := model.SubscriptionFeed{
		SubscriptionID:   subscriptionID,
		Name:             name,
		RSSURL:           rssURL,
		RSSURLNormalized: rssURL,
		EpisodeOffset:    offset,
		Enabled:          true,
		BaselinePending:  baselinePending,
	}
	require.NoError(t, f.feedRepo.Create(&feed))
	return feed
}

func (f *schedulerLedgerFixture) defaultFeed(t *testing.T, subscriptionID uint) model.SubscriptionFeed {
	t.Helper()
	feeds, err := f.feedRepo.ListBySubscription(subscriptionID)
	require.NoError(t, err)
	require.NotEmpty(t, feeds)
	return feeds[0]
}

func (f *schedulerLedgerFixture) createSubscription(t *testing.T) model.Subscription {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	watermark := now.Add(-2 * time.Hour)
	sub := model.Subscription{
		Name:               "Ledger Anime",
		RssURL:             "https://example.test/feed.xml",
		Status:             "active",
		Enabled:            true,
		CreatedAt:          now.Add(-24 * time.Hour),
		LastRSSPubTime:     &watermark,
		TotalEpisodes:      12,
		LanguagePreference: "both",
	}
	require.NoError(t, f.db.Create(&sub).Error)
	f.seedFeed(t, sub.ID, "Default", sub.RssURL, sub.EpisodeOffset, sub.RSSBaselinePending)
	return sub
}

func schedulerRSSItem(episodeNumber int, hash string, pubTime time.Time) rss.RSSItem {
	return rss.RSSItem{
		Title:       fmt.Sprintf("Ledger Anime - %02d - %s", episodeNumber, hash),
		TorrentURL:  "https://example.test/" + hash + ".torrent",
		TorrentHash: hash,
		PubTime:     pubTime,
		Fansub:      "Group",
		Episode:     episodeNumber,
		Language:    rss.LangCHS,
	}
}

func TestRSSCheckMapsDifferentFeedOffsetsToOneDownload(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	a := fx.seedFeed(t, sub.ID, "A", "https://a.test/rss", 0, false)
	b := fx.seedFeed(t, sub.ID, "B", "https://b.test/rss", 100, false)
	now := time.Now().UTC()
	fx.parser.set(a.RSSURL, []rss.RSSItem{schedulerRSSItem(1, "feed-a", now)})
	itemB := schedulerRSSItem(101, "feed-b", now)
	itemB.TorrentURL = "https://b.test/101"
	fx.parser.set(b.RSSURL, []rss.RSSItem{itemB})

	fx.scheduler.checkRSSFeeds()

	assert.Zero(t, fx.qb.addCalls, "scheduler must only create an outbox download")
	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	require.NotNil(t, downloads[0].SubscriptionFeedID)
	var candidates int64
	require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidates).Error)
	assert.EqualValues(t, 1, candidates)
}

func TestNewFeedBaselineDoesNotDownloadHistoricalMissingEpisodes(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	feed := fx.seedFeed(t, sub.ID, "B", "https://b.test/rss", 100, true)
	base := time.Now().UTC().Add(-time.Hour)
	first := schedulerRSSItem(101, "baseline-b1", base)
	second := schedulerRSSItem(102, "baseline-b2", base.Add(time.Minute))
	fx.parser.set(feed.RSSURL, []rss.RSSItem{first, second})

	fx.scheduler.checkRSSFeeds()

	assert.Zero(t, fx.qb.addCalls)
	var downloads int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.Zero(t, downloads)
	for _, episodeNumber := range []int{1, 2} {
		ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, episodeNumber)
		require.NoError(t, err)
		assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
	}
	stored, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.False(t, stored.BaselinePending)
	require.NotNil(t, stored.LastRSSPubTime)
	assert.Equal(t, base.Add(time.Minute), *stored.LastRSSPubTime)
}

func TestManualCollectionBackfillsHistoricalMissingEpisodesAfterBaseline(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	feed := fx.defaultFeed(t, sub.ID)
	feed.BaselinePending = true
	require.NoError(t, fx.feedRepo.Update(&feed))

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	items := []rss.RSSItem{
		schedulerRSSItem(1, "manual-backfill-1", base),
		schedulerRSSItem(2, "manual-backfill-2", base.Add(time.Minute)),
	}
	fx.parser.set(feed.RSSURL, items)

	fx.scheduler.checkRSSFeeds()

	var downloads int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.Zero(t, downloads, "automatic baseline sync must not backfill historical episodes")

	summary, err := fx.scheduler.CollectSubscription(context.Background(), sub.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.DownloadsCreated)
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.EqualValues(t, 2, downloads)
}

func TestOneFeedFailureDoesNotBlockAnotherFeed(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	failed := fx.seedFeed(t, sub.ID, "A", "https://a.test/rss", 0, false)
	healthy := fx.seedFeed(t, sub.ID, "B", "https://b.test/rss", 100, false)
	fx.parser.fail(failed.RSSURL, errors.New("timeout"))
	fx.parser.set(healthy.RSSURL, []rss.RSSItem{schedulerRSSItem(101, "healthy-b", time.Now().UTC())})

	fx.scheduler.checkRSSFeeds()

	var downloads int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.EqualValues(t, 1, downloads)
	failedStored, err := fx.feedRepo.GetByID(failed.ID)
	require.NoError(t, err)
	healthyStored, err := fx.feedRepo.GetByID(healthy.ID)
	require.NoError(t, err)
	assert.Contains(t, failedStored.LastError, "timeout")
	assert.Empty(t, healthyStored.LastError)
	assert.NotNil(t, healthyStored.LastSuccessAt)
}

func TestFeedWithoutPublicationTimesStillFindsNewEpisodesIdempotently(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	feed := fx.seedFeed(t, sub.ID, "A", "https://a.test/rss", 0, false)
	fx.parser.set(feed.RSSURL, []rss.RSSItem{schedulerRSSItem(1, "no-time-a1", time.Time{})})

	fx.scheduler.checkRSSFeeds()
	fx.scheduler.checkRSSFeeds()

	var downloads int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.EqualValues(t, 1, downloads)
}

func TestFeedConfiguredFansubFallsBackIntoDownload(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	feed := fx.seedFeed(t, sub.ID, "Fallback", "https://fallback.test/rss", 0, false)
	feed.Fansub = "Configured Group"
	require.NoError(t, fx.feedRepo.Update(&feed))
	item := schedulerRSSItem(1, "fallback-fansub", time.Now().UTC())
	item.Fansub = ""
	fx.parser.set(feed.RSSURL, []rss.RSSItem{item})

	fx.scheduler.checkRSSFeeds()

	var download model.Download
	require.NoError(t, fx.db.Where("torrent_hash = ?", "fallback-fansub").First(&download).Error)
	assert.Equal(t, "Configured Group", download.Fansub)
	require.NotNil(t, download.SubscriptionFeedID)
	assert.Equal(t, feed.ID, *download.SubscriptionFeedID)
}

func TestBaselineWithoutPublicationTimesDoesNotBackfillOnSecondCheck(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	feed := fx.seedFeed(t, sub.ID, "B", "https://b.test/rss", 100, true)
	first := schedulerRSSItem(101, "baseline-no-time-b1", time.Time{})
	fx.parser.set(feed.RSSURL, []rss.RSSItem{first})

	fx.scheduler.checkRSSFeeds()
	fx.scheduler.checkRSSFeeds()
	var downloads int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.Zero(t, downloads)

	second := schedulerRSSItem(102, "baseline-no-time-b2", time.Time{})
	fx.parser.set(feed.RSSURL, []rss.RSSItem{first, second})
	fx.scheduler.checkRSSFeeds()
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.EqualValues(t, 1, downloads)
}

func TestRSSCheckDoesNotReplaceDownloadedEpisode(t *testing.T) {
	pubTime := time.Now().UTC().Add(-time.Hour)
	fx := newSchedulerLedgerFixture(t, []rss.RSSItem{schedulerRSSItem(1, "new-hash", pubTime)})
	sub := fx.createSubscription(t)
	oldDownload := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Ledger Anime - 01 - old-hash",
		Episode:        1,
		TorrentURL:     "https://example.test/old-hash.torrent",
		TorrentHash:    "old-hash",
		Status:         model.DownloadStatusCompleted,
		Purpose:        model.DownloadPurposeNormal,
	}
	require.NoError(t, fx.db.Create(&oldDownload).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           1,
		Status:            model.EpisodeStatusDownloaded,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &oldDownload.ID,
		ActiveTorrentHash: oldDownload.TorrentHash,
		ActiveTorrentURL:  oldDownload.TorrentURL,
		ActiveTitle:       oldDownload.Title,
	}
	require.NoError(t, fx.db.Create(&ledger).Error)

	fx.scheduler.checkRSSFeeds()

	assert.Zero(t, fx.qb.addCalls)
	assert.Zero(t, fx.qb.deleteCalls)
	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	assert.Equal(t, oldDownload.ID, downloads[0].ID)
	after, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, after.Status)
	assert.Equal(t, "old-hash", after.ActiveTorrentHash)
	assert.Equal(t, oldDownload.ID, *after.ActiveDownloadID)
	candidates, err := fx.episodeRepo.ListCandidates(ledger.ID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "new-hash", candidates[0].TorrentHash)
	assert.Equal(t, model.CandidateStatusPending, candidates[0].Status)
}

func TestRSSCheckDownloadsNewMissingEpisodeOnceAndRecordsLaterCandidate(t *testing.T) {
	pubTime := time.Now().UTC().Add(-time.Hour)
	fx := newSchedulerLedgerFixture(t, []rss.RSSItem{
		schedulerRSSItem(2, "hash-a", pubTime),
		schedulerRSSItem(2, "hash-b", pubTime.Add(time.Minute)),
	})
	sub := fx.createSubscription(t)

	fx.scheduler.checkRSSFeeds()

	assert.Zero(t, fx.qb.addCalls)
	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	assert.Equal(t, "hash-a", downloads[0].TorrentHash)
	assert.Equal(t, model.DownloadStatusPending, downloads[0].Status)
	assert.Equal(t, model.DownloadPurposeNormal, downloads[0].Purpose)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, downloads[0].ID, *ledger.ActiveDownloadID)
	candidates, err := fx.episodeRepo.ListCandidates(ledger.ID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "hash-b", candidates[0].TorrentHash)
}

func TestRSSCheckDoesNotRepeatKnownResourceAcrossEntriesOrRuns(t *testing.T) {
	pubTime := time.Now().UTC().Add(-time.Hour)
	item := schedulerRSSItem(3, "same-hash", pubTime)
	fx := newSchedulerLedgerFixture(t, []rss.RSSItem{item, item})
	sub := fx.createSubscription(t)

	fx.scheduler.checkRSSFeeds()
	fx.scheduler.checkRSSFeeds()

	assert.Zero(t, fx.qb.addCalls)
	var downloadCount, candidateCount int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloadCount).Error)
	require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
	assert.EqualValues(t, 1, downloadCount)
	assert.Zero(t, candidateCount)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 3)
	require.NoError(t, err)
	assert.Equal(t, "same-hash", ledger.ActiveTorrentHash)
}

func TestRSSCheckLedgerFailureDoesNotAdvanceWatermarkAndRetriesNextRun(t *testing.T) {
	pubTime := time.Now().UTC().Add(-time.Hour)
	item := schedulerRSSItem(8, "retry-watermark-hash", pubTime)
	fx := newSchedulerLedgerFixture(t, []rss.RSSItem{item})
	sub := fx.createSubscription(t)
	originalWatermark := *sub.LastRSSPubTime
	feed := fx.defaultFeed(t, sub.ID)
	feed.LastRSSPubTime = &originalWatermark
	require.NoError(t, fx.feedRepo.Update(&feed))
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER block_episode_observe
		BEFORE INSERT ON subscription_episodes
		BEGIN
			SELECT RAISE(ABORT, 'observe blocked');
		END;
	`).Error)

	fx.scheduler.checkRSSFeeds()

	afterFailure, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	require.NotNil(t, afterFailure.LastRSSPubTime)
	assert.Equal(t, originalWatermark, *afterFailure.LastRSSPubTime)
	assert.Zero(t, fx.qb.addCalls)

	require.NoError(t, fx.db.Exec("DROP TRIGGER block_episode_observe").Error)
	fx.scheduler.checkRSSFeeds()

	afterRetry, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	require.NotNil(t, afterRetry.LastRSSPubTime)
	assert.Equal(t, pubTime, *afterRetry.LastRSSPubTime)
	assert.Zero(t, fx.qb.addCalls)
	var queuedCount int64
	require.NoError(t, fx.db.Model(&model.Download{}).Where("status = ?", model.DownloadStatusPending).Count(&queuedCount).Error)
	assert.EqualValues(t, 1, queuedCount)
}

func TestRSSSourceBaselineReconcilesLedgerWithoutCreatingDownloads(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	items := []rss.RSSItem{
		schedulerRSSItem(1, "new-source-episode-1", now.Add(-72*time.Hour)),
		schedulerRSSItem(2, "new-source-episode-2", now.Add(-71*time.Hour)),
		schedulerRSSItem(3, "new-source-episode-3", now.Add(-70*time.Hour)),
		schedulerRSSItem(99, "new-source-out-of-range", now),
	}
	for i := range items {
		items[i].SizeBytes = 1
	}
	fx := newSchedulerLedgerFixture(t, items)
	sub := fx.createSubscription(t)
	oldWatermark := now.Add(-96 * time.Hour)
	require.NoError(t, fx.db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{
		"filter_keywords":     "will-not-match",
		"language_preference": "cht",
	}).Error)
	feed := fx.defaultFeed(t, sub.ID)
	feed.RSSURL = "https://new.example/feed.xml"
	feed.RSSURLNormalized = feed.RSSURL
	feed.BaselinePending = true
	feed.LastRSSPubTime = &oldWatermark
	require.NoError(t, fx.feedRepo.Update(&feed))

	oldDownload := model.Download{
		SubscriptionID: sub.ID,
		Title:          "old source episode 1",
		Episode:        1,
		TorrentURL:     "https://old.example/episode-1.torrent",
		TorrentHash:    "old-source-episode-1",
		Status:         model.DownloadStatusCompleted,
		Purpose:        model.DownloadPurposeNormal,
	}
	require.NoError(t, fx.db.Create(&oldDownload).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           1,
		Status:            model.EpisodeStatusDownloaded,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &oldDownload.ID,
		ActiveTorrentHash: oldDownload.TorrentHash,
		ActiveTorrentURL:  oldDownload.TorrentURL,
		ActiveTitle:       oldDownload.Title,
	}
	require.NoError(t, fx.db.Create(&ledger).Error)

	fx.scheduler.checkRSSFeeds()

	assert.Zero(t, fx.qb.addCalls)
	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	assert.Equal(t, oldDownload.ID, downloads[0].ID)

	for episodeNumber := 2; episodeNumber <= 3; episodeNumber++ {
		entry, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, episodeNumber)
		require.NoError(t, err)
		assert.Equal(t, model.EpisodeStatusMissing, entry.Status)
	}
	candidates, err := fx.episodeRepo.ListCandidates(ledger.ID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "new-source-episode-1", candidates[0].TorrentHash)
	assert.Equal(t, model.CandidateStatusPending, candidates[0].Status)

	got, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.False(t, got.BaselinePending)
	require.NotNil(t, got.LastRSSPubTime)
	assert.Equal(t, items[3].PubTime, *got.LastRSSPubTime)
	assert.NotNil(t, got.LastCheckTime)
}

func TestRSSSourceBaselineFailureKeepsPendingAndWatermarkUntilRetry(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	items := []rss.RSSItem{
		schedulerRSSItem(1, "baseline-retry-1", now.Add(-48*time.Hour)),
		schedulerRSSItem(2, "baseline-retry-2", now.Add(-47*time.Hour)),
	}
	fx := newSchedulerLedgerFixture(t, items)
	sub := fx.createSubscription(t)
	originalWatermark := now.Add(-72 * time.Hour)
	feed := fx.defaultFeed(t, sub.ID)
	feed.BaselinePending = true
	feed.LastRSSPubTime = &originalWatermark
	require.NoError(t, fx.feedRepo.Update(&feed))
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER block_baseline_observe
		BEFORE INSERT ON subscription_episodes
		WHEN NEW.episode = 2
		BEGIN SELECT RAISE(ABORT, 'baseline observe blocked'); END;
	`).Error)

	fx.scheduler.checkRSSFeeds()

	afterFailure, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.True(t, afterFailure.BaselinePending)
	require.NotNil(t, afterFailure.LastRSSPubTime)
	assert.Equal(t, originalWatermark, *afterFailure.LastRSSPubTime)
	var downloads int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
	assert.Zero(t, downloads)

	require.NoError(t, fx.db.Exec("DROP TRIGGER block_baseline_observe").Error)
	fx.scheduler.checkRSSFeeds()

	afterRetry, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.False(t, afterRetry.BaselinePending)
	require.NotNil(t, afterRetry.LastRSSPubTime)
	assert.Equal(t, items[1].PubTime, *afterRetry.LastRSSPubTime)
	for episodeNumber := 1; episodeNumber <= 2; episodeNumber++ {
		entry, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, episodeNumber)
		require.NoError(t, err)
		assert.Equal(t, model.EpisodeStatusMissing, entry.Status)
	}
}

func TestEmptyRSSSourceBaselineClearsOldWatermark(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	feed := fx.defaultFeed(t, sub.ID)
	feed.BaselinePending = true
	feed.LastRSSPubTime = nil
	require.NoError(t, fx.feedRepo.Update(&feed))

	fx.scheduler.checkRSSFeeds()

	got, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.False(t, got.BaselinePending)
	assert.Nil(t, got.LastRSSPubTime)
	assert.NotNil(t, got.LastCheckTime)
}

func TestRSSSourceBaselineCompletionFailureKeepsPendingAndWatermark(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	items := []rss.RSSItem{
		schedulerRSSItem(1, "completion-rollback-candidate", now.Add(-2*time.Hour)),
		schedulerRSSItem(2, "completion-rollback-missing", now.Add(-time.Hour)),
	}
	fx := newSchedulerLedgerFixture(t, items)
	sub := fx.createSubscription(t)
	originalWatermark := *sub.LastRSSPubTime
	feed := fx.defaultFeed(t, sub.ID)
	feed.BaselinePending = true
	feed.LastRSSPubTime = &originalWatermark
	require.NoError(t, fx.feedRepo.Update(&feed))
	oldDownload := model.Download{
		SubscriptionID: sub.ID,
		Title:          "old source episode 1",
		Episode:        1,
		TorrentURL:     "https://old.example/episode-1.torrent",
		TorrentHash:    "completion-rollback-old",
		Status:         model.DownloadStatusCompleted,
		Purpose:        model.DownloadPurposeNormal,
	}
	require.NoError(t, fx.db.Create(&oldDownload).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           1,
		Status:            model.EpisodeStatusDownloaded,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &oldDownload.ID,
		ActiveTorrentHash: oldDownload.TorrentHash,
		ActiveTorrentURL:  oldDownload.TorrentURL,
		ActiveTitle:       oldDownload.Title,
	}
	require.NoError(t, fx.db.Create(&ledger).Error)
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER block_baseline_completion
		BEFORE UPDATE OF baseline_pending ON subscription_feeds
		WHEN NEW.baseline_pending = 0
		BEGIN SELECT RAISE(ABORT, 'baseline completion blocked'); END;
	`).Error)

	fx.scheduler.checkRSSFeeds()

	afterFailure, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.True(t, afterFailure.BaselinePending)
	require.NotNil(t, afterFailure.LastRSSPubTime)
	assert.Equal(t, originalWatermark, *afterFailure.LastRSSPubTime)
	var candidateCount int64
	require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
	assert.EqualValues(t, 1, candidateCount)
	missing, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, missing.Status)
	unchanged, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, oldDownload.TorrentHash, unchanged.ActiveTorrentHash)

	require.NoError(t, fx.db.Exec("DROP TRIGGER block_baseline_completion").Error)
	fx.scheduler.checkRSSFeeds()

	afterRetry, err := fx.feedRepo.GetByID(feed.ID)
	require.NoError(t, err)
	assert.False(t, afterRetry.BaselinePending)
	require.NotNil(t, afterRetry.LastRSSPubTime)
	assert.Equal(t, items[1].PubTime, *afterRetry.LastRSSPubTime)
	assert.NotNil(t, afterRetry.LastCheckTime)
	require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
	assert.EqualValues(t, 1, candidateCount)
	missing, err = fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, missing.Status)
}

func TestSmartFetchAlwaysFetchesPendingRSSBaselineAfterCalendarGuard(t *testing.T) {
	filter := NewSmartFetchFilter(nil, nil)
	filter.strategy = DefaultSmartFetchStrategy()
	filter.strategy.Enabled = true
	filter.strategy.CheckLocalComplete = false
	filter.strategy.SkipCompleted = true
	filter.strategy.CompletedStopDays = 1
	completedAt := time.Now().Add(-30 * 24 * time.Hour)

	sub := model.Subscription{
		Name:               "Completed Show",
		RssURL:             "https://new.example/feed",
		SourceType:         "manual",
		RSSBaselinePending: true,
		TotalEpisodes:      1,
		CurrentEpisode:     1,
		CompletedAt:        &completedAt,
	}
	status, _ := filter.EvaluateSubscription(&sub, true)
	assert.True(t, status.ShouldFetch)
	assert.Equal(t, "feed_baseline_pending", status.FetchReason)

	sub.RssURL = ""
	sub.SourceType = "calendar"
	status, _ = filter.EvaluateSubscription(&sub, true)
	assert.False(t, status.ShouldFetch)
	assert.Equal(t, "calendar_only", status.FetchReason)
}

func TestProcessDownloadItemCreatesPendingIntentWithoutQBAdd(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	item := schedulerRSSItem(4, "pending-intent-hash", time.Now().UTC())
	decision, err := fx.episodeService.EvaluateRSSItem(t.Context(), &sub, episode.RSSResource{
		OriginalEpisode: item.Episode,
		RelativeEpisode: item.Episode,
		Resource:        model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title},
	}, false)
	require.NoError(t, err)
	require.Equal(t, episode.DecisionDownload, decision.Action)

	created, err := fx.scheduler.processDownloadItem(&sub, &item, decision.EpisodeID, nil)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Zero(t, fx.qb.addCalls)
	var download model.Download
	require.NoError(t, fx.db.Where("torrent_hash = ?", item.TorrentHash).First(&download).Error)
	assert.Equal(t, model.DownloadStatusPending, download.Status)
	assert.Empty(t, download.ErrorMessage)
	assert.Empty(t, download.LastError)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 4)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, download.ID, *ledger.ActiveDownloadID)
	assert.Equal(t, item.TorrentHash, ledger.ActiveTorrentHash)
}

func TestRSSCheckSecondPauseCheckReleasesClaimAndKeepsWatermark(t *testing.T) {
	pubTime := time.Now().UTC().Add(-time.Hour)
	item := schedulerRSSItem(9, "pause-race-hash", pubTime)
	fx := newSchedulerLedgerFixture(t, []rss.RSSItem{item})
	sub := fx.createSubscription(t)
	originalWatermark := *sub.LastRSSPubTime
	pauseChecks := 0
	fx.scheduler.downloadsPaused = func() bool {
		pauseChecks++
		return pauseChecks >= 2
	}

	fx.scheduler.checkRSSFeeds()

	assert.Equal(t, 2, pauseChecks)
	assert.Zero(t, fx.qb.addCalls)
	var downloadCount int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloadCount).Error)
	assert.Zero(t, downloadCount)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, item.Episode)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
	assert.Nil(t, ledger.ActiveDownloadID)
	assert.Empty(t, ledger.ActiveTorrentHash)
	after, err := repository.NewSubscriptionRepository(fx.db).GetByID(sub.ID)
	require.NoError(t, err)
	require.NotNil(t, after.LastRSSPubTime)
	assert.Equal(t, originalWatermark, *after.LastRSSPubTime)
}

func TestProcessDownloadItemPauseReturnsRetryableErrorAndReleasesClaim(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	item := schedulerRSSItem(10, "paused-process-hash", time.Now().UTC())
	resource := model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title}
	decision, err := fx.episodeService.EvaluateRSSItem(t.Context(), &sub, episode.RSSResource{
		OriginalEpisode: item.Episode,
		RelativeEpisode: item.Episode,
		Resource:        resource,
	}, false)
	require.NoError(t, err)
	require.Equal(t, episode.DecisionDownload, decision.Action)
	fx.scheduler.downloadsPaused = func() bool { return true }

	created, err := fx.scheduler.processDownloadItem(&sub, &item, decision.EpisodeID, nil)

	assert.False(t, created)
	require.ErrorIs(t, err, errDownloadsPaused)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, item.Episode)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
	assert.Nil(t, ledger.ActiveDownloadID)
	assert.Empty(t, ledger.ActiveTorrentHash)
}

func TestProcessDownloadItemCreateFailureReleasesOnlyMatchingClaim(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	existing := model.Download{
		SubscriptionID: sub.ID,
		Title:          "existing",
		Episode:        99,
		TorrentURL:     "https://example.test/duplicate.torrent",
		TorrentHash:    "duplicate-hash",
		Status:         model.DownloadStatusDownloading,
		Purpose:        model.DownloadPurposeNormal,
	}
	require.NoError(t, fx.db.Create(&existing).Error)
	otherResource := model.EpisodeResource{Hash: "other-hash", URL: "https://example.test/other.torrent"}
	other, claimed, err := fx.episodeRepo.ClaimForDownload(sub.ID, 6, otherResource)
	require.NoError(t, err)
	require.True(t, claimed)

	item := schedulerRSSItem(5, existing.TorrentHash, time.Now().UTC())
	resource := model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title}
	claimedEpisode, claimed, err := fx.episodeRepo.ClaimForDownload(sub.ID, 5, resource)
	require.NoError(t, err)
	require.True(t, claimed)

	created, err := fx.scheduler.processDownloadItem(&sub, &item, claimedEpisode.ID, nil)
	require.ErrorContains(t, err, "failed to create download")
	assert.False(t, created)
	var count int64
	require.NoError(t, fx.db.Model(&model.Download{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	failedLedger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 5)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, failedLedger.Status)
	assert.Empty(t, failedLedger.ActiveTorrentHash)
	_, claimed, err = fx.episodeRepo.ClaimForDownload(sub.ID, 5, resource)
	require.NoError(t, err)
	assert.True(t, claimed)
	otherAfter, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 6)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, otherAfter.Status)
	assert.Equal(t, other.ID, otherAfter.ID)
	assert.Equal(t, otherResource.Hash, otherAfter.ActiveTorrentHash)
}

func TestProcessDownloadItemAttachFailureRollsBackDownloadAndReleasesClaim(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	item := schedulerRSSItem(7, "attach-failure-hash", time.Now().UTC())
	resource := model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title}
	decision, err := fx.episodeService.EvaluateRSSItem(t.Context(), &sub, episode.RSSResource{
		OriginalEpisode: item.Episode,
		RelativeEpisode: item.Episode,
		Resource:        resource,
	}, false)
	require.NoError(t, err)
	require.Equal(t, episode.DecisionDownload, decision.Action)
	require.NotZero(t, decision.EpisodeID)

	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER block_episode_download_attach
		BEFORE UPDATE OF active_download_id ON subscription_episodes
		WHEN NEW.active_download_id IS NOT NULL
		BEGIN
			SELECT RAISE(ABORT, 'attach blocked');
		END;
	`).Error)

	created, err := fx.scheduler.processDownloadItem(&sub, &item, decision.EpisodeID, nil)
	require.ErrorContains(t, err, "failed to attach download to episode")
	require.ErrorContains(t, err, "attach blocked")
	assert.False(t, created)
	assert.Zero(t, fx.qb.addCalls)

	var downloadCount int64
	require.NoError(t, fx.db.Model(&model.Download{}).
		Where("torrent_hash = ?", item.TorrentHash).
		Count(&downloadCount).Error)
	assert.Zero(t, downloadCount, "download create must roll back with attach failure")

	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, item.Episode)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
	assert.Nil(t, ledger.ActiveDownloadID)
	assert.Empty(t, ledger.ActiveTorrentHash)
	assert.Empty(t, ledger.ActiveTorrentURL)
	assert.Empty(t, ledger.ActiveTitle)

	require.NoError(t, fx.db.Exec("DROP TRIGGER block_episode_download_attach").Error)
	reclaimed, claimed, err := fx.episodeRepo.ClaimForDownload(sub.ID, item.Episode, resource)
	require.NoError(t, err)
	require.True(t, claimed, "same resource must be claimable after attach rollback")
	require.NoError(t, fx.episodeService.ReleaseDownloadClaim(reclaimed.ID, resource))
	otherResource := model.EpisodeResource{Hash: "attach-retry-other-hash"}
	_, claimed, err = fx.episodeRepo.ClaimForDownload(sub.ID, item.Episode, otherResource)
	require.NoError(t, err)
	assert.True(t, claimed, "other resource must be claimable after the released retry")
}

type schedulerRSSParser struct {
	mu     sync.RWMutex
	items  []rss.RSSItem
	byURL  map[string][]rss.RSSItem
	errors map[string]error
}

func (p *schedulerRSSParser) set(url string, items []rss.RSSItem) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byURL == nil {
		p.byURL = make(map[string][]rss.RSSItem)
	}
	p.byURL[url] = append([]rss.RSSItem(nil), items...)
}

func (p *schedulerRSSParser) fail(url string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.errors == nil {
		p.errors = make(map[string]error)
	}
	p.errors[url] = err
}

func (p *schedulerRSSParser) FetchAndParse(url string) ([]rss.RSSItem, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.errors[url]; err != nil {
		return nil, err
	}
	if items, ok := p.byURL[url]; ok {
		return append([]rss.RSSItem(nil), items...), nil
	}
	return append([]rss.RSSItem(nil), p.items...), nil
}
func (p *schedulerRSSParser) FetchAndParseWithTimeout(url string, _ time.Duration) ([]rss.RSSItem, error) {
	return p.FetchAndParse(url)
}
func (p *schedulerRSSParser) Parse(interface{}) ([]rss.RSSItem, error) { return nil, nil }
func (p *schedulerRSSParser) ExtractFansub(string) string              { return "" }
func (p *schedulerRSSParser) ExtractEpisode(string) int                { return 0 }
func (p *schedulerRSSParser) SetProxy(string) error                    { return nil }

type schedulerQBClient struct {
	smallTorrentGuardQBClient
	addCalls    int
	deleteCalls int
}

func (q *schedulerQBClient) AddTorrent(string, string, string) (string, error) {
	q.addCalls++
	return "", nil
}
func (q *schedulerQBClient) AddTorrentExclusive(url, savePath, category, expectedHash string) (string, error) {
	return q.AddTorrent(url, savePath, category)
}

func (q *schedulerQBClient) DeleteTorrentWithPayload(string) error {
	q.deleteCalls++
	return nil
}

var _ downloader.QBittorrentClient = (*schedulerQBClient)(nil)
