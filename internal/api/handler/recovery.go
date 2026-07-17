package handler

import (
	"errors"
	"net/http"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/recovery"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const RecoveryPreviewSampleLimit = 10

// RecoveryHandler 扫描恢复处理器
type RecoveryHandler struct {
	db               *gorm.DB
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	bangumiService   *bangumi.BangumiService
	downloadPath     string
}

// NewRecoveryHandler 创建恢复处理器实例
func NewRecoveryHandler(
	db *gorm.DB,
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	bangumiService *bangumi.BangumiService,
	downloadPath string,
) *RecoveryHandler {
	return &RecoveryHandler{
		db:               db,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		bangumiService:   bangumiService,
		downloadPath:     downloadPath,
	}
}

// ScanRequest 扫描请求
type ScanRequest struct {
	DryRun         bool  `json:"dry_run"`
	SubscriptionID *uint `json:"subscription_id,omitempty"`
}

type RecoveryScanPreview struct {
	DryRun                 bool                          `json:"dry_run"`
	PreviewOnly            bool                          `json:"preview_only"`
	Applied                bool                          `json:"applied"`
	ScannedFiles           int                           `json:"scanned_files"`
	MatchedFiles           int                           `json:"matched_files"`
	OrphanFileCount        int                           `json:"orphan_file_count"`
	OrphanFileSamples      []string                      `json:"orphan_file_samples,omitempty"`
	OrphanFileOmittedCount int                           `json:"orphan_file_omitted_count,omitempty"`
	SubscriptionCount      int                           `json:"subscription_count"`
	DownloadsToUpdateCount int                           `json:"downloads_to_update_count"`
	DownloadsToCreateCount int                           `json:"downloads_to_create_count"`
	DownloadsMissingCount  int                           `json:"downloads_missing_count"`
	Subscriptions          []RecoverySubscriptionPreview `json:"subscriptions"`
	BackupPath             string                        `json:"backup_path,omitempty"`
}

type RecoverySubscriptionPreview struct {
	SubscriptionID             uint                   `json:"subscription_id"`
	Name                       string                 `json:"name"`
	CurrentEpisodeOld          int                    `json:"current_episode_old"`
	CurrentEpisodeNew          int                    `json:"current_episode_new"`
	LatestEpisodeOld           int                    `json:"latest_episode_old"`
	LatestEpisodeNew           int                    `json:"latest_episode_new"`
	EpisodesOnDiskCount        int                    `json:"episodes_on_disk_count"`
	EpisodeSamples             []int                  `json:"episode_samples,omitempty"`
	EpisodeOmittedCount        int                    `json:"episode_omitted_count,omitempty"`
	MatchedEpisodeCount        int                    `json:"matched_episode_count"`
	MatchedEpisodeSamples      []recovery.EpisodeFile `json:"matched_episode_samples,omitempty"`
	MatchedEpisodeOmittedCount int                    `json:"matched_episode_omitted_count,omitempty"`
	DownloadsToUpdateCount     int                    `json:"downloads_to_update_count"`
	DownloadsToUpdateIDs       []uint                 `json:"downloads_to_update_ids,omitempty"`
	DownloadsToCreateCount     int                    `json:"downloads_to_create_count"`
	DownloadsToCreate          []int                  `json:"downloads_to_create,omitempty"`
	DownloadsMissingCount      int                    `json:"downloads_missing_count"`
	DownloadsMissingIDs        []uint                 `json:"downloads_missing_ids,omitempty"`
}

// Scan 执行扫描恢复
func (h *RecoveryHandler) Scan(c *gin.Context) {
	var req ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	scanner := recovery.NewScanner(h.db, h.subscriptionRepo, h.downloadRepo, h.configRepo, h.bangumiService, h.downloadPath)
	result, err := scanner.Scan(&recovery.ScanRequest{
		DryRun:         req.DryRun,
		SubscriptionID: req.SubscriptionID,
	})
	if err != nil {
		if errors.Is(err, recovery.ErrApplyDisabled) {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "扫描恢复失败: " + err.Error(),
			})
			return
		}
		logger.Error("Recovery scan failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "扫描恢复失败: " + err.Error(),
		})
		return
	}

	msg := "扫描完成"
	if result.Applied {
		msg = "扫描并修正完成"
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": msg,
		"data":    summarizeRecoveryScan(result, req.DryRun),
	})
}

