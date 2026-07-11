package handler

import "github.com/WormW/auto-rss/internal/model"

type DownloadRequeueService interface {
	RequeueDownload(download *model.Download, subscription *model.Subscription) error
}

func resetDownloadForManualRetry(download *model.Download) {
	download.RetryCount = 0
	download.RetryReason = "user_retry"
	download.NextRetryAt = nil
	download.LastError = ""
	download.ErrorMessage = ""
	download.Status = model.DownloadStatusPending
	download.TorrentHash = ""
}
