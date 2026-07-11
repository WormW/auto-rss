package organizer

import (
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
}
