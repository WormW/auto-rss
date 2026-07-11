package downloader

import (
	"fmt"
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
	PersistDownloadFailure(download *model.Download, releaseEpisode bool) error
	DetachDownload(downloadID uint) error
}

type downloadFailureRepository interface {
	Update(download *model.Download) error
}

func persistDownloadFailure(downloadRepo downloadFailureRepository, episodeService EpisodeCompletionService, download *model.Download, releaseEpisode bool) error {
	if episodeService != nil {
		return episodeService.PersistDownloadFailure(download, releaseEpisode)
	}
	if download != nil && download.Episode > 0 {
		return fmt.Errorf("episode failure lifecycle service is unavailable for download %d", download.ID)
	}
	return downloadRepo.Update(download)
}
