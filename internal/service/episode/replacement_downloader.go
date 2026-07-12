package episode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	qbclient "github.com/WormW/auto-rss/internal/qbittorrent"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

const replacementTorrentCategoryPrefix = "AutoRss:replacement:"

var replacementPollInterval = 5 * time.Second

type qbReplacementDownloader struct {
	db             *gorm.DB
	downloads      repository.DownloadRepository
	configs        repository.ConfigRepository
	qb             qbclient.Client
	renameTemplate string
	downloadRoot   string
}

func NewQBReplacementDownloader(
	db *gorm.DB,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	qbClient qbclient.Client,
	renameTemplate string,
	downloadRoot string,
) ReplacementDownloader {
	return &qbReplacementDownloader{
		db: db, downloads: downloadRepo, configs: configRepo, qb: qbClient,
		renameTemplate: renameTemplate, downloadRoot: filepath.Clean(downloadRoot),
	}
}

func (d *qbReplacementDownloader) DownloadToStage(ctx context.Context, candidate model.EpisodeResourceCandidate, stagedDir string) (*model.Download, string, error) {
	if d == nil || d.db == nil || d.downloads == nil || d.qb == nil {
		return nil, "", errors.New("replacement downloader dependencies are required")
	}
	if candidate.ID == 0 || strings.TrimSpace(candidate.TorrentURL) == "" {
		return nil, "", errors.New("persisted replacement candidate with torrent URL is required")
	}
	root, err := filepath.Abs(d.downloadRoot)
	if err != nil || strings.TrimSpace(d.downloadRoot) == "" {
		return nil, "", errors.New("replacement download root is invalid")
	}
	if err := validateRenameEndpoint(root, false); err != nil {
		return nil, "", err
	}

	var ledger model.SubscriptionEpisode
	if err := d.db.First(&ledger, candidate.SubscriptionEpisodeID).Error; err != nil {
		return nil, "", err
	}
	var subscription model.Subscription
	if err := d.db.First(&subscription, ledger.SubscriptionID).Error; err != nil {
		return nil, "", err
	}
	originalEpisode := ledger.Episode + max(subscription.EpisodeOffset, 0)
	queuedHash := fmt.Sprintf("replacement:%d:queued", candidate.ID)
	var download *model.Download
	if candidate.ReplacementDownloadID != nil {
		download, err = d.downloads.GetByID(*candidate.ReplacementDownloadID)
		if err != nil {
			return nil, "", fmt.Errorf("load replacement download checkpoint: %w", err)
		}
		if download.Purpose != model.DownloadPurposeReplacement || download.ReplacementCandidateID == nil || *download.ReplacementCandidateID != candidate.ID {
			return nil, "", errors.New("replacement download checkpoint belongs to another workflow")
		}
	} else {
		download = &model.Download{
			SubscriptionID:         subscription.ID,
			Title:                  candidate.Title,
			Episode:                originalEpisode,
			Fansub:                 candidate.Fansub,
			Language:               candidate.Language,
			TorrentURL:             candidate.TorrentURL,
			TorrentHash:            queuedHash,
			Status:                 model.DownloadStatusDownloading,
			Purpose:                model.DownloadPurposeReplacement,
			ReplacementCandidateID: &candidate.ID,
			MaxRetries:             0,
		}
		if err := d.downloads.Create(download); err != nil {
			return nil, "", err
		}
		checkpoint := d.db.Model(&model.EpisodeResourceCandidate{}).
			Where("id = ? AND status = ? AND replacement_stage = ? AND replacement_download_id IS NULL", candidate.ID, model.CandidateStatusReplacing, ReplacementStageDownloading).
			Update("replacement_download_id", download.ID)
		if checkpoint.Error != nil || checkpoint.RowsAffected != 1 {
			_ = d.downloads.Delete(download.ID)
			if checkpoint.Error != nil {
				return nil, "", checkpoint.Error
			}
			return nil, "", errors.New("replacement candidate checkpoint changed before qBittorrent add")
		}
		candidate.ReplacementDownloadID = &download.ID
	}

	if stagedDir == "" {
		provisional := replacementFileName(d.renameTemplate, &subscription, download, candidate.Title, "")
		stagedDir = filepath.Join(root, filepath.Dir(provisional), ".auto-rss-replacements", fmt.Sprint(candidate.ID))
	}
	stagedDir, err = filepath.Abs(stagedDir)
	if err != nil || !pathWithin(root, stagedDir) {
		return download, "", fmt.Errorf("replacement staging directory escapes download root: %s", stagedDir)
	}
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		return download, "", err
	}
	if err := validateRenameEndpoint(stagedDir, false); err != nil {
		return download, "", err
	}

	hash := strings.TrimSpace(download.TorrentHash)
	if hash == "" || hash == queuedHash {
		hash, err = d.qb.AddTorrent(candidate.TorrentURL, stagedDir, replacementTorrentCategoryPrefix+fmt.Sprint(candidate.ID))
		if err != nil {
			download.Status = model.DownloadStatusFailed
			download.ErrorMessage = err.Error()
			_ = d.downloads.Update(download)
			return download, "", err
		}
		hash = strings.TrimSpace(hash)
		if hash == "" {
			hash = strings.TrimSpace(candidate.TorrentHash)
		}
		if hash == "" {
			return download, "", errors.New("qBittorrent did not return a replacement torrent hash")
		}
		download.TorrentHash = hash
		if err := d.downloads.Update(download); err != nil {
			return download, "", err
		}
	}
	if download.FilePath != "" && download.RenamedPath != "" && pathWithin(stagedDir, download.FilePath) && (osFilePromoter{}).Exists(download.FilePath) {
		if err := d.qb.RemoveTorrentTask(hash); err != nil {
			return download, "", fmt.Errorf("detach replacement torrent: %w", err)
		}
		download.Status = model.DownloadStatusCompleted
		if err := d.downloads.Update(download); err != nil {
			return download, "", err
		}
		return download, download.FilePath, nil
	}

	info, err := d.waitForCompletion(ctx, hash)
	if err != nil {
		download.Status = model.DownloadStatusFailed
		download.ErrorMessage = err.Error()
		_ = d.downloads.Update(download)
		return download, "", err
	}
	fileName, err := d.completedFileName(hash, info)
	if err != nil {
		return download, "", err
	}
	source := filepath.Join(stagedDir, filepath.FromSlash(fileName))
	if !pathWithin(stagedDir, source) {
		return download, "", fmt.Errorf("qBittorrent replacement file escapes staging directory: %s", fileName)
	}
	if err := validateRenameEndpoint(source, true); err != nil {
		return download, "", fmt.Errorf("replacement staged file validation failed: %w", err)
	}

	extension := filepath.Ext(fileName)
	finalPath := strings.TrimSpace(candidate.FinalPath)
	if finalPath == "" {
		relative := replacementFileName(d.renameTemplate, &subscription, download, fileName, extension)
		finalPath = filepath.Join(root, filepath.FromSlash(relative))
	}
	finalPath, err = filepath.Abs(finalPath)
	if err != nil || !pathWithin(root, finalPath) {
		return download, "", fmt.Errorf("replacement final path escapes download root: %s", finalPath)
	}
	if err := validateRenameEndpoint(finalPath, false); err != nil {
		return download, "", err
	}
	stagedPath := filepath.Join(stagedDir, filepath.Base(finalPath))
	download.FilePath = stagedPath
	download.RenamedPath = finalPath
	if err := d.downloads.Update(download); err != nil {
		return download, "", err
	}
	if source != stagedPath {
		if err := (osFilePromoter{}).Move(source, stagedPath); err != nil {
			return download, "", fmt.Errorf("rename replacement in staging directory: %w", err)
		}
	}
	if err := validateStagedPath(stagedPath, stagedDir); err != nil {
		return download, "", err
	}
	if err := d.qb.RemoveTorrentTask(hash); err != nil {
		return download, "", fmt.Errorf("detach replacement torrent: %w", err)
	}
	now := time.Now()
	download.Status = model.DownloadStatusCompleted
	download.DownloadedAt = &now
	if err := d.downloads.Update(download); err != nil {
		return download, "", err
	}
	return download, stagedPath, nil
}

