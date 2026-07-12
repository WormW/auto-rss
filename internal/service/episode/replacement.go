package episode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	qbclient "github.com/WormW/auto-rss/internal/qbittorrent"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

const (
	ReplacementStageQueued          = "queued"
	ReplacementStageDownloading     = "downloading"
	ReplacementStageDownloadCleanup = "download_cleanup"
	ReplacementStageTerminalCleanup = "terminal_cleanup"
	ReplacementStageDetaching       = "detaching"
	ReplacementStageStaged          = "staged"
	ReplacementStageOldBackedUp     = "old_backed_up"
	ReplacementStagePromoted        = "promoted"
	ReplacementStageSwitched        = "switched"
	ReplacementStageCleaning        = "cleaning"
	ReplacementStageDone            = "done"
)

var ErrReplacementInProgress = repository.ErrReplacementInProgress

type ReplacementDownloader interface {
	DownloadToStage(ctx context.Context, candidate model.EpisodeResourceCandidate, stagedDir string) (*model.Download, string, error)
	CleanupFailedDownload(download *model.Download) error
}

type TorrentTaskController interface {
	GetTorrentInfo(hash string) (*qbclient.TorrentInfo, error)
	PauseTorrent(hash string) error
	ResumeTorrent(hash string) error
	RemoveTorrentTask(hash string) error
}

type FilePromoter interface {
	Move(source, destination string) error
	Remove(path string) error
	Exists(path string) bool
}

type osFilePromoter struct{}

func NewOSFilePromoter() FilePromoter { return osFilePromoter{} }

func (osFilePromoter) Move(source, destination string) error {
	if err := validateRenameEndpoint(source, true); err != nil {
		return err
	}
	if err := validateRenameEndpoint(destination, false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("atomic rename %q to %q: %w", source, destination, err)
	}
	return nil
}

