package downloader

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

type completionLedgerFixture struct {
	db             *gorm.DB
	downloadRepo   repository.DownloadRepository
	episodeRepo    repository.EpisodeRepository
	episodeService *episodeservice.Service
	subscription   *model.Subscription
	download       *model.Download
}

func newCompletionLedgerFixture(t *testing.T, renameEnabled bool) *completionLedgerFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "completion-ledger.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	sub := &model.Subscription{Name: "Ledger Completion", Season: 1, Status: "active", RenameEnabled: renameEnabled}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Model(sub).Update("rename_enabled", renameEnabled).Error)
	sub.RenameEnabled = renameEnabled
	download := &model.Download{
		SubscriptionID: sub.ID,
		Title:          "Ledger Completion - 01",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:completion-ledger",
		TorrentHash:    "completion-ledger-hash",
		Status:         model.DownloadStatusDownloading,
	}
	require.NoError(t, db.Create(download).Error)
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
	return &completionLedgerFixture{
		db:             db,
		downloadRepo:   repository.NewDownloadRepository(db),
		episodeRepo:    episodeRepo,
		episodeService: episodeservice.NewService(episodeRepo),
		subscription:   sub,
		download:       download,
	}
}

func (f *completionLedgerFixture) handler(qb QBittorrentClient) CompletionHandler {
	return NewCompletionHandler(
		&mockSubscriptionRepo{},
		f.downloadRepo,
		nil,
		NewRenameService(""),
		qb,
		f.db,
		f.episodeService,
	)
}

func (f *completionLedgerFixture) assertCompleted(t *testing.T) {
	t.Helper()
	persisted, err := f.downloadRepo.GetByID(f.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
	assert.NotNil(t, persisted.DownloadedAt)
	ledger, err := f.episodeRepo.GetBySubscriptionAndEpisode(f.subscription.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, f.download.ID, *ledger.ActiveDownloadID)
	assert.Equal(t, f.download.TorrentHash, ledger.ActiveTorrentHash)
}

func TestCompletionHandlerRealLedgerRenameDisabledCompletesAtomically(t *testing.T) {
	fx := newCompletionLedgerFixture(t, false)
	qb := &mockQBClient{getTorrentFilesFunc: func(string) ([]TorrentFile, error) {
		return nil, errors.New("rename must remain disabled")
	}}

	require.NoError(t, fx.handler(qb).HandleComplete(
		fx.download,
		&TorrentInfo{Hash: fx.download.TorrentHash, SavePath: "/downloads/ledger-completion"},
		fx.subscription,
	))
	fx.assertCompleted(t)
}

func TestCompletionHandlerRealLedgerRenameSuccessCompletesAtomically(t *testing.T) {
	fx := newCompletionLedgerFixture(t, true)
	mediaRoot := t.TempDir()
	savePath := filepath.Join(mediaRoot, fx.subscription.Name)
	require.NoError(t, os.MkdirAll(savePath, 0755))
	qb := &mockQBClient{getTorrentFilesFunc: func(string) ([]TorrentFile, error) {
		return []TorrentFile{{Name: "episode01.mkv", Size: 1024}}, nil
	}}

	require.NoError(t, fx.handler(qb).HandleComplete(
		fx.download,
		&TorrentInfo{Hash: fx.download.TorrentHash, SavePath: savePath},
		fx.subscription,
	))
	fx.assertCompleted(t)
	assert.NotEmpty(t, fx.download.RenamedPath)
}

func TestCompletionHandlerRealLedgerRenameFailureKeepsDownloading(t *testing.T) {
	fx := newCompletionLedgerFixture(t, true)
	qb := &mockQBClient{getTorrentFilesFunc: func(string) ([]TorrentFile, error) {
		return nil, errors.New("rename failed")
	}}

	err := fx.handler(qb).HandleComplete(
		fx.download,
		&TorrentInfo{Hash: fx.download.TorrentHash, SavePath: t.TempDir()},
		fx.subscription,
	)
	require.Error(t, err)
	persisted, err := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	assert.Nil(t, persisted.DownloadedAt)
	ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(fx.subscription.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, fx.download.ID, *ledger.ActiveDownloadID)
}

func TestCompletionHandlerRealLedgerCASFailureRollsBackDownload(t *testing.T) {
	fx := newCompletionLedgerFixture(t, false)
	otherDownloadID := fx.download.ID + 100
	require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).
		Where("subscription_id = ? AND episode = ?", fx.subscription.ID, 1).
		Update("active_download_id", otherDownloadID).Error)

	err := fx.handler(&mockQBClient{}).HandleComplete(
		fx.download,
		&TorrentInfo{Hash: fx.download.TorrentHash, SavePath: "/downloads/ledger-cas"},
		fx.subscription,
	)
	require.Error(t, err)
	persisted, reloadErr := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	assert.Nil(t, persisted.DownloadedAt)
	ledger, reloadErr := fx.episodeRepo.GetBySubscriptionAndEpisode(fx.subscription.ID, 1)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, otherDownloadID, *ledger.ActiveDownloadID)
}

func TestCompletionHandlerRealDownloadDBFailureKeepsLedgerDownloading(t *testing.T) {
	fx := newCompletionLedgerFixture(t, false)
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER fail_completed_download
		BEFORE UPDATE OF status ON downloads
		WHEN NEW.status = 'completed'
		BEGIN
			SELECT RAISE(ABORT, 'injected download update failure');
		END;
	`).Error)

	err := fx.handler(&mockQBClient{}).HandleComplete(
		fx.download,
		&TorrentInfo{Hash: fx.download.TorrentHash, SavePath: "/downloads/db-failure"},
		fx.subscription,
	)
	require.Error(t, err)
	persisted, reloadErr := fx.downloadRepo.GetByID(fx.download.ID)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	assert.Nil(t, persisted.DownloadedAt)
	ledger, reloadErr := fx.episodeRepo.GetBySubscriptionAndEpisode(fx.subscription.ID, 1)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, fx.download.ID, *ledger.ActiveDownloadID)
}
