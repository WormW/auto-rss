package episode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	downloaderService "github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type replacementFixture struct {
	t          *testing.T
	db         *gorm.DB
	root       string
	repo       repository.EpisodeRepository
	downloads  repository.DownloadRepository
	subs       repository.SubscriptionRepository
	service    *ReplacementService
	downloader *fakeReplacementDownloader
	controller *fakeTorrentController
	promoter   *fakeFilePromoter
	sub        model.Subscription
	ledger     model.SubscriptionEpisode
	old        model.Download
	events     []string
	mu         sync.Mutex
}

func newReplacementFixture(t *testing.T) *replacementFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "replacement.db")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	fx := &replacementFixture{t: t, db: db, root: t.TempDir()}
	fx.repo = repository.NewEpisodeRepository(db)
	fx.downloads = repository.NewDownloadRepository(db)
	fx.subs = repository.NewSubscriptionRepository(db)
	fx.sub = model.Subscription{Name: "Replacement Show", Season: 1, Status: "active", Enabled: true}
	require.NoError(t, db.Create(&fx.sub).Error)
	fx.events = make([]string, 0, 16)
	fx.downloader = &fakeReplacementDownloader{fx: fx, stagedContent: []byte("new-content")}
	fx.controller = &fakeTorrentController{fx: fx}
	fx.promoter = &fakeFilePromoter{fx: fx, delegate: NewOSFilePromoter()}
	fx.service = NewReplacementService(db, fx.repo, fx.downloads, fx.subs, fx.downloader, fx.controller, fx.promoter)
	return fx
}

func (fx *replacementFixture) record(event string) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.events = append(fx.events, event)
}