func (osFilePromoter) Remove(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := validateRenameEndpoint(path, false); err != nil {
		return err
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (osFilePromoter) Exists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func validateRenameEndpoint(path string, mustExist bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("unsafe replacement path: %q", path)
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		// macOS exposes /var and /tmp as system compatibility symlinks. They are
		// trusted roots; candidate-controlled symlinks below them are not.
		trustedSystemLink := current == "/var" || current == "/tmp"
		if err == nil && info.Mode()&os.ModeSymlink != 0 && !trustedSystemLink {
			return fmt.Errorf("symlink is not allowed in replacement path: %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
	}
	if mustExist {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("replacement source is not a regular file: %s", path)
		}
	}
	return nil
}

type ReplacementService struct {
	recoveryMu    sync.Mutex
	db            *gorm.DB
	episodes      repository.EpisodeRepository
	downloads     repository.DownloadRepository
	subscriptions repository.SubscriptionRepository
	downloader    ReplacementDownloader
	torrents      TorrentTaskController
	files         FilePromoter
}

func NewReplacementService(
	db *gorm.DB,
	episodeRepo repository.EpisodeRepository,
	downloadRepo repository.DownloadRepository,
	subscriptionRepo repository.SubscriptionRepository,
	downloader ReplacementDownloader,
	torrentController TorrentTaskController,
	filePromoter FilePromoter,
) *ReplacementService {
	return &ReplacementService{db: db, episodes: episodeRepo, downloads: downloadRepo, subscriptions: subscriptionRepo, downloader: downloader, torrents: torrentController, files: filePromoter}
}

func (s *ReplacementService) Replace(ctx context.Context, candidateID uint) error {
	if err := s.validate(); err != nil {
		return err
	}
	candidate, err := s.episodes.ClaimCandidateForReplacement(candidateID)
	if err != nil {
		return err
	}
	return s.advance(ctx, candidate)
}

func (s *ReplacementService) RetryCleanup(ctx context.Context, candidateID uint) error {
	if err := s.validate(); err != nil {
		return err
	}
	candidate, err := s.episodes.GetCandidateByID(candidateID)
	if err != nil {
		return err
	}
	if candidate.Status != model.CandidateStatusAcceptedCleanupFailed {
		return fmt.Errorf("%w: candidate %d has status %s", repository.ErrCandidateStateConflict, candidateID, candidate.Status)
	}
	return s.cleanup(ctx, candidate)
}

func (s *ReplacementService) RecoverIncomplete(ctx context.Context) error {
	_, err := s.RecoverIncompleteWithCount(ctx)
	return err
}

func (s *ReplacementService) RecoverIncompleteWithCount(ctx context.Context) (int, error) {
	if err := s.validate(); err != nil {
		return 0, err
	}
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	candidates, err := s.episodes.ListIncompleteReplacements()
	if err != nil {
		return 0, err
	}
	scannedCount := len(candidates)
	errCh := make(chan error, len(candidates))
	var activeDownloads sync.WaitGroup
	advanceCandidate := func(candidate model.EpisodeResourceCandidate) {
		if err := s.advance(ctx, &candidate); err != nil {
			errCh <- fmt.Errorf("candidate %d: %w", candidate.ID, err)
		}
	}
	for i := range candidates {
		if err := ctx.Err(); err != nil {
			errCh <- err
			break
		}
		candidate := candidates[i]
		if candidate.Status == model.CandidateStatusReplacing &&
			(candidate.ReplacementStage == "" || candidate.ReplacementStage == ReplacementStageQueued || candidate.ReplacementStage == ReplacementStageDownloading) {
			activeDownloads.Add(1)
			go func() {
				defer activeDownloads.Done()
				advanceCandidate(candidate)
			}()
			continue
		}
		advanceCandidate(candidate)
	}
	activeDownloads.Wait()
	close(errCh)
	var recoveryErrors []error
	for err := range errCh {
		recoveryErrors = append(recoveryErrors, err)
	}
	return scannedCount, errors.Join(recoveryErrors...)
}

func (s *ReplacementService) validate() error {
	if s == nil || s.db == nil || s.episodes == nil || s.downloads == nil || s.downloader == nil || s.torrents == nil || s.files == nil {
		return errors.New("replacement service dependencies are required")
	}
	return nil
}

func (s *ReplacementService) advance(ctx context.Context, candidate *model.EpisodeResourceCandidate) error {
	if candidate == nil {
		return errors.New("replacement candidate is required")
	}
	if candidate.Status == model.CandidateStatusAcceptedCleanupFailed {
		if candidate.ReplacementStage == ReplacementStageSwitched || candidate.ReplacementStage == ReplacementStageCleaning {
			return s.cleanup(ctx, candidate)
		}
		return s.recordUnknownStage(candidate)
	}
	if candidate.Status == model.CandidateStatusReplacing && candidate.ReplacementStage == ReplacementStageTerminalCleanup {
		return s.cleanupTerminalFailure(candidate)
	}
	_, oldDownload, err := s.loadResources(candidate)
	if err != nil {
		return s.recordFailure(candidate, err)
	}

	switch candidate.ReplacementStage {
	case ReplacementStageQueued, ReplacementStageDownloading, "":
		if err := ctx.Err(); err != nil {
			return err
		}
		finalPath := strings.TrimSpace(candidate.FinalPath)
		if oldDownload != nil {
			finalPath = replacementDownloadPath(oldDownload)
			oldID := oldDownload.ID
			candidate.OldDownloadID = &oldID
			candidate.OldTorrentHash = oldDownload.TorrentHash
			candidate.OldResourcePath = finalPath
			candidate.RollbackPath = filepath.Join(filepath.Dir(finalPath), ".auto-rss-rollback", fmt.Sprint(candidate.ID), filepath.Base(finalPath))
		}
		candidate.FinalPath = finalPath
		candidate.ReplacementStage = ReplacementStageDownloading
		candidate.FailureReason = ""
		if err := s.episodes.UpdateCandidate(candidate); err != nil {
			return err
		}
		if finalPath != "" {
			if err := validateRenameEndpoint(finalPath, false); err != nil {
				return s.recordFailure(candidate, err)
			}
		}
		stagedDir := ""
		if finalPath != "" {
			stagedDir = filepath.Join(filepath.Dir(finalPath), ".auto-rss-replacements", fmt.Sprint(candidate.ID))
		}
		newDownload, stagedPath, downloadErr := s.downloader.DownloadToStage(ctx, *candidate, stagedDir)
		if downloadErr != nil {
			if newDownload != nil {
				if newDownload.ID != 0 {
					candidate.ReplacementDownloadID = &newDownload.ID
				}
			}
			fresh, freshErr := s.episodes.GetCandidateByID(candidate.ID)
			if freshErr == nil {
				*candidate = *fresh
			}
			if errors.Is(downloadErr, ErrReplacementOwnershipUnknown) || errors.Is(downloadErr, ErrReplacementCheckpointPending) {
				if freshErr != nil {
					return errors.Join(downloadErr, freshErr)
				}
				if candidate.Status != model.CandidateStatusReplacing || candidate.ReplacementStage != ReplacementStageDownloading {
					return downloadErr
				}
				candidate.Status = model.CandidateStatusReplacing
				candidate.FailureReason = downloadErr.Error()
				if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
					return errors.Join(downloadErr, saveErr)
				}
				return downloadErr
			}
			if freshErr == nil && (fresh.ReplacementStage == ReplacementStageDetaching || fresh.ReplacementStage == ReplacementStageStaged) && fresh.StagedPath != "" {
				fresh.FailureReason = downloadErr.Error()
				if saveErr := s.episodes.UpdateCandidate(fresh); saveErr != nil {
					return errors.Join(downloadErr, saveErr)
				}
				return downloadErr
			}
			cleanupErr := s.downloader.CleanupFailedDownload(newDownload)
			if cleanupErr != nil {
				candidate.Status = model.CandidateStatusReplacing
				candidate.ReplacementStage = ReplacementStageDownloadCleanup
				candidate.FailureReason = errors.Join(downloadErr, cleanupErr).Error()
				if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
					return errors.Join(downloadErr, cleanupErr, saveErr)
				}
				return errors.Join(downloadErr, cleanupErr)
			}
			if err := s.discardFailedReplacementDownload(candidate, newDownload); err != nil {
				return errors.Join(downloadErr, err)
			}
			return s.recordFailure(candidate, downloadErr)
		}
		if newDownload == nil || newDownload.ID == 0 {
			return s.recordFailure(candidate, errors.New("replacement downloader returned an unpersisted download"))
		}
		if finalPath == "" {
			finalPath = replacementDownloadPath(newDownload)
		}
		if finalPath == "" {
			return s.failDownloadedCandidate(candidate, newDownload, errors.New("replacement final path is unavailable"))
		}
		if err := validateRenameEndpoint(finalPath, false); err != nil {
			return s.failDownloadedCandidate(candidate, newDownload, err)
		}
		if stagedDir == "" {
			stagedDir = filepath.Join(filepath.Dir(finalPath), ".auto-rss-replacements", fmt.Sprint(candidate.ID))
		}
		if err := validateStagedPath(stagedPath, stagedDir); err != nil {
			return s.failDownloadedCandidate(candidate, newDownload, err)
		}
		candidate.ReplacementDownloadID = &newDownload.ID
		candidate.StagedPath = stagedPath
		candidate.FinalPath = finalPath
		candidate.ReplacementStage = ReplacementStageStaged
		if err := s.episodes.UpdateCandidate(candidate); err != nil {
			return err
		}
		return s.advance(ctx, candidate)
	case ReplacementStageDownloadCleanup:
		if candidate.ReplacementDownloadID == nil {
			return s.recordUnknownStage(candidate)
		}
		download, err := s.downloads.GetByID(*candidate.ReplacementDownloadID)
		if err != nil {
			return err
		}
		cause := errors.New(candidate.FailureReason)
		if err := s.downloader.CleanupFailedDownload(download); err != nil {
			candidate.FailureReason = errors.Join(cause, err).Error()
			if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			return err
		}
		if err := s.discardFailedReplacementDownload(candidate, download); err != nil {
			return errors.Join(cause, err)
		}
		return s.recordFailure(candidate, cause)
	case ReplacementStageDetaching:
		if err := ctx.Err(); err != nil {
			return err
		}
		if candidate.ReplacementDownloadID == nil {
			return s.recordUnknownStage(candidate)
		}
		newDownload, err := s.downloads.GetByID(*candidate.ReplacementDownloadID)
		if err != nil {
			return err
		}
		if newDownload.ReplacementTorrentOwned {
			info, infoErr := s.torrents.GetTorrentInfo(newDownload.TorrentHash)
			if errors.Is(infoErr, qbclient.ErrTorrentNotFound) {
				newDownload.ReplacementTorrentOwned = false
			} else if infoErr != nil {
				err := fmt.Errorf("verify detaching torrent ownership: %w", infoErr)
				candidate.FailureReason = err.Error()
				if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
					return errors.Join(err, saveErr)
				}
				return err
			} else if err := validateDetachingTorrentOwnership(candidate, newDownload, info); err != nil {
				candidate.FailureReason = err.Error()
				if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
					return errors.Join(err, saveErr)
				}
				return err
			} else if err := s.torrents.RemoveTorrentTask(newDownload.TorrentHash); err != nil {
				candidate.FailureReason = err.Error()
				if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
					return errors.Join(err, saveErr)
				}
				return err
			} else {
				newDownload.ReplacementTorrentOwned = false
			}
		}
		newDownload.Status = model.DownloadStatusCompleted
		if err := s.downloads.Update(newDownload); err != nil {
			return err
		}
		candidate.ReplacementStage = ReplacementStageStaged
		candidate.FailureReason = ""
		if err := s.episodes.UpdateCandidate(candidate); err != nil {
			return err
		}
		return s.advance(ctx, candidate)
	case ReplacementStageStaged:
		if err := ctx.Err(); err != nil {
			return err
		}
		if candidate.OldResourcePath != "" {
			if candidate.OldTorrentHash != "" {
				if err := s.torrents.PauseTorrent(candidate.OldTorrentHash); err != nil {
					return s.recordFailure(candidate, fmt.Errorf("pause old torrent: %w", err))
				}
			}
			oldExists := s.files.Exists(candidate.OldResourcePath)
			rollbackExists := s.files.Exists(candidate.RollbackPath)
			if oldExists && rollbackExists {
				err := errors.New("both old and rollback resources exist at staged checkpoint")
				candidate.FailureReason = err.Error()
				if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
					return errors.Join(err, saveErr)
				}
				return err
			}
			if !oldExists && !rollbackExists {
				return s.failAndResume(candidate, errors.New("old resource and rollback files are missing"))
			}
			if oldExists {
				if err := s.files.Move(candidate.OldResourcePath, candidate.RollbackPath); err != nil {
					return s.failAndResume(candidate, fmt.Errorf("backup old resource: %w", err))
				}
			}
			candidate.ReplacementStage = ReplacementStageOldBackedUp
			if err := s.episodes.UpdateCandidate(candidate); err != nil {
				_ = s.files.Move(candidate.RollbackPath, candidate.OldResourcePath)
				return s.failAndResume(candidate, err)
			}
		}
		fallthrough
	case ReplacementStageOldBackedUp:
		if err := ctx.Err(); err != nil {
			if candidate.RollbackPath != "" && s.files.Exists(candidate.RollbackPath) && !s.files.Exists(candidate.FinalPath) {
				_ = s.files.Move(candidate.RollbackPath, candidate.FinalPath)
			}
			return s.failAndResume(candidate, err)
		}
		stagedExists := s.files.Exists(candidate.StagedPath)
		finalExists := s.files.Exists(candidate.FinalPath)
		if finalExists && !stagedExists {
			candidate.ReplacementStage = ReplacementStagePromoted
			if err := s.episodes.UpdateCandidate(candidate); err != nil {
				return err
			}
			return s.finishPromoted(ctx, candidate)
		}
		if finalExists && stagedExists {
			err := errors.New("both staged and final resources exist at old_backed_up checkpoint")
			candidate.FailureReason = err.Error()
			if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			return err
		}
		if !stagedExists {
			if candidate.RollbackPath != "" && s.files.Exists(candidate.RollbackPath) && !s.files.Exists(candidate.FinalPath) {
				_ = s.files.Move(candidate.RollbackPath, candidate.FinalPath)
			}
			return s.failAndResume(candidate, errors.New("staged replacement file is missing"))
		}
		if err := s.files.Move(candidate.StagedPath, candidate.FinalPath); err != nil {
			if candidate.RollbackPath != "" && s.files.Exists(candidate.RollbackPath) && !s.files.Exists(candidate.FinalPath) {
				_ = s.files.Move(candidate.RollbackPath, candidate.FinalPath)
			}
			return s.failAndResume(candidate, fmt.Errorf("promote replacement: %w", err))
		}
		candidate.ReplacementStage = ReplacementStagePromoted
		if err := s.episodes.UpdateCandidate(candidate); err != nil {
			return s.compensateSwitchFailure(candidate, err)
		}
		fallthrough
	case ReplacementStagePromoted:
		return s.finishPromoted(ctx, candidate)
	case ReplacementStageDone:
		return nil
	default:
		return s.recordUnknownStage(candidate)
	}
}

