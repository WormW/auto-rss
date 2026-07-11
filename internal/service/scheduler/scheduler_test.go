package scheduler

import (
	"errors"
	"fmt"
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
}

func newSchedulerLedgerFixture(t *testing.T, items []rss.RSSItem) *schedulerLedgerFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
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
	episodeService := episode.NewService(episodeRepo)
	parser := &schedulerRSSParser{items: items}
	qb := &schedulerQBClient{}
	created := NewScheduler(db, subscriptionRepo, downloadRepo, configRepo, "30m", parser, qb, episodeService)

	return &schedulerLedgerFixture{
		db:             db,
		scheduler:      created.(*scheduler),
		downloadRepo:   downloadRepo,
		episodeRepo:    episodeRepo,
		episodeService: episodeService,
		parser:         parser,
		qb:             qb,
	}
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

	assert.Equal(t, 1, fx.qb.addCalls)
	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	assert.Equal(t, "hash-a", downloads[0].TorrentHash)
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

	assert.Equal(t, 1, fx.qb.addCalls)
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
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER block_episode_observe
		BEFORE INSERT ON subscription_episodes
		BEGIN
			SELECT RAISE(ABORT, 'observe blocked');
		END;
	`).Error)

	fx.scheduler.checkRSSFeeds()

	afterFailure, err := repository.NewSubscriptionRepository(fx.db).GetByID(sub.ID)
	require.NoError(t, err)
	require.NotNil(t, afterFailure.LastRSSPubTime)
	assert.Equal(t, originalWatermark, *afterFailure.LastRSSPubTime)
	assert.Zero(t, fx.qb.addCalls)

	require.NoError(t, fx.db.Exec("DROP TRIGGER block_episode_observe").Error)
	fx.scheduler.checkRSSFeeds()

	afterRetry, err := repository.NewSubscriptionRepository(fx.db).GetByID(sub.ID)
	require.NoError(t, err)
	require.NotNil(t, afterRetry.LastRSSPubTime)
	assert.Equal(t, pubTime, *afterRetry.LastRSSPubTime)
	assert.Equal(t, 1, fx.qb.addCalls)
}

func TestProcessDownloadItemQBFailureKeepsAttachedLedgerForRetry(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	fx.qb.addErr = errors.New("qB unavailable")
	sub := fx.createSubscription(t)
	item := schedulerRSSItem(4, "failing-hash", time.Now().UTC())
	decision, err := fx.episodeService.EvaluateRSSItem(t.Context(), &sub, episode.RSSResource{
		OriginalEpisode: item.Episode,
		Resource:        model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title},
	}, false)
	require.NoError(t, err)
	require.Equal(t, episode.DecisionDownload, decision.Action)

	created, err := fx.scheduler.processDownloadItem(&sub, &item, decision.EpisodeID)
	require.ErrorContains(t, err, "qB unavailable")
	assert.False(t, created)
	var download model.Download
	require.NoError(t, fx.db.Where("torrent_hash = ?", item.TorrentHash).First(&download).Error)
	assert.Equal(t, model.DownloadStatusFailed, download.Status)
	assert.Equal(t, "qB unavailable", download.ErrorMessage)
	assert.Equal(t, "qB unavailable", download.LastError)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 4)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, download.ID, *ledger.ActiveDownloadID)
	assert.Equal(t, item.TorrentHash, ledger.ActiveTorrentHash)
	_, claimed, err := fx.episodeRepo.ClaimForDownload(sub.ID, 4, model.EpisodeResource{Hash: "other-hash"})
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestProcessDownloadItemPostQBUpdateFailureKeepsPendingDurableIntent(t *testing.T) {
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	item := schedulerRSSItem(9, "durable-intent-hash", time.Now().UTC())
	resource := model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title}
	decision, err := fx.episodeService.EvaluateRSSItem(t.Context(), &sub, episode.RSSResource{
		OriginalEpisode: item.Episode,
		Resource:        resource,
	}, false)
	require.NoError(t, err)
	require.Equal(t, episode.DecisionDownload, decision.Action)
	failingRepo := &failOnceDownloadRepository{DownloadRepository: fx.downloadRepo, failNextUpdate: true}
	fx.scheduler.downloadRepo = failingRepo
	fx.qb.returnHash = item.TorrentHash

	created, err := fx.scheduler.processDownloadItem(&sub, &item, decision.EpisodeID)
	require.ErrorContains(t, err, "failed to update download status")
	assert.False(t, created)
	assert.Equal(t, []string{downloader.AutoRssCategory}, fx.qb.addCategories)

	var pending model.Download
	require.NoError(t, fx.db.Where("torrent_hash = ?", item.TorrentHash).First(&pending).Error)
	assert.Equal(t, model.DownloadStatusPending, pending.Status)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, item.Episode)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, pending.ID, *ledger.ActiveDownloadID)

	statusSync := downloader.NewStatusSync(fx.downloadRepo, nil)
	changed, err := statusSync.UpdateStatus(&pending, &downloader.TorrentInfo{
		Hash:  item.TorrentHash,
		State: downloader.StateDownloading,
	})
	require.NoError(t, err)
	assert.True(t, changed)
	reconciled, err := fx.downloadRepo.GetByID(pending.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, reconciled.Status)
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

	created, err := fx.scheduler.processDownloadItem(&sub, &item, claimedEpisode.ID)
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

	created, err := fx.scheduler.processDownloadItem(&sub, &item, decision.EpisodeID)
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
	items []rss.RSSItem
}

func (p *schedulerRSSParser) FetchAndParse(string) ([]rss.RSSItem, error) {
	return append([]rss.RSSItem(nil), p.items...), nil
}
func (p *schedulerRSSParser) FetchAndParseWithTimeout(string, time.Duration) ([]rss.RSSItem, error) {
	return append([]rss.RSSItem(nil), p.items...), nil
}
func (p *schedulerRSSParser) Parse(interface{}) ([]rss.RSSItem, error) { return nil, nil }
func (p *schedulerRSSParser) ExtractFansub(string) string              { return "" }
func (p *schedulerRSSParser) ExtractEpisode(string) int                { return 0 }
func (p *schedulerRSSParser) SetProxy(string) error                    { return nil }

type schedulerQBClient struct {
	smallTorrentGuardQBClient
	addCalls      int
	deleteCalls   int
	addErr        error
	returnHash    string
	addCategories []string
}

func (q *schedulerQBClient) AddTorrent(_ string, _ string, category string) (string, error) {
	q.addCalls++
	q.addCategories = append(q.addCategories, category)
	if q.returnHash != "" {
		return q.returnHash, q.addErr
	}
	return "", q.addErr
}

func (q *schedulerQBClient) DeleteTorrentWithPayload(string) error {
	q.deleteCalls++
	return nil
}

var _ downloader.QBittorrentClient = (*schedulerQBClient)(nil)

type failOnceDownloadRepository struct {
	repository.DownloadRepository
	failNextUpdate bool
}

func (r *failOnceDownloadRepository) Update(download *model.Download) error {
	if r.failNextUpdate {
		r.failNextUpdate = false
		return errors.New("injected download update failure")
	}
	return r.DownloadRepository.Update(download)
}
