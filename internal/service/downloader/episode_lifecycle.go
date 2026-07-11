package downloader

import (
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// EpisodeCompletionService is the download lifecycle boundary owned by the
// episode ledger. Keeping it narrow avoids coupling downloader to its concrete
// implementation.
type EpisodeCompletionService interface {
	MarkDownloadCompleted(download *model.Download, sub *model.Subscription, completedAt time.Time) error
	MarkDownloadCompletedInTx(tx *gorm.DB, download *model.Download, sub *model.Subscription, completedAt time.Time) error
	MarkDownloadFailed(downloadID uint) error
	DetachDownload(downloadID uint) error
}