func validateDetachingTorrentOwnership(candidate *model.EpisodeResourceCandidate, download *model.Download, info *qbclient.TorrentInfo) error {
	if candidate == nil || download == nil || info == nil {
		return errors.New("detaching torrent ownership information is missing")
	}
	expectedCategory := replacementTorrentCategoryPrefix + fmt.Sprint(candidate.ID)
	if !strings.EqualFold(strings.TrimSpace(info.Hash), strings.TrimSpace(download.TorrentHash)) ||
		info.Category != expectedCategory || !sameReplacementSavePath(info.SavePath, filepath.Dir(candidate.StagedPath)) {
		return errors.New("detaching torrent ownership hash/category/save path changed")
	}
	return nil
}

func (s *ReplacementService) failDownloadedCandidate(candidate *model.EpisodeResourceCandidate, download *model.Download, cause error) error {
	if download != nil && download.ID != 0 {
		candidate.ReplacementDownloadID = &download.ID
	}
	if cleanupErr := s.downloader.CleanupFailedDownload(download); cleanupErr != nil {
		candidate.Status = model.CandidateStatusReplacing
		candidate.ReplacementStage = ReplacementStageDownloadCleanup
		candidate.FailureReason = errors.Join(cause, cleanupErr).Error()
		if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
			return errors.Join(cause, cleanupErr, saveErr)
		}
		return errors.Join(cause, cleanupErr)
	}
	if err := s.discardFailedReplacementDownload(candidate, download); err != nil {
		return errors.Join(cause, err)
	}
	return s.recordFailure(candidate, cause)
}