func summarizeRecoveryScan(result *recovery.ScanResult, dryRun bool) RecoveryScanPreview {
	out := RecoveryScanPreview{
		DryRun:                 dryRun,
		PreviewOnly:            dryRun,
		Applied:                result.Applied,
		ScannedFiles:           result.ScannedFiles,
		MatchedFiles:           result.MatchedFiles,
		OrphanFileCount:        len(result.OrphanFiles),
		OrphanFileSamples:      limitStrings(result.OrphanFiles, RecoveryPreviewSampleLimit),
		OrphanFileOmittedCount: omittedCount(len(result.OrphanFiles), RecoveryPreviewSampleLimit),
		SubscriptionCount:      len(result.Subscriptions),
		Subscriptions:          make([]RecoverySubscriptionPreview, 0, len(result.Subscriptions)),
		BackupPath:             result.BackupPath,
	}

	for _, sub := range result.Subscriptions {
		preview := RecoverySubscriptionPreview{
			SubscriptionID:             sub.SubscriptionID,
			Name:                       sub.Name,
			CurrentEpisodeOld:          sub.CurrentEpisodeOld,
			CurrentEpisodeNew:          sub.CurrentEpisodeNew,
			LatestEpisodeOld:           sub.LatestEpisodeOld,
			LatestEpisodeNew:           sub.LatestEpisodeNew,
			EpisodesOnDiskCount:        len(sub.EpisodesOnDisk),
			EpisodeSamples:             limitInts(sub.EpisodesOnDisk, RecoveryPreviewSampleLimit),
			EpisodeOmittedCount:        omittedCount(len(sub.EpisodesOnDisk), RecoveryPreviewSampleLimit),
			MatchedEpisodeCount:        len(sub.MatchedEpisodes),
			MatchedEpisodeSamples:      limitEpisodeFiles(sub.MatchedEpisodes, RecoveryPreviewSampleLimit),
			MatchedEpisodeOmittedCount: omittedCount(len(sub.MatchedEpisodes), RecoveryPreviewSampleLimit),
			DownloadsToUpdateCount:     len(sub.DownloadsToUpdate),
			DownloadsToUpdateIDs:       limitUints(sub.DownloadsToUpdate, RecoveryPreviewSampleLimit),
			DownloadsToCreateCount:     len(sub.DownloadsToCreate),
			DownloadsToCreate:          limitInts(sub.DownloadsToCreate, RecoveryPreviewSampleLimit),
			DownloadsMissingCount:      len(sub.DownloadsMissing),
			DownloadsMissingIDs:        limitUints(sub.DownloadsMissing, RecoveryPreviewSampleLimit),
		}
		out.DownloadsToUpdateCount += preview.DownloadsToUpdateCount
		out.DownloadsToCreateCount += preview.DownloadsToCreateCount
		out.DownloadsMissingCount += preview.DownloadsMissingCount
		out.Subscriptions = append(out.Subscriptions, preview)
	}

	return out
}

func limitStrings(items []string, limit int) []string {
	if len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		return append([]string(nil), items[:limit]...)
	}
	return append([]string(nil), items...)
}

func limitInts(items []int, limit int) []int {
	if len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		return append([]int(nil), items[:limit]...)
	}
	return append([]int(nil), items...)
}

func limitUints(items []uint, limit int) []uint {
	if len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		return append([]uint(nil), items[:limit]...)
	}
	return append([]uint(nil), items...)
}

func limitEpisodeFiles(items []recovery.EpisodeFile, limit int) []recovery.EpisodeFile {
	if len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		return append([]recovery.EpisodeFile(nil), items[:limit]...)
	}
	return append([]recovery.EpisodeFile(nil), items...)
}

func omittedCount(length, limit int) int {
	if length <= limit {
		return 0
	}
	return length - limit
}