func (d *qbReplacementDownloader) waitForCompletion(ctx context.Context, hash string) (*qbclient.TorrentInfo, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := d.qb.GetTorrentInfo(hash)
		if err != nil {
			return nil, err
		}
		if info != nil && (info.Progress >= 1 || info.State == "uploading" || info.State == "pausedUP" || info.State == "queuedUP") {
			return info, nil
		}
		if info != nil && (info.State == "error" || info.State == "missingFiles") {
			return nil, fmt.Errorf("replacement torrent entered state %s", info.State)
		}
		timer := time.NewTimer(replacementPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (d *qbReplacementDownloader) completedFileName(hash string, info *qbclient.TorrentInfo) (string, error) {
	files, err := d.qb.GetTorrentFiles(hash)
	if err == nil && len(files) > 0 {
		var selected *qbclient.TorrentFile
		for i := range files {
			file := &files[i]
			if file.Progress < 1 || !replacementVideoFile(file.Name) {
				continue
			}
			if selected == nil || file.Size > selected.Size {
				selected = file
			}
		}
		if selected != nil && strings.TrimSpace(selected.Name) != "" {
			return selected.Name, nil
		}
	}
	if info != nil && strings.TrimSpace(info.Name) != "" && replacementVideoFile(info.Name) {
		return info.Name, nil
	}
	return "", errors.New("completed replacement torrent has no staged file")
}

func replacementVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mkv", ".avi", ".wmv", ".flv", ".mov", ".m4v", ".ts", ".m2ts", ".webm":
		return true
	default:
		return false
	}
}