func (s *ReplacementService) discardFailedReplacementDownload(candidate *model.EpisodeResourceCandidate, download *model.Download) error {
	if candidate == nil || download == nil || download.ID == 0 {
		return nil
	}
	if download.Purpose != model.DownloadPurposeReplacement || download.ReplacementCandidateID == nil || *download.ReplacementCandidateID != candidate.ID {
		return errors.New("refusing to discard unrelated replacement download")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.EpisodeResourceCandidate{}).
			Where("id = ? AND replacement_download_id = ?", candidate.ID, download.ID).
			Update("replacement_download_id", nil).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND purpose = ? AND replacement_candidate_id = ?", download.ID, model.DownloadPurposeReplacement, candidate.ID).
			Delete(&model.Download{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("replacement download changed before discard")
		}
		return nil
	})
	if err == nil {
		candidate.ReplacementDownloadID = nil
	}
	return err
}

func (s *ReplacementService) recordUnknownStage(candidate *model.EpisodeResourceCandidate) error {
	err := fmt.Errorf("unknown replacement stage: %s", candidate.ReplacementStage)
	candidate.FailureReason = err.Error()
	if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	return err
}

func (s *ReplacementService) finishPromoted(ctx context.Context, candidate *model.EpisodeResourceCandidate) error {
	if err := ctx.Err(); err != nil {
		return s.compensateSwitchFailure(candidate, err)
	}
	if err := s.switchDatabase(candidate); err != nil {
		return s.compensateSwitchFailure(candidate, err)
	}
	return s.cleanup(ctx, candidate)
}

