package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	episodeservice "github.com/WormW/auto-rss/internal/service/episode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type blockingRecoveryMover struct {
	FileMover
	started chan struct{}
	release chan struct{}
}

func (m *blockingRecoveryMover) Move(src, dest string) (string, error) {
	close(m.started)
	<-m.release
	return m.FileMover.Move(src, dest)
}

type compensationFailMover struct {
	FileMover
	calls int
}

func (m *compensationFailMover) Move(src, dest string) (string, error) {
	m.calls++
	if m.calls == 2 {
		return "", errors.New("injected compensation failure")
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

func TestFileOrganizerRealLedgerMoveFailureKeepsCheckpoint(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	fx.organizer.mover = &failingOrganizerMover{FileMover: NewFileMover()}

	require.Error(t, fx.organizer.organizeFile(fx.sourcePath))

	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusOrganizing, persisted.Status)
	assert.Equal(t, fx.sourcePath, persisted.FilePath)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(fx.subscription.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
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
	require.NoError(t, organizer.organizeFile(target))

	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
	assert.Equal(t, target, persisted.RenamedPath)
	after, err := episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, after.Status)
}

func TestFileOrganizerStagesCheckpointBeforeMove(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	require.NoError(t, fx.db.Exec(`CREATE TRIGGER fail_organizing_stage BEFORE UPDATE OF status ON downloads WHEN NEW.status = 'organizing' BEGIN SELECT RAISE(ABORT, 'injected stage failure'); END;`).Error)
	info := fx.organizer.parser.Parse(filepath.Base(fx.sourcePath))
	target := fx.organizer.generateNewPath(fx.subscription, info)

	err := fx.organizer.organizeFile(fx.sourcePath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to persist organizing checkpoint")
	_, sourceErr := os.Stat(fx.sourcePath)
	require.NoError(t, sourceErr)
	_, targetErr := os.Stat(target)
	require.ErrorIs(t, targetErr, os.ErrNotExist)
	fx.assertDownloading(t, fx.download.ID)
}

func TestFileOrganizerRecoversAfterCompensationFailure(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	info := fx.organizer.parser.Parse(filepath.Base(fx.sourcePath))
	target := fx.organizer.generateNewPath(fx.subscription, info)
	require.NoError(t, fx.db.Exec(`CREATE TRIGGER fail_organizing_completion BEFORE UPDATE OF status ON subscription_episodes WHEN NEW.status = 'downloaded' BEGIN SELECT RAISE(ABORT, 'injected completion failure'); END;`).Error)
	fx.organizer.mover = &compensationFailMover{FileMover: NewFileMover()}

	err := fx.organizer.organizeFile(fx.sourcePath)
	require.Error(t, err)
	checkpoint, reloadErr := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.DownloadStatusOrganizing, checkpoint.Status)
	assert.Equal(t, fx.sourcePath, checkpoint.FilePath)
	assert.Equal(t, target, checkpoint.RenamedPath)
	_, targetErr := os.Stat(target)
	require.NoError(t, targetErr)

	require.NoError(t, fx.db.Exec("DROP TRIGGER fail_organizing_completion").Error)
	fx.organizer.mover = NewFileMover()
	fx.organizer.recoverOrganizingDownloads()

	persisted, reloadErr := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
	assert.Equal(t, target, persisted.RenamedPath)
	ledger, reloadErr := fx.episodeRepo.GetBySubscriptionAndEpisode(fx.subscription.ID, 1)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.EpisodeStatusDownloaded, ledger.Status)
}

func TestFileOrganizerRecoversCheckpointBeforeMove(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	info := fx.organizer.parser.Parse(filepath.Base(fx.sourcePath))
	target := fx.organizer.generateNewPath(fx.subscription, info)
	require.NoError(t, fx.db.Model(&model.Download{}).Where("id = ?", fx.download.ID).Updates(map[string]any{
		"status": model.DownloadStatusOrganizing, "file_path": fx.sourcePath, "renamed_path": target,
	}).Error)

	fx.organizer.recoverOrganizingDownloads()

	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
	_, sourceErr := os.Stat(fx.sourcePath)
	require.ErrorIs(t, sourceErr, os.ErrNotExist)
	_, targetErr := os.Stat(target)
	require.NoError(t, targetErr)
}