func replacementFileName(template string, subscription *model.Subscription, download *model.Download, originalName, extension string) string {
	if template == "" {
		template = "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	}
	title := ""
	season := 1
	fansub := ""
	language := ""
	if subscription != nil {
		title = utils.MediaLibraryTitle(subscription.Name)
		season = subscription.Season
		fansub = subscription.Fansub
		language = subscription.Language
	}
	episode := 0
	if download != nil {
		episode = download.Episode
	}
	result := template
	result = strings.ReplaceAll(result, "${title}", utils.SanitizeDirectoryName(title))
	result = strings.ReplaceAll(result, "${season}", fmt.Sprint(season))
	result = strings.ReplaceAll(result, "${seasonFormat}", fmt.Sprintf("%02d", season))
	result = strings.ReplaceAll(result, "${episode}", fmt.Sprint(episode))
	result = strings.ReplaceAll(result, "${episodeFormat}", fmt.Sprintf("%02d", episode))
	result = strings.ReplaceAll(result, "${fansub}", utils.SanitizeDirectoryName(fansub))
	result = strings.ReplaceAll(result, "${language}", language)
	result = strings.ReplaceAll(result, "${resolution}", replacementResolution(originalName))
	return result + extension
}

func replacementResolution(name string) string {
	for _, resolution := range []string{"2160p", "1080p", "720p", "480p", "4K", "UHD"} {
		if strings.Contains(strings.ToLower(name), strings.ToLower(resolution)) {
			return resolution
		}
	}
	return "Unknown"
}

func (d *qbReplacementDownloader) CleanupFailedDownload(download *model.Download) error {
	if download == nil {
		return nil
	}
	if download.Purpose != model.DownloadPurposeReplacement || download.ReplacementCandidateID == nil {
		return errors.New("refusing payload cleanup for non-replacement download")
	}
	hash := strings.TrimSpace(download.TorrentHash)
	if hash == "" || strings.HasPrefix(hash, "replacement:") {
		return nil
	}
	var candidate model.EpisodeResourceCandidate
	if err := d.db.Select("old_torrent_hash").First(&candidate, *download.ReplacementCandidateID).Error; err != nil {
		return fmt.Errorf("verify replacement cleanup ownership: %w", err)
	}
	if candidate.OldTorrentHash != "" && strings.EqualFold(strings.TrimSpace(candidate.OldTorrentHash), hash) {
		return errors.New("refusing payload cleanup for old active torrent")
	}
	return d.qb.DeleteTorrentWithPayload(hash)
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
