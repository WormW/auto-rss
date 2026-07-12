package handler

import (
	"errors"

	"github.com/WormW/auto-rss/internal/model"
)

var errEpisodeRetryLifecycleUnavailable = errors.New("episode retry lifecycle service is unavailable")

type DownloadRequeueService interface {
	RequeueDownload(download *model.Download, subscription *model.Subscription) error
}

func resetDownloadForManualRetry(download *model.Download) {
	download.RetryCount = 0
	download.RetryReason = "user_retry"
	download.NextRetryAt = nil
	download.LastError = ""
	download.ErrorMessage = ""
	download.Status = model.DownloadStatusRetryCleanup
}