func TestFileOrganizerRecoveryFailureKeepsCheckpoint(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	info := fx.organizer.parser.Parse(filepath.Base(fx.sourcePath))
	target := fx.organizer.generateNewPath(fx.subscription, info)
	require.NoError(t, fx.db.Model(&model.Download{}).Where("id = ?", fx.download.ID).Updates(map[string]any{
		"status": model.DownloadStatusOrganizing, "file_path": fx.sourcePath, "renamed_path": target,
	}).Error)
	fx.organizer.mover = &failingOrganizerMover{FileMover: NewFileMover()}

	fx.organizer.recoverOrganizingDownloads()

	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusOrganizing, persisted.Status)
	assert.Equal(t, fx.sourcePath, persisted.FilePath)
	assert.Equal(t, target, persisted.RenamedPath)
}

func TestFileOrganizerRecoveryRejectsDistinctSourceAndTarget(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	info := fx.organizer.parser.Parse(filepath.Base(fx.sourcePath))
	target := fx.organizer.generateNewPath(fx.subscription, info)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("different"), 0600))
	require.NoError(t, fx.db.Model(&model.Download{}).Where("id = ?", fx.download.ID).Updates(map[string]any{
		"status": model.DownloadStatusOrganizing, "file_path": fx.sourcePath, "renamed_path": target,
	}).Error)

	fx.organizer.recoverOrganizingDownloads()

	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusOrganizing, persisted.Status)
	sourceBytes, err := os.ReadFile(fx.sourcePath)
	require.NoError(t, err)
	targetBytes, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "video", string(sourceBytes))
	assert.Equal(t, "different", string(targetBytes))
}

func TestFileOrganizerStopCancelsSleepingHandlerAndIsIdempotent(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	fx.organizer.stabilizeTime = time.Hour
	fx.organizer.handleNewFile(fx.sourcePath)
	fx.organizer.Stop()
	fx.organizer.Stop()

	_, err := os.Stat(fx.sourcePath)
	require.NoError(t, err)
	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
}

func TestFileOrganizerStopWaitsForBlockedRecoveryWithoutCompleting(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	info := fx.organizer.parser.Parse(filepath.Base(fx.sourcePath))
	target := fx.organizer.generateNewPath(fx.subscription, info)
	require.NoError(t, fx.db.Model(&model.Download{}).Where("id = ?", fx.download.ID).Updates(map[string]any{
		"status": model.DownloadStatusOrganizing, "file_path": fx.sourcePath, "renamed_path": target,
	}).Error)
	blocker := &blockingRecoveryMover{FileMover: NewFileMover(), started: make(chan struct{}), release: make(chan struct{})}
	fx.organizer.mover = blocker
	require.True(t, fx.organizer.startTask(fx.organizer.recoverOrganizingDownloads))
	<-blocker.started
	stopped := make(chan struct{})
	go func() { fx.organizer.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned before blocked recovery exited")
	case <-time.After(50 * time.Millisecond):
	}
	close(blocker.release)
	<-stopped
	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusOrganizing, persisted.Status)
}

func TestFileOrganizerRecoveryKeysetProcessesBeyondFailedFirstThousand(t *testing.T) {
	fx := newOrganizerLedgerFixture(t)
	missingSource := filepath.Join(fx.watchDir, "missing.mkv")
	missingTarget := filepath.Join(fx.destDir, "missing.mkv")
	require.NoError(t, fx.db.Exec(`
		WITH RECURSIVE seq(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM seq WHERE x < 1000)
		INSERT INTO downloads (subscription_id,title,episode,torrent_url,torrent_hash,file_path,renamed_path,status,created_at,updated_at)
		SELECT ?, 'failed-' || x, 0, 'magnet:failed-' || x, 'failed-keyset-' || x, ?, ?, 'organizing', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP FROM seq
	`, fx.subscription.ID, missingSource, missingTarget).Error)
	validSource := filepath.Join(fx.watchDir, "[Group] Organizer Ledger - 02 [1080p].mkv")
	require.NoError(t, os.WriteFile(validSource, []byte("episode2"), 0600))
	info := fx.organizer.parser.Parse(filepath.Base(validSource))
	validTarget := fx.organizer.generateNewPath(fx.subscription, info)
	valid := model.Download{SubscriptionID: fx.subscription.ID, Title: "episode2", Episode: 2, TorrentURL: "magnet:valid-keyset", TorrentHash: "valid-keyset", FilePath: validSource, RenamedPath: validTarget, Status: model.DownloadStatusOrganizing}
	require.NoError(t, fx.downloadRepo.Create(&valid))
	ledger := model.SubscriptionEpisode{SubscriptionID: fx.subscription.ID, Episode: 2, Status: model.EpisodeStatusDownloading, StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &valid.ID, ActiveTorrentHash: valid.TorrentHash}
	require.NoError(t, fx.db.Create(&ledger).Error)

	fx.organizer.recoverOrganizingDownloads()

	persisted, err := fx.downloadRepo.GetByID(valid.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
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