func (s *ReplacementService) loadResources(candidate *model.EpisodeResourceCandidate) (*model.SubscriptionEpisode, *model.Download, error) {
	var ledger model.SubscriptionEpisode
	if err := s.db.First(&ledger, candidate.SubscriptionEpisodeID).Error; err != nil {
		return nil, nil, err
	}
	var old *model.Download
	oldID := ledger.ActiveDownloadID
	if candidate.OldDownloadID != nil {
		oldID = candidate.OldDownloadID
	}
	if oldID != nil {
		download, err := s.downloads.GetByID(*oldID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		if err == nil {
			old = download
		}
	}
	return &ledger, old, nil
}

func replacementDownloadPath(download *model.Download) string {
	if download == nil {
		return ""
	}
	if path := strings.TrimSpace(download.RenamedPath); path != "" {
		return path
	}
	return strings.TrimSpace(download.FilePath)
}

func validateStagedPath(path, stagedDir string) error {
	if err := validateRenameEndpoint(path, true); err != nil {
		return err
	}
	relative, err := filepath.Rel(stagedDir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("staged path escapes candidate directory: %s", path)
	}
	return nil
}

func (s *ReplacementService) switchDatabase(candidate *model.EpisodeResourceCandidate) error {
	if candidate.ReplacementDownloadID == nil {
		return errors.New("replacement download is missing")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var current model.EpisodeResourceCandidate
		if err := tx.First(&current, candidate.ID).Error; err != nil {
			return err
		}
		if current.Status == model.CandidateStatusAcceptedCleanupFailed && (current.ReplacementStage == ReplacementStageSwitched || current.ReplacementStage == ReplacementStageCleaning) {
			return nil
		}
		if current.Status != model.CandidateStatusReplacing || current.ReplacementStage != ReplacementStagePromoted {
			return fmt.Errorf("candidate replacement checkpoint changed")
		}
		var ledger model.SubscriptionEpisode
		if err := tx.First(&ledger, candidate.SubscriptionEpisodeID).Error; err != nil {
			return err
		}

		var download model.Download
		if err := tx.First(&download, *candidate.ReplacementDownloadID).Error; err != nil {
			return err
		}
		now := time.Now()
		download.Status = model.DownloadStatusCompleted
		download.ReplacementTorrentOwned = false
		download.FilePath = candidate.FinalPath
		download.RenamedPath = candidate.FinalPath
		download.DownloadedAt = &now
		if err := tx.Save(&download).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"status": model.EpisodeStatusDownloaded, "status_source": model.EpisodeStatusSourceUser,
			"active_download_id": download.ID, "active_torrent_hash": download.TorrentHash,
			"active_torrent_url": download.TorrentURL, "active_title": download.Title, "downloaded_at": now,
		}
		ledgerUpdate := tx.Model(&model.SubscriptionEpisode{}).Where("id = ? AND status IN ?", candidate.SubscriptionEpisodeID, []string{model.EpisodeStatusDownloaded, model.EpisodeStatusMarkedDownloaded})
		if candidate.OldDownloadID == nil {
			ledgerUpdate = ledgerUpdate.Where("active_download_id IS NULL")
		} else {
			ledgerUpdate = ledgerUpdate.Where("active_download_id = ?", *candidate.OldDownloadID)
		}
		result := ledgerUpdate.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("episode active resource changed during replacement")
		}

		result = tx.Model(&model.EpisodeResourceCandidate{}).
			Where("id = ? AND status = ? AND replacement_stage = ?", candidate.ID, model.CandidateStatusReplacing, ReplacementStagePromoted).
			Updates(map[string]any{"status": model.CandidateStatusAcceptedCleanupFailed, "replacement_stage": ReplacementStageSwitched, "failure_reason": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("candidate replacement checkpoint changed")
		}
		return repository.NewEpisodeRepository(tx).RefreshSubscriptionProgressInTx(tx, ledger.SubscriptionID)
	})
}