func (fx *replacementFixture) writeOldFile(content string) string {
	fx.t.Helper()
	path := filepath.Join(fx.root, "Replacement Show", "Season 1", "Replacement Show S01E01.mkv")
	require.NoError(fx.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(fx.t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func (fx *replacementFixture) seedPendingCandidate(oldPath string) model.EpisodeResourceCandidate {
	fx.t.Helper()
	fx.old = model.Download{
		SubscriptionID: fx.sub.ID,
		Title:          "old",
		Episode:        1,
		TorrentURL:     "magnet:?xt=urn:btih:old-hash",
		TorrentHash:    "old-hash",
		RenamedPath:    oldPath,
		Status:         model.DownloadStatusCompleted,
		Purpose:        model.DownloadPurposeNormal,
	}
	require.NoError(fx.t, fx.db.Create(&fx.old).Error)
	activeID := fx.old.ID
	fx.ledger = model.SubscriptionEpisode{
		SubscriptionID:    fx.sub.ID,
		Episode:           1,
		Status:            model.EpisodeStatusDownloaded,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &activeID,
		ActiveTorrentHash: fx.old.TorrentHash,
		ActiveTorrentURL:  fx.old.TorrentURL,
		ActiveTitle:       fx.old.Title,
	}
	require.NoError(fx.t, fx.db.Create(&fx.ledger).Error)
	candidate := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: fx.ledger.ID,
		ResourceKey:           "hash:new-hash",
		TorrentHash:           "new-hash",
		TorrentURL:            "magnet:?xt=urn:btih:new-hash",
		Title:                 "new",
		Status:                model.CandidateStatusPending,
	}
	require.NoError(fx.t, fx.db.Create(&candidate).Error)
	return candidate
}

func (fx *replacementFixture) seedUntrackedMarkedCandidate() model.EpisodeResourceCandidate {
	fx.t.Helper()
	fx.ledger = model.SubscriptionEpisode{
		SubscriptionID: fx.sub.ID,
		Episode:        1,
		Status:         model.EpisodeStatusMarkedDownloaded,
		StatusSource:   model.EpisodeStatusSourceUser,
	}
	require.NoError(fx.t, fx.db.Create(&fx.ledger).Error)
	candidate := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: fx.ledger.ID,
		ResourceKey:           "hash:new-hash",
		TorrentHash:           "new-hash",
		TorrentURL:            "magnet:?xt=urn:btih:new-hash",
		Title:                 "new",
		Status:                model.CandidateStatusPending,
		FinalPath:             filepath.Join(fx.root, "Replacement Show", "Season 1", "Replacement Show S01E01.mkv"),
	}
	require.NoError(fx.t, fx.db.Create(&candidate).Error)
	return candidate
}

func (fx *replacementFixture) loadCandidate(id uint) model.EpisodeResourceCandidate {
	fx.t.Helper()
	var candidate model.EpisodeResourceCandidate
	require.NoError(fx.t, fx.db.First(&candidate, id).Error)
	return candidate
}

func (fx *replacementFixture) loadLedger() model.SubscriptionEpisode {
	fx.t.Helper()
	var ledger model.SubscriptionEpisode
	require.NoError(fx.t, fx.db.First(&ledger, fx.ledger.ID).Error)
	return ledger
}

type fakeReplacementDownloader struct {
	fx            *replacementFixture
	stagedContent []byte
	downloadErr   error
	cleanupHashes []string
	stagedOutside bool
}

func (d *fakeReplacementDownloader) DownloadToStage(ctx context.Context, candidate model.EpisodeResourceCandidate, stagedDir string) (*model.Download, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if d.downloadErr != nil {
		download := &model.Download{SubscriptionID: d.fx.sub.ID, Episode: 1, Title: candidate.Title, TorrentURL: candidate.TorrentURL, TorrentHash: candidate.TorrentHash, Status: model.DownloadStatusFailed, Purpose: model.DownloadPurposeReplacement, ReplacementCandidateID: &candidate.ID}
		require.NoError(d.fx.t, d.fx.db.Create(download).Error)
		return download, "", d.downloadErr
	}
	d.fx.record("download_new")
	staged := filepath.Join(stagedDir, "new.mkv")
	if d.stagedOutside {
		staged = filepath.Join(d.fx.root, "outside-stage.mkv")
	}
	require.NoError(d.fx.t, os.MkdirAll(filepath.Dir(staged), 0o755))
	require.NoError(d.fx.t, os.WriteFile(staged, d.stagedContent, 0o644))
	finalPath := candidate.FinalPath
	if finalPath == "" {
		finalPath = d.fx.old.RenamedPath
	}
	download := &model.Download{SubscriptionID: d.fx.sub.ID, Episode: 1, Title: candidate.Title, TorrentURL: candidate.TorrentURL, TorrentHash: candidate.TorrentHash, FilePath: staged, RenamedPath: finalPath, Status: model.DownloadStatusCompleted, Purpose: model.DownloadPurposeReplacement, ReplacementCandidateID: &candidate.ID}
	require.NoError(d.fx.t, d.fx.db.Create(download).Error)
	d.fx.record("detach_new_torrent")
	return download, staged, nil
}

func (d *fakeReplacementDownloader) CleanupFailedDownload(download *model.Download) error {
	if download != nil {
		d.cleanupHashes = append(d.cleanupHashes, download.TorrentHash)
	}
	return nil
}

type fakeTorrentController struct {
	fx        *replacementFixture
	pauseErr  error
	removeErr error
	paused    []string
	resumed   []string
	removed   []string
}

func (c *fakeTorrentController) PauseTorrent(hash string) error {
	c.fx.record("pause_old")
	c.paused = append(c.paused, hash)
	return c.pauseErr
}
func (c *fakeTorrentController) ResumeTorrent(hash string) error {
	c.fx.record("resume_old")
	c.resumed = append(c.resumed, hash)
	return nil
}
func (c *fakeTorrentController) RemoveTorrentTask(hash string) error {
	c.fx.record("remove_old_torrent")
	c.removed = append(c.removed, hash)
	return c.removeErr
}

type fakeFilePromoter struct {
	fx          *replacementFixture
	delegate    FilePromoter
	failPromote error
	afterMove   func(source, destination string)
}

func (p *fakeFilePromoter) Move(source, destination string) error {
	if strings.Contains(destination, string(filepath.Separator)+".auto-rss-rollback"+string(filepath.Separator)) {
		p.fx.record("backup_old")
	} else if strings.Contains(source, string(filepath.Separator)+".auto-rss-replacements"+string(filepath.Separator)) {
		p.fx.record("promote")
		if p.failPromote != nil {
			return p.failPromote
		}
	}
	err := p.delegate.Move(source, destination)
	if err == nil && p.afterMove != nil {
		p.afterMove(source, destination)
	}
	return err
}
func (p *fakeFilePromoter) Remove(path string) error {
	p.fx.record("remove_rollback")
	return p.delegate.Remove(path)
}
func (p *fakeFilePromoter) Exists(path string) bool { return p.delegate.Exists(path) }

func TestReplacementPromotesNewFileBeforeRemovingOldTask(t *testing.T) {
	fx := newReplacementFixture(t)
	oldPath := fx.writeOldFile("old-content")
	candidate := fx.seedPendingCandidate(oldPath)

	require.NoError(t, fx.service.Replace(context.Background(), candidate.ID))

	content, err := os.ReadFile(oldPath)
	require.NoError(t, err)
	assert.Equal(t, "new-content", string(content))
	assert.Equal(t, []string{"download_new", "detach_new_torrent", "pause_old", "backup_old", "promote", "remove_old_torrent", "remove_rollback"}, fx.events)
	ledger := fx.loadLedger()
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.NotEqual(t, fx.old.ID, *ledger.ActiveDownloadID)
	got := fx.loadCandidate(candidate.ID)
	assert.Equal(t, model.CandidateStatusAccepted, got.Status)
	assert.Equal(t, ReplacementStageDone, got.ReplacementStage)
	assert.NoFileExists(t, got.RollbackPath)
	assert.NotContains(t, fx.downloader.cleanupHashes, fx.old.TorrentHash)
}

func TestReplacementPromoteFailureRestoresOldFileAndLedger(t *testing.T) {
	fx := newReplacementFixture(t)
	oldPath := fx.writeOldFile("old-content")
	candidate := fx.seedPendingCandidate(oldPath)
	fx.promoter.failPromote = errors.New("promote failed")

	err := fx.service.Replace(context.Background(), candidate.ID)

	require.ErrorContains(t, err, "promote failed")
	content, readErr := os.ReadFile(oldPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old-content", string(content))
	assert.Equal(t, fx.old.ID, *fx.loadLedger().ActiveDownloadID)
	assert.Equal(t, model.CandidateStatusFailed, fx.loadCandidate(candidate.ID).Status)
	assert.Contains(t, fx.events, "resume_old")
	assert.NotContains(t, fx.downloader.cleanupHashes, fx.old.TorrentHash)
}

func TestReplacementCleanupFailureEndsAcceptedCleanupFailed(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	fx.controller.removeErr = errors.New("remove failed")

	err := fx.service.Replace(context.Background(), candidate.ID)

	require.ErrorContains(t, err, "remove failed")
	assert.Equal(t, model.CandidateStatusAcceptedCleanupFailed, fx.loadCandidate(candidate.ID).Status)
	assert.NotEqual(t, fx.old.ID, *fx.loadLedger().ActiveDownloadID)
}

func TestReplacementRejectsConcurrentReplacingCandidate(t *testing.T) {
	fx := newReplacementFixture(t)
	first := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	require.NoError(t, fx.db.Model(&first).Updates(map[string]any{"status": model.CandidateStatusReplacing, "replacement_stage": ReplacementStageDownloading}).Error)
	second := model.EpisodeResourceCandidate{SubscriptionEpisodeID: fx.ledger.ID, ResourceKey: "hash:second", TorrentHash: "second", TorrentURL: "magnet:second", Title: "second", Status: model.CandidateStatusPending}
	require.NoError(t, fx.db.Create(&second).Error)

	err := fx.service.Replace(context.Background(), second.ID)

	require.ErrorIs(t, err, ErrReplacementInProgress)
	assert.Equal(t, model.CandidateStatusReplacing, fx.loadCandidate(first.ID).Status)
	assert.Equal(t, model.CandidateStatusPending, fx.loadCandidate(second.ID).Status)
}

func TestReplacementMarkedDownloadedWithoutTrackedOldResourceSwitchesLedger(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedUntrackedMarkedCandidate()

	require.NoError(t, fx.service.Replace(context.Background(), candidate.ID))

	ledger := fx.loadLedger()
	assert.Equal(t, model.EpisodeStatusDownloaded, ledger.Status)
	require.NotNil(t, ledger.ActiveDownloadID)
	assert.Equal(t, "new-hash", ledger.ActiveTorrentHash)
	assert.Contains(t, fx.events, "detach_new_torrent")
	assert.NotContains(t, fx.events, "pause_old")
	assert.NotContains(t, fx.events, "remove_old_torrent")
}

func TestReplacementDownloadFailureDeletesOnlyNewPayload(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	fx.downloader.downloadErr = errors.New("download failed")

	err := fx.service.Replace(context.Background(), candidate.ID)

	require.ErrorContains(t, err, "download failed")
	assert.NotContains(t, fx.downloader.cleanupHashes, fx.old.TorrentHash)
	assert.Contains(t, fx.downloader.cleanupHashes, candidate.TorrentHash)
}

func TestQBReplacementDownloaderCreatesReplacementDownloadAndDetachesNewTask(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	require.NoError(t, fx.db.Model(&candidate).Updates(map[string]any{
		"status":            model.CandidateStatusReplacing,
		"replacement_stage": ReplacementStageDownloading,
	}).Error)
	candidate.Status = model.CandidateStatusReplacing
	candidate.ReplacementStage = ReplacementStageDownloading
	qb := &replacementQBClient{hash: "new-hash", fileName: "downloaded.mkv"}
	production := NewQBReplacementDownloader(
		fx.db,
		fx.downloads,
		repository.NewConfigRepository(fx.db),
		qb,
		"${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}",
		fx.root,
	)
	stagedDir := filepath.Join(filepath.Dir(fx.old.RenamedPath), ".auto-rss-replacements", fmt.Sprint(candidate.ID))

	download, stagedPath, err := production.DownloadToStage(context.Background(), candidate, stagedDir)

	require.NoError(t, err)
	require.NotNil(t, download)
	assert.Equal(t, model.DownloadPurposeReplacement, download.Purpose)
	require.NotNil(t, download.ReplacementCandidateID)
	assert.Equal(t, candidate.ID, *download.ReplacementCandidateID)
	assert.Equal(t, "new-hash", download.TorrentHash)
	assert.Equal(t, []string{"new-hash"}, qb.removed)
	assert.FileExists(t, stagedPath)
	relative, relErr := filepath.Rel(stagedDir, stagedPath)
	require.NoError(t, relErr)
	assert.NotContains(t, relative, "..")
}

func TestReplacementRecoveryResumesPromotedCheckpoint(t *testing.T) {
	fx := newReplacementFixture(t)
	oldPath := fx.writeOldFile("old-content")
	candidate := fx.seedPendingCandidate(oldPath)
	newDownload := fx.seedReplacementDownload(candidate, oldPath)
	rollback := filepath.Join(filepath.Dir(oldPath), ".auto-rss-rollback", fmt.Sprint(candidate.ID), filepath.Base(oldPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(rollback), 0o755))
	require.NoError(t, os.Rename(oldPath, rollback))
	require.NoError(t, os.WriteFile(oldPath, []byte("new-content"), 0o644))
	require.NoError(t, fx.db.Model(&candidate).Updates(map[string]any{
		"status":                  model.CandidateStatusReplacing,
		"replacement_stage":       ReplacementStagePromoted,
		"replacement_download_id": newDownload.ID,
		"old_download_id":         fx.old.ID,
		"old_torrent_hash":        fx.old.TorrentHash,
		"old_resource_path":       oldPath,
		"rollback_path":           rollback,
		"final_path":              oldPath,
		"staged_path":             filepath.Join(filepath.Dir(oldPath), ".auto-rss-replacements", fmt.Sprint(candidate.ID), "new.mkv"),
	}).Error)

	require.NoError(t, fx.service.RecoverIncomplete(context.Background()))
	require.NoError(t, fx.service.RecoverIncomplete(context.Background()))

	got := fx.loadCandidate(candidate.ID)
	assert.Equal(t, model.CandidateStatusAccepted, got.Status)
	assert.Equal(t, ReplacementStageDone, got.ReplacementStage)
	assert.Equal(t, newDownload.ID, *fx.loadLedger().ActiveDownloadID)
	assert.NoFileExists(t, rollback)
}

func TestReplacementRecoveryContinuesOldBackedUpCheckpoint(t *testing.T) {
	fx := newReplacementFixture(t)
	oldPath := fx.writeOldFile("old-content")
	candidate := fx.seedPendingCandidate(oldPath)
	newDownload := fx.seedReplacementDownload(candidate, oldPath)
	staged := filepath.Join(filepath.Dir(oldPath), ".auto-rss-replacements", fmt.Sprint(candidate.ID), "new.mkv")
	rollback := filepath.Join(filepath.Dir(oldPath), ".auto-rss-rollback", fmt.Sprint(candidate.ID), filepath.Base(oldPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(staged), 0o755))
	require.NoError(t, os.WriteFile(staged, []byte("new-content"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Dir(rollback), 0o755))
	require.NoError(t, os.Rename(oldPath, rollback))
	require.NoError(t, fx.db.Model(&candidate).Updates(map[string]any{
		"status":                  model.CandidateStatusReplacing,
		"replacement_stage":       ReplacementStageOldBackedUp,
		"replacement_download_id": newDownload.ID,
		"old_download_id":         fx.old.ID,
		"old_torrent_hash":        fx.old.TorrentHash,
		"old_resource_path":       oldPath,
		"rollback_path":           rollback,
		"final_path":              oldPath,
		"staged_path":             staged,
	}).Error)

	require.NoError(t, fx.service.RecoverIncomplete(context.Background()))

	content, err := os.ReadFile(oldPath)
	require.NoError(t, err)
	assert.Equal(t, "new-content", string(content))
	assert.Equal(t, ReplacementStageDone, fx.loadCandidate(candidate.ID).ReplacementStage)
}

func TestReplacementRecoveryClosesRenameBeforeCheckpointCrashWindows(t *testing.T) {
	tests := []struct {
		name    string
		stage   string
		arrange func(t *testing.T, oldPath, staged, rollback string)
	}{
		{
			name:  "backup rename completed while checkpoint remains staged",
			stage: ReplacementStageStaged,
			arrange: func(t *testing.T, oldPath, staged, rollback string) {
				require.NoError(t, os.MkdirAll(filepath.Dir(staged), 0o755))
				require.NoError(t, os.WriteFile(staged, []byte("new-content"), 0o644))
				require.NoError(t, os.MkdirAll(filepath.Dir(rollback), 0o755))
				require.NoError(t, os.Rename(oldPath, rollback))
			},
		},
		{
			name:  "promote rename completed while checkpoint remains old backed up",
			stage: ReplacementStageOldBackedUp,
			arrange: func(t *testing.T, oldPath, _, rollback string) {
				require.NoError(t, os.MkdirAll(filepath.Dir(rollback), 0o755))
				require.NoError(t, os.Rename(oldPath, rollback))
				require.NoError(t, os.WriteFile(oldPath, []byte("new-content"), 0o644))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReplacementFixture(t)
			oldPath := fx.writeOldFile("old-content")
			candidate := fx.seedPendingCandidate(oldPath)
			newDownload := fx.seedReplacementDownload(candidate, oldPath)
			staged := filepath.Join(filepath.Dir(oldPath), ".auto-rss-replacements", fmt.Sprint(candidate.ID), "new.mkv")
			rollback := filepath.Join(filepath.Dir(oldPath), ".auto-rss-rollback", fmt.Sprint(candidate.ID), filepath.Base(oldPath))
			tt.arrange(t, oldPath, staged, rollback)
			require.NoError(t, fx.db.Model(&candidate).Updates(map[string]any{
				"status": model.CandidateStatusReplacing, "replacement_stage": tt.stage,
				"replacement_download_id": newDownload.ID, "old_download_id": fx.old.ID,
				"old_torrent_hash": fx.old.TorrentHash, "old_resource_path": oldPath,
				"rollback_path": rollback, "final_path": oldPath, "staged_path": staged,
			}).Error)

			require.NoError(t, fx.service.RecoverIncomplete(context.Background()))

			content, err := os.ReadFile(oldPath)
			require.NoError(t, err)
			assert.Equal(t, "new-content", string(content))
			assert.Equal(t, ReplacementStageDone, fx.loadCandidate(candidate.ID).ReplacementStage)
		})
	}
}

func TestReplacementDatabaseSwitchFailureRestoresFilesAndOldTask(t *testing.T) {
	fx := newReplacementFixture(t)
	oldPath := fx.writeOldFile("old-content")
	candidate := fx.seedPendingCandidate(oldPath)
	require.NoError(t, fx.db.Exec(`CREATE TRIGGER fail_replacement_switch
		BEFORE UPDATE OF active_download_id ON subscription_episodes
		WHEN NEW.active_download_id <> OLD.active_download_id
		BEGIN SELECT RAISE(ABORT, 'injected replacement switch failure'); END;`).Error)

	err := fx.service.Replace(context.Background(), candidate.ID)

	require.ErrorContains(t, err, "injected replacement switch failure")
	content, readErr := os.ReadFile(oldPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old-content", string(content))
	assert.Equal(t, fx.old.ID, *fx.loadLedger().ActiveDownloadID)
	assert.Equal(t, model.CandidateStatusFailed, fx.loadCandidate(candidate.ID).Status)
	assert.Contains(t, fx.controller.resumed, fx.old.TorrentHash)
}

func TestReplacementRejectsConcurrentReentryForSameCandidate(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	require.NoError(t, fx.db.Model(&candidate).Updates(map[string]any{"status": model.CandidateStatusReplacing, "replacement_stage": ReplacementStageDownloading}).Error)

	err := fx.service.Replace(context.Background(), candidate.ID)

	require.ErrorIs(t, err, ErrReplacementInProgress)
}

func TestReplacementRejectsStagedPathOutsideCandidateDirectory(t *testing.T) {
	fx := newReplacementFixture(t)
	oldPath := fx.writeOldFile("old-content")
	candidate := fx.seedPendingCandidate(oldPath)
	fx.downloader.stagedOutside = true

	err := fx.service.Replace(context.Background(), candidate.ID)

	require.ErrorContains(t, err, "escapes candidate directory")
	content, readErr := os.ReadFile(oldPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old-content", string(content))
	assert.Equal(t, model.CandidateStatusFailed, fx.loadCandidate(candidate.ID).Status)
	assert.Equal(t, []string{"new-hash"}, fx.downloader.cleanupHashes)
}

func TestReplacementCancellationAfterFileMoveRestoresOldResource(t *testing.T) {
	tests := []struct {
		name       string
		cancelWhen func(source, destination string) bool
	}{
		{name: "after backup", cancelWhen: func(_, destination string) bool { return strings.Contains(destination, ".auto-rss-rollback") }},
		{name: "after promote", cancelWhen: func(source, _ string) bool { return strings.Contains(source, ".auto-rss-replacements") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReplacementFixture(t)
			oldPath := fx.writeOldFile("old-content")
			candidate := fx.seedPendingCandidate(oldPath)
			ctx, cancel := context.WithCancel(context.Background())
			fx.promoter.afterMove = func(source, destination string) {
				if tt.cancelWhen(source, destination) {
					cancel()
				}
			}

			err := fx.service.Replace(ctx, candidate.ID)

			require.ErrorIs(t, err, context.Canceled)
			content, readErr := os.ReadFile(oldPath)
			require.NoError(t, readErr)
			assert.Equal(t, "old-content", string(content))
			assert.Equal(t, fx.old.ID, *fx.loadLedger().ActiveDownloadID)
			assert.Equal(t, model.CandidateStatusFailed, fx.loadCandidate(candidate.ID).Status)
			assert.Contains(t, fx.controller.resumed, fx.old.TorrentHash)
		})
	}
}

func TestOSFilePromoterRejectsSymlinkAndExistingDestination(t *testing.T) {
	root := t.TempDir()
	realSource := filepath.Join(root, "real.mkv")
	require.NoError(t, os.WriteFile(realSource, []byte("source"), 0o644))
	symlinkSource := filepath.Join(root, "link.mkv")
	require.NoError(t, os.Symlink(realSource, symlinkSource))
	promoter := NewOSFilePromoter()

	err := promoter.Move(symlinkSource, filepath.Join(root, "target.mkv"))
	require.ErrorContains(t, err, "symlink")

	existingDestination := filepath.Join(root, "existing.mkv")
	require.NoError(t, os.WriteFile(existingDestination, []byte("keep"), 0o644))
	err = promoter.Move(realSource, existingDestination)
	require.ErrorContains(t, err, "already exists")
	content, readErr := os.ReadFile(existingDestination)
	require.NoError(t, readErr)
	assert.Equal(t, "keep", string(content))
}

func TestQBReplacementDownloaderPayloadCleanupRefusesNormalDownload(t *testing.T) {
	fx := newReplacementFixture(t)
	qb := &replacementQBClient{}
	production := NewQBReplacementDownloader(fx.db, fx.downloads, repository.NewConfigRepository(fx.db), qb, "", fx.root)

	err := production.CleanupFailedDownload(&model.Download{TorrentHash: "old-hash", Purpose: model.DownloadPurposeNormal})

	require.ErrorContains(t, err, "refusing payload cleanup")
	assert.Empty(t, qb.deleted)
	candidate := fx.seedUntrackedMarkedCandidate()
	require.NoError(t, production.CleanupFailedDownload(&model.Download{
		TorrentHash: "new-hash", Purpose: model.DownloadPurposeReplacement, ReplacementCandidateID: &candidate.ID,
	}))
	assert.Equal(t, []string{"new-hash"}, qb.deleted)
	require.NoError(t, fx.db.Model(&candidate).Update("old_torrent_hash", "old-hash").Error)
	err = production.CleanupFailedDownload(&model.Download{
		TorrentHash: "old-hash", Purpose: model.DownloadPurposeReplacement, ReplacementCandidateID: &candidate.ID,
	})
	require.ErrorContains(t, err, "old active torrent")
	assert.Equal(t, []string{"new-hash"}, qb.deleted)
}

func TestQBReplacementDownloaderRecoveryReusesPersistedDownload(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	require.NoError(t, fx.db.Model(&candidate).Updates(map[string]any{
		"status": model.CandidateStatusReplacing, "replacement_stage": ReplacementStageDownloading,
	}).Error)
	candidate.Status = model.CandidateStatusReplacing
	candidate.ReplacementStage = ReplacementStageDownloading
	qb := &replacementQBClient{hash: "new-hash", fileName: "downloaded.mkv", state: downloaderService.StateDownloading}
	production := NewQBReplacementDownloader(fx.db, fx.downloads, repository.NewConfigRepository(fx.db), qb, "", fx.root)
	stagedDir := filepath.Join(filepath.Dir(fx.old.RenamedPath), ".auto-rss-replacements", fmt.Sprint(candidate.ID))
	previousPollInterval := replacementPollInterval
	replacementPollInterval = time.Millisecond
	t.Cleanup(func() { replacementPollInterval = previousPollInterval })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, _, err := production.DownloadToStage(ctx, candidate, stagedDir)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, fx.db.First(&candidate, candidate.ID).Error)
	require.NotNil(t, candidate.ReplacementDownloadID)
	qb.state = downloaderService.StateCompleted

	download, stagedPath, err := production.DownloadToStage(context.Background(), candidate, stagedDir)

	require.NoError(t, err)
	assert.Equal(t, 1, qb.addCalls)
	assert.Equal(t, *candidate.ReplacementDownloadID, download.ID)
	assert.FileExists(t, stagedPath)
}

func TestReplacementRecoveryRetriesCleanupAndIsIdempotent(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	fx.controller.removeErr = errors.New("remove failed")
	require.Error(t, fx.service.Replace(context.Background(), candidate.ID))
	fx.controller.removeErr = nil

	require.NoError(t, fx.service.RecoverIncomplete(context.Background()))
	require.NoError(t, fx.service.RecoverIncomplete(context.Background()))

	got := fx.loadCandidate(candidate.ID)
	assert.Equal(t, model.CandidateStatusAccepted, got.Status)
	assert.Equal(t, ReplacementStageDone, got.ReplacementStage)
}

func TestReplacementRecoveryKeepsUnknownStageForManualInspection(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	require.NoError(t, fx.db.Model(&candidate).Updates(map[string]any{
		"status": model.CandidateStatusReplacing, "replacement_stage": "future_unknown_stage",
	}).Error)

	require.NoError(t, fx.service.RecoverIncomplete(context.Background()))

	got := fx.loadCandidate(candidate.ID)
	assert.Equal(t, model.CandidateStatusReplacing, got.Status)
	assert.Equal(t, "future_unknown_stage", got.ReplacementStage)
	assert.Contains(t, got.FailureReason, "unknown replacement stage")
}

func TestReplacementCanceledBeforeDownloadRemainsRecoverable(t *testing.T) {
	fx := newReplacementFixture(t)
	candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := fx.service.Replace(ctx, candidate.ID)

	require.ErrorIs(t, err, context.Canceled)
	got := fx.loadCandidate(candidate.ID)
	assert.Equal(t, model.CandidateStatusReplacing, got.Status)
	assert.Equal(t, ReplacementStageQueued, got.ReplacementStage)
	assert.Empty(t, fx.events)
}

func (fx *replacementFixture) seedReplacementDownload(candidate model.EpisodeResourceCandidate, finalPath string) model.Download {
	fx.t.Helper()
	download := model.Download{
		SubscriptionID: fx.sub.ID, Episode: 1, Title: candidate.Title,
		TorrentURL: candidate.TorrentURL, TorrentHash: candidate.TorrentHash,
		FilePath: finalPath, RenamedPath: finalPath,
		Status: model.DownloadStatusCompleted, Purpose: model.DownloadPurposeReplacement,
		ReplacementCandidateID: &candidate.ID,
	}
	require.NoError(fx.t, fx.db.Create(&download).Error)
	return download
}

type replacementQBClient struct {
	hash     string
	fileName string
	state    string
	addCalls int
	removed  []string
	deleted  []string
}

func (q *replacementQBClient) Login(string, string, string) error          { return nil }
func (q *replacementQBClient) TestConnection(string, string, string) error { return nil }
func (q *replacementQBClient) AddTorrent(_ string, savePath string, _ string) (string, error) {
	q.addCalls++
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(savePath, q.fileName), []byte("new-content"), 0o644); err != nil {
		return "", err
	}
	return q.hash, nil
}
func (q *replacementQBClient) AddTorrentFile(string, []byte, string, string) (string, error) {
	return "", errors.New("not used")
}
func (q *replacementQBClient) GetTorrentInfo(string) (*downloaderService.TorrentInfo, error) {
	state := q.state
	if state == "" {
		state = downloaderService.StateCompleted
	}
	progress := float64(0)
	if state == downloaderService.StateCompleted {
		progress = 1
	}
	return &downloaderService.TorrentInfo{Hash: q.hash, Name: q.fileName, Progress: progress, State: state}, nil
}
func (q *replacementQBClient) GetTorrentsByCategory(string) ([]*downloaderService.TorrentInfo, error) {
	return nil, nil
}
func (q *replacementQBClient) SetCategory(string, string) error               { return nil }
func (q *replacementQBClient) SetLocation(string, string) error               { return nil }
func (q *replacementQBClient) RenameTorrentFile(string, string, string) error { return nil }
func (q *replacementQBClient) PauseTorrent(string) error                      { return nil }
func (q *replacementQBClient) ResumeTorrent(string) error                     { return nil }
func (q *replacementQBClient) RemoveTorrentTask(hash string) error {
	q.removed = append(q.removed, hash)
	return nil
}
func (q *replacementQBClient) DeleteTorrentWithPayload(hash string) error {
	q.deleted = append(q.deleted, hash)
	return nil
}
func (q *replacementQBClient) GetTorrentFiles(string) ([]downloaderService.TorrentFile, error) {
	return []downloaderService.TorrentFile{{Name: q.fileName, Size: 11, Progress: 1}}, nil
}
func (q *replacementQBClient) GetVersion() (string, error)                { return "", nil }
func (q *replacementQBClient) SetProxy(string) error                      { return nil }
func (q *replacementQBClient) DownloadTorrentFile(string) ([]byte, error) { return nil, nil }
