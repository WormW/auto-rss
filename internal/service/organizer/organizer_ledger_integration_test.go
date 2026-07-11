package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	episodeservice "github.com/WormW/auto-rss/internal/service/episode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type compensationFailMover struct {
	FileMover
	calls int
}

func (m *compensationFailMover) Move(src, dest string) error {
	m.calls++
	if m.calls == 2 {
		return errors.New("injected compensation failure")
	}
	return m.FileMover.Move(src, dest)
}

type organizerLedgerFixture struct {
	db           *gorm.DB
	watchDir     string
	destDir      string
	sourcePath   string
	downloadRepo repository.DownloadRepository
	episodeRepo  repository.EpisodeRepository
	subscription *model.Subscription
	download     *model.Download
	organizer    *FileOrganizer
}

func newOrganizerLedgerFixture(t *testing.T) *organizerLedgerFixture {
	t.Helper()
	watchDir := t.TempDir()
	destDir := t.TempDir()
	sourcePath := filepath.Join(watchDir, "[Group] Organizer Ledger - 01 [1080p].mkv")
	require.NoError(t, os.WriteFile(sourcePath, []byte("video"), 0600))
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "organizer-ledger.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	sub := &model.Subscription{Name: "Organizer Ledger", Season: 1, Status: "active"}
	require.NoError(t, subRepo.Create(sub))
	download := &model.Download{
		SubscriptionID: sub.ID,
		Title:          "Organizer Ledger - 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:organizer-ledger",
		TorrentHash:    "organizer-ledger-hash",
		Status:         model.DownloadStatusDownloading,
	}
	require.NoError(t, downloadRepo.Create(download))
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           1,
		Status:            model.EpisodeStatusDownloading,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &download.ID,
		ActiveTorrentHash: download.TorrentHash,
		ActiveTorrentURL:  download.TorrentURL,
		ActiveTitle:       download.Title,
	}
	require.NoError(t, db.Create(&ledger).Error)
	episodeRepo := repository.NewEpisodeRepository(db)
	organizer, err := NewFileOrganizer(
		watchDir,
		destDir,
		subRepo,
		downloadRepo,
		db,
		nil,
		"",
		episodeservice.NewService(episodeRepo),
	)
	require.NoError(t, err)
	t.Cleanup(organizer.Stop)
	return &organizerLedgerFixture{
		db:           db,
		watchDir:     watchDir,
		destDir:      destDir,
		sourcePath:   sourcePath,
		downloadRepo: downloadRepo,
		episodeRepo:  episodeRepo,
		subscription: sub,
		download:     download,
		organizer:    organizer,
	}
}

func (f *organizerLedgerFixture) assertDownloading(t *testing.T, activeDownloadID uint) {
	t.Helper()
	persisted, err := f.downloadRepo.GetByID(f.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	assert.Nil(t, persisted.DownloadedAt)
	ledger, err := f.episodeRepo.GetBySubscriptionAndEpisode(f.subscription.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, activeDownloadID, *ledger.ActiveDownloadID)
}

func TestFileOrganizerRealLedgerCompletesAtomically(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)

	require.NoError(t, fx.organizer.organizeFile(fx.sourcePath))

	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
	assert.NotNil(t, persisted.DownloadedAt)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(fx.subscription.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, fx.download.ID, *ledger.ActiveDownloadID)
}

func TestFileOrganizerRealLedgerMoveFailureKeepsDownloading(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	fx.organizer.mover = &failingOrganizerMover{FileMover: NewFileMover()}

	require.Error(t, fx.organizer.organizeFile(fx.sourcePath))

	fx.assertDownloading(t, fx.download.ID)
}

func TestFileOrganizerRealLedgerCASFailureRollsBackDownload(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	otherDownloadID := fx.download.ID + 100
	require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).
		Where("subscription_id = ? AND episode = ?", fx.subscription.ID, 1).
		Update("active_download_id", otherDownloadID).Error)

	require.Error(t, fx.organizer.organizeFile(fx.sourcePath))

	fx.assertDownloading(t, otherDownloadID)
	_, sourceErr := os.Stat(fx.sourcePath)
	require.NoError(t, sourceErr, "failed persistence must compensate the file move")
	info := fx.organizer.parser.Parse(filepath.Base(fx.sourcePath))
	_, targetErr := os.Stat(fx.organizer.generateNewPath(fx.subscription, info))
	require.ErrorIs(t, targetErr, os.ErrNotExist)
}

func TestFileOrganizerSamePathCompletesInterruptedPersistence(t *testing.T) {
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "same-path.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{}))
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	sub := model.Subscription{Name: "Same Path Show", Season: 1, Status: "active"}
	require.NoError(t, subRepo.Create(&sub))
	download := model.Download{
		SubscriptionID: sub.ID, Title: "Same Path Show - 01", Episode: 1,
		TorrentURL: "magnet:same-path", TorrentHash: "same-path-hash", Status: model.DownloadStatusDownloading,
	}
	require.NoError(t, downloadRepo.Create(&download))
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
		ActiveTorrentHash: download.TorrentHash,
	}
	require.NoError(t, db.Create(&ledger).Error)
	organizer, err := NewFileOrganizer(root, root, subRepo, downloadRepo, db, nil, "", episodeservice.NewService(episodeRepo))
	require.NoError(t, err)
	t.Cleanup(organizer.Stop)
	seedName := "[Group] Same Path Show - 01 [1080p].mkv"
	info := organizer.parser.Parse(seedName)
	target := organizer.generateNewPath(&sub, info)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("video"), 0600))

	require.NoError(t, organizer.organizeFile(target))

	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
	assert.Equal(t, target, persisted.RenamedPath)
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, after.Status)
}

func TestFileOrganizerReportsPersistenceAndCompensationFailures(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	fx.organizer.mover = &compensationFailMover{FileMover: NewFileMover()}
	otherDownloadID := fx.download.ID + 100
	require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).
		Where("subscription_id = ? AND episode = ?", fx.subscription.ID, 1).
		Update("active_download_id", otherDownloadID).Error)

	err := fx.organizer.organizeFile(fx.sourcePath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to persist organized download")
	assert.ErrorContains(t, err, "failed to compensate file move")
	assert.ErrorContains(t, err, "injected compensation failure")
}