func (s *ReplacementService) cleanup(ctx context.Context, candidate *model.EpisodeResourceCandidate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fresh, err := s.episodes.GetCandidateByID(candidate.ID)
	if err != nil {
		return err
	}
	*candidate = *fresh
	if candidate.ReplacementStage == ReplacementStageDone && candidate.Status == model.CandidateStatusAccepted {
		return nil
	}
	if candidate.Status != model.CandidateStatusAcceptedCleanupFailed ||
		(candidate.ReplacementStage != ReplacementStageSwitched && candidate.ReplacementStage != ReplacementStageCleaning) {
		return fmt.Errorf("%w: candidate %d cannot retry cleanup from status %s stage %s", repository.ErrCandidateStateConflict, candidate.ID, candidate.Status, candidate.ReplacementStage)
	}
	candidate.Status = model.CandidateStatusAcceptedCleanupFailed
	candidate.ReplacementStage = ReplacementStageCleaning
	if err := s.episodes.UpdateCandidate(candidate); err != nil {
		return err
	}
	if candidate.OldTorrentHash != "" {
		if err := s.torrents.RemoveTorrentTask(candidate.OldTorrentHash); err != nil {
			return s.cleanupFailure(candidate, err)
		}
	}
	if candidate.RollbackPath != "" {
		if err := s.files.Remove(candidate.RollbackPath); err != nil {
			return s.cleanupFailure(candidate, err)
		}
	}
	if candidate.OldDownloadID != nil {
		if err := s.downloads.Delete(*candidate.OldDownloadID); err != nil {
			return s.cleanupFailure(candidate, err)
		}
	}
	candidate.Status = model.CandidateStatusAccepted
	candidate.ReplacementStage = ReplacementStageDone
	candidate.FailureReason = ""
	return s.episodes.UpdateCandidate(candidate)
}

func (s *ReplacementService) cleanupFailure(candidate *model.EpisodeResourceCandidate, err error) error {
	candidate.Status = model.CandidateStatusAcceptedCleanupFailed
	candidate.ReplacementStage = ReplacementStageCleaning
	candidate.FailureReason = err.Error()
	if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	return err
}

