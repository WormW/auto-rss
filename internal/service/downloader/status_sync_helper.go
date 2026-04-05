package downloader

import (
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
)

// mapQBStateToStatus 将qBittorrent状态映射到数据库状态
func mapQBStateToStatus(qbState string) string {
	switch {
	case strings.Contains(qbState, "error") || qbState == "missingFiles":
		return "failed"
	case qbState == "uploading" || strings.HasSuffix(qbState, "UP"):
		return "completed"
	case qbState == "downloading" || qbState == "forcedDL":
		return "downloading"
	default:
		return "stalled"
	}
}

// isTorrentComplete 检查种子是否已完成
func isTorrentComplete(torrent *TorrentInfo) bool {
	if torrent == nil {
		return false
	}
	if torrent.Size > 0 && torrent.Downloaded >= torrent.Size {
		return true
	}
	return torrent.Progress >= 0.9999
}

// shouldSkipReconcileByGracePeriod 检查是否应该跳过对账（宽限期内）
func shouldSkipReconcileByGracePeriod(download *model.Download, now time.Time) bool {
	if download == nil {
		return true
	}
	return now.Sub(download.UpdatedAt) < ReconcileGracePeriod
}