func (s *ReplacementService) failAndResume(candidate *model.EpisodeResourceCandidate, err error) error {
	var compensation []error
	oldRestored := candidate.OldResourcePath == "" || s.files.Exists(candidate.OldResourcePath)
	if !oldRestored && candidate.RollbackPath != "" && s.files.Exists(candidate.RollbackPath) {
		if restoreErr := s.files.Move(candidate.RollbackPath, candidate.OldResourcePath); restoreErr != nil {
			compensation = append(compensation, fmt.Errorf("restore old resource: %w", restoreErr))
		} else {
			oldRestored = true
		}
	}
	if oldRestored && candidate.OldTorrentHash != "" {
		if resumeErr := s.torrents.ResumeTorrent(candidate.OldTorrentHash); resumeErr != nil {
			compensation = append(compensation, fmt.Errorf("resume old torrent: %w", resumeErr))
		}
	}
	if !oldRestored && len(compensation) == 0 {
		compensation = append(compensation, errors.New("old resource restoration is incomplete"))
	}
	joined := errors.Join(append([]error{err}, compensation...)...)
	if len(compensation) == 0 {
		return s.recordFailure(candidate, joined)
	}
	candidate.Status = model.CandidateStatusReplacing
	if oldRestored {
		candidate.ReplacementStage = ReplacementStageStaged
	} else {
		candidate.ReplacementStage = ReplacementStageOldBackedUp
	}
	candidate.FailureReason = joined.Error()
	if saveErr := s.episodes.UpdateCandidate(candidate); saveErr != nil {
		return errors.Join(joined, saveErr)
	}
	return joined
}

func (s *ReplacementService) compensateSwitchFailure(candidate *model.EpisodeResourceCandidate, cause error) error {
	var compensation []error
	newRestored := !s.files.Exists(candidate.FinalPath)
	if s.files.Exists(candidate.FinalPath) && !s.files.Exists(candidate.StagedPath) {
		if err := s.files.Move(candidate.FinalPath, candidate.StagedPath); err != nil {
			compensation = append(compensation, err)
		} else {
			newRestored = true
		}
	}
	oldRestored := candidate.RollbackPath == "" || (newRestored && s.files.Exists(candidate.OldResourcePath))
	if newRestored && candidate.RollbackPath != "" && s.files.Exists(candidate.RollbackPath) && !s.files.Exists(candidate.OldResourcePath) {
		if err := s.files.Move(candidate.RollbackPath, candidate.OldResourcePath); err != nil {
			compensation = append(compensation, err)
		} else {
			oldRestored = true
		}
	}
	resumeFailed := false
	if newRestored && oldRestored && candidate.OldTorrentHash != "" {
		if err := s.torrents.ResumeTorrent(candidate.OldTorrentHash); err != nil {
			compensation = append(compensation, err)
			resumeFailed = true
		}
	}
	joined := errors.Join(append([]error{cause}, compensation...)...)
	if len(compensation) == 0 && newRestored && oldRestored {
		return s.recordFailure(candidate, joined)
	}
	candidate.Status = model.CandidateStatusReplacing
	if !newRestored {
		candidate.ReplacementStage = ReplacementStagePromoted
	} else if !oldRestored {
		candidate.ReplacementStage = ReplacementStageOldBackedUp
	} else if resumeFailed {
		candidate.ReplacementStage = ReplacementStageStaged
	} else {
		candidate.ReplacementStage = ReplacementStageStaged
	}
	candidate.FailureReason = joined.Error()
	if err := s.episodes.UpdateCandidate(candidate); err != nil {
		return errors.Join(joined, err)
	}
	return joined
}

func (s *ReplacementService) recordFailure(candidate *model.EpisodeResourceCandidate, cause error) error {
	if candidate == nil {
		return errors.Join(cause, errors.New("replacement candidate is required"))
	}
	if (candidate.OldResourcePath != "" && !s.files.Exists(candidate.OldResourcePath)) ||
		(candidate.RollbackPath != "" && s.files.Exists(candidate.RollbackPath)) {
		checkpointErr := errors.New("terminal failure cleanup requires the old resource restored at final path")
		candidate.Status = model.CandidateStatusReplacing
		candidate.FailureReason = errors.Join(cause, checkpointErr).Error()
		if err := s.episodes.UpdateCandidate(candidate); err != nil {
			return errors.Join(cause, checkpointErr, err)
		}
		return errors.Join(cause, checkpointErr)
	}
	candidate.Status = model.CandidateStatusReplacing
	candidate.ReplacementStage = ReplacementStageTerminalCleanup
	candidate.FailureReason = cause.Error()
	if err := s.episodes.UpdateCandidate(candidate); err != nil {
		return errors.Join(cause, err)
	}
	return s.cleanupTerminalFailureWithCause(candidate, cause)
}

func (s *ReplacementService) cleanupTerminalFailure(candidate *model.EpisodeResourceCandidate) error {
	cause := errors.New(candidate.FailureReason)
	if strings.TrimSpace(candidate.FailureReason) == "" {
		cause = errors.New("replacement failed before terminal cleanup")
	}
	return s.cleanupTerminalFailureWithCause(candidate, cause)
}

func (s *ReplacementService) cleanupTerminalFailureWithCause(candidate *model.EpisodeResourceCandidate, cause error) error {
	if candidate.Status != model.CandidateStatusReplacing || candidate.ReplacementStage != ReplacementStageTerminalCleanup {
		return errors.Join(cause, errors.New("terminal failure cleanup checkpoint changed"))
	}
	if (candidate.OldResourcePath != "" && !s.files.Exists(candidate.OldResourcePath)) ||
		(candidate.RollbackPath != "" && s.files.Exists(candidate.RollbackPath)) {
		return s.persistTerminalCleanupFailure(candidate, cause, errors.New("terminal cleanup old resource checkpoint is no longer valid"))
	}
	var replacementDownload *model.Download
	if candidate.ReplacementDownloadID != nil {
		download, err := s.downloads.GetByID(*candidate.ReplacementDownloadID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			candidate.ReplacementDownloadID = nil
		} else if err != nil {
			return s.persistTerminalCleanupFailure(candidate, cause, fmt.Errorf("load unadopted replacement download: %w", err))
		} else {
			replacementDownload = download
			if err := s.resolveTerminalTorrentOwnership(candidate, download); err != nil {
				return s.persistTerminalCleanupFailure(candidate, cause, err)
			}
		}
	}
	if candidate.StagedPath != "" && s.files.Exists(candidate.StagedPath) {
		if err := s.files.Remove(candidate.StagedPath); err != nil {
			return s.persistTerminalCleanupFailure(candidate, cause, fmt.Errorf("remove unadopted staged resource: %w", err))
		}
	}
	if replacementDownload != nil {
		if err := s.discardFailedReplacementDownload(candidate, replacementDownload); err != nil {
			return s.persistTerminalCleanupFailure(candidate, cause, fmt.Errorf("discard unadopted replacement download: %w", err))
		}
	}
	candidate.Status = model.CandidateStatusFailed
	candidate.ReplacementStage = ""
	candidate.ReplacementDownloadID = nil
	candidate.StagedPath = ""
	candidate.OldDownloadID = nil
	candidate.OldTorrentHash = ""
	candidate.OldResourcePath = ""
	candidate.RollbackPath = ""
	candidate.FailureReason = cause.Error()
	if err := s.episodes.UpdateCandidate(candidate); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (s *ReplacementService) resolveTerminalTorrentOwnership(candidate *model.EpisodeResourceCandidate, download *model.Download) error {
	if download == nil || !download.ReplacementTorrentOwned {
		return nil
	}
	info, err := s.torrents.GetTorrentInfo(download.TorrentHash)
	if errors.Is(err, qbclient.ErrTorrentNotFound) {
		download.ReplacementTorrentOwned = false
		if updateErr := s.downloads.Update(download); updateErr != nil {
			return fmt.Errorf("persist missing terminal torrent ownership: %w", updateErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify terminal torrent ownership: %w", err)
	}
	if err := validateDetachingTorrentOwnership(candidate, download, info); err != nil {
		return fmt.Errorf("verify terminal torrent ownership: %w", err)
	}
	if err := s.torrents.RemoveTorrentTask(download.TorrentHash); err != nil {
		return fmt.Errorf("remove terminal replacement torrent task: %w", err)
	}
	download.ReplacementTorrentOwned = false
	if err := s.downloads.Update(download); err != nil {
		return fmt.Errorf("persist terminal torrent ownership: %w", err)
	}
	return nil
}

func (s *ReplacementService) persistTerminalCleanupFailure(candidate *model.EpisodeResourceCandidate, cause, cleanupErr error) error {
	joined := errors.Join(cause, cleanupErr)
	candidate.Status = model.CandidateStatusReplacing
	candidate.ReplacementStage = ReplacementStageTerminalCleanup
	candidate.FailureReason = joined.Error()
	if err := s.episodes.UpdateCandidate(candidate); err != nil {
		return errors.Join(joined, err)
	}
	return joined
}
