package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EpisodeWithCandidateCount struct {
	model.SubscriptionEpisode
	ActionRequiredCandidateCount int64 `json:"action_required_candidate_count" gorm:"column:action_required_candidate_count"`
}

type EpisodeRepository interface {
	ListBySubscription(subscriptionID uint) ([]model.SubscriptionEpisode, error)
	ListWithCandidateCounts(subscriptionID uint) ([]EpisodeWithCandidateCount, error)
	GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error)
	ClaimForDownload(subscriptionID uint, episode int, resource model.EpisodeResource) (*model.SubscriptionEpisode, bool, error)
	AttachDownload(episodeID, downloadID uint) error
	AttachDownloadInTx(tx *gorm.DB, episodeID, downloadID uint) error
	ReleaseDownloadClaim(episodeID uint, resource model.EpisodeResource) error
	MarkDownloaded(episodeID, downloadID uint, resource model.EpisodeResource, downloadedAt time.Time) error
	MarkDownloadedInTx(tx *gorm.DB, episodeID, downloadID uint, resource model.EpisodeResource, downloadedAt time.Time) error
	MarkMissingIfActiveDownload(downloadID uint) error
	DetachDownload(downloadID uint) error
	SetStatus(subscriptionID uint, episodes []int, status, source string) error
	UpsertCandidate(episodeID uint, candidate *model.EpisodeResourceCandidate) (*model.EpisodeResourceCandidate, bool, error)
	ListCandidates(episodeID uint) ([]model.EpisodeResourceCandidate, error)
	UpdateCandidate(candidate *model.EpisodeResourceCandidate) error
	ObserveEpisode(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error)
	EnsureRange(subscriptionID uint, total int) error
	RefreshSubscriptionProgress(subscriptionID uint) error
}

type episodeRepository struct {
	db *gorm.DB
}

func NewEpisodeRepository(db *gorm.DB) EpisodeRepository {
	return &episodeRepository{db: db}
}

func (r *episodeRepository) ListBySubscription(subscriptionID uint) ([]model.SubscriptionEpisode, error) {
	var episodes []model.SubscriptionEpisode
	err := r.db.Where("subscription_id = ?", subscriptionID).Order("episode ASC").Find(&episodes).Error
	return episodes, err
}

func (r *episodeRepository) ListWithCandidateCounts(subscriptionID uint) ([]EpisodeWithCandidateCount, error) {
	var episodes []EpisodeWithCandidateCount
	err := r.db.Model(&model.SubscriptionEpisode{}).
		Select(
			"subscription_episodes.*, COUNT(CASE WHEN episode_resource_candidates.status IN (?, ?, ?) THEN 1 END) AS action_required_candidate_count",
			model.CandidateStatusPending,
			model.CandidateStatusFailed,
			model.CandidateStatusAcceptedCleanupFailed,
		).
		Joins("LEFT JOIN episode_resource_candidates ON episode_resource_candidates.subscription_episode_id = subscription_episodes.id").
		Where("subscription_episodes.subscription_id = ?", subscriptionID).
		Group("subscription_episodes.id").
		Order("subscription_episodes.episode ASC").
		Scan(&episodes).Error
	return episodes, err
}

func (r *episodeRepository) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error) {
	var result model.SubscriptionEpisode
	err := r.db.Where("subscription_id = ? AND episode = ?", subscriptionID, episode).First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *episodeRepository) ClaimForDownload(subscriptionID uint, episode int, resource model.EpisodeResource) (*model.SubscriptionEpisode, bool, error) {
	if episode <= 0 {
		return nil, false, fmt.Errorf("episode must be positive")
	}

	var result model.SubscriptionEpisode
	claimed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		missing := model.SubscriptionEpisode{
			SubscriptionID: subscriptionID,
			Episode:        episode,
			Status:         model.EpisodeStatusMissing,
			StatusSource:   model.EpisodeStatusSourceAutomatic,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&missing).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"status":              model.EpisodeStatusDownloading,
			"status_source":       model.EpisodeStatusSourceAutomatic,
			"active_download_id":  nil,
			"active_torrent_hash": resource.Hash,
			"active_torrent_url":  resource.URL,
			"active_title":        resource.Title,
			"downloaded_at":       nil,
		}
		update := tx.Model(&model.SubscriptionEpisode{}).
			Where("subscription_id = ? AND episode = ? AND status = ?", subscriptionID, episode, model.EpisodeStatusMissing).
			Updates(updates)
		if update.Error != nil {
			return update.Error
		}
		claimed = update.RowsAffected == 1
		return tx.Where("subscription_id = ? AND episode = ?", subscriptionID, episode).First(&result).Error
	})
	if err != nil {
		return nil, false, err
	}
	return &result, claimed, nil
}

func (r *episodeRepository) AttachDownload(episodeID, downloadID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return r.AttachDownloadInTx(tx, episodeID, downloadID)
	})
}

func (r *episodeRepository) AttachDownloadInTx(tx *gorm.DB, episodeID, downloadID uint) error {
	result := tx.Model(&model.SubscriptionEpisode{}).
		Where("id = ? AND status = ? AND active_download_id IS NULL", episodeID, model.EpisodeStatusDownloading).
		Update("active_download_id", downloadID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("episode %d is not an unattached download claim", episodeID)
	}
	return nil
}

func (r *episodeRepository) ReleaseDownloadClaim(episodeID uint, resource model.EpisodeResource) error {
	query := r.db.Model(&model.SubscriptionEpisode{}).
		Where("id = ? AND status = ? AND active_download_id IS NULL", episodeID, model.EpisodeStatusDownloading)
	if hash := strings.TrimSpace(resource.Hash); hash != "" {
		query = query.Where("LOWER(TRIM(active_torrent_hash)) = ?", strings.ToLower(hash))
	} else if url := strings.TrimSpace(resource.URL); url != "" {
		query = query.Where("TRIM(active_torrent_hash) = '' AND TRIM(active_torrent_url) = ?", url)
	} else {
		return nil
	}
	return query.Updates(clearActiveDownloadUpdates(model.EpisodeStatusMissing, model.EpisodeStatusSourceAutomatic)).Error
}

func (r *episodeRepository) MarkDownloaded(episodeID, downloadID uint, resource model.EpisodeResource, downloadedAt time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return r.MarkDownloadedInTx(tx, episodeID, downloadID, resource, downloadedAt)
	})
}

func (r *episodeRepository) MarkDownloadedInTx(tx *gorm.DB, episodeID, downloadID uint, resource model.EpisodeResource, downloadedAt time.Time) error {
	result := tx.Model(&model.SubscriptionEpisode{}).
		Where("id = ? AND status = ? AND active_download_id = ?", episodeID, model.EpisodeStatusDownloading, downloadID).
		Updates(map[string]any{
			"status":              model.EpisodeStatusDownloaded,
			"status_source":       model.EpisodeStatusSourceAutomatic,
			"active_torrent_hash": resource.Hash,
			"active_torrent_url":  resource.URL,
			"active_title":        resource.Title,
			"downloaded_at":       downloadedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("download %d is not active for episode %d", downloadID, episodeID)
	}
	return nil
}

func (r *episodeRepository) MarkMissingIfActiveDownload(downloadID uint) error {
	return r.db.Model(&model.SubscriptionEpisode{}).
		Where("active_download_id = ? AND status = ?", downloadID, model.EpisodeStatusDownloading).
		Updates(clearActiveDownloadUpdates(model.EpisodeStatusMissing, model.EpisodeStatusSourceAutomatic)).Error
}

func (r *episodeRepository) DetachDownload(downloadID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SubscriptionEpisode{}).
			Where("active_download_id = ? AND status = ?", downloadID, model.EpisodeStatusDownloaded).
			Update("active_download_id", nil).Error; err != nil {
			return err
		}
		return tx.Model(&model.SubscriptionEpisode{}).
			Where("active_download_id = ? AND status <> ?", downloadID, model.EpisodeStatusDownloaded).
			Updates(clearActiveDownloadUpdates(model.EpisodeStatusMissing, model.EpisodeStatusSourceAutomatic)).Error
	})
}

func (r *episodeRepository) SetStatus(subscriptionID uint, episodes []int, status, source string) error {
	if len(episodes) == 0 {
		return nil
	}
	updates := map[string]any{"status": status, "status_source": source}
	if status == model.EpisodeStatusMissing || status == model.EpisodeStatusIgnored || status == model.EpisodeStatusMarkedDownloaded {
		for key, value := range clearActiveDownloadUpdates(status, source) {
			updates[key] = value
		}
	}
	return r.db.Model(&model.SubscriptionEpisode{}).
		Where("subscription_id = ? AND episode IN ?", subscriptionID, episodes).
		Updates(updates).Error
}

func (r *episodeRepository) UpsertCandidate(episodeID uint, candidate *model.EpisodeResourceCandidate) (*model.EpisodeResourceCandidate, bool, error) {
	if candidate == nil {
		return nil, false, errors.New("candidate is required")
	}
	if strings.TrimSpace(candidate.ResourceKey) == "" {
		return nil, false, errors.New("resource key is required")
	}

	lookup := model.EpisodeResourceCandidate{}
	err := r.db.Where(
		"subscription_episode_id = ? AND resource_key = ?",
		episodeID,
		candidate.ResourceKey,
	).First(&lookup).Error
	if err == nil {
		return &lookup, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	candidateToCreate := *candidate
	candidateToCreate.SubscriptionEpisodeID = episodeID
	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidateToCreate)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		return &candidateToCreate, true, nil
	}
	if err := r.db.Where(
		"subscription_episode_id = ? AND resource_key = ?",
		episodeID,
		candidate.ResourceKey,
	).First(&lookup).Error; err != nil {
		return nil, false, err
	}
	return &lookup, false, nil
}

func (r *episodeRepository) ListCandidates(episodeID uint) ([]model.EpisodeResourceCandidate, error) {
	var candidates []model.EpisodeResourceCandidate
	err := r.db.Where("subscription_episode_id = ?", episodeID).Order("created_at ASC, id ASC").Find(&candidates).Error
	return candidates, err
}

func (r *episodeRepository) UpdateCandidate(candidate *model.EpisodeResourceCandidate) error {
	if candidate == nil {
		return errors.New("candidate is required")
	}
	return r.db.Save(candidate).Error
}

func (r *episodeRepository) ObserveEpisode(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error) {
	if episode <= 0 {
		return nil, fmt.Errorf("episode must be positive")
	}
	observed := model.SubscriptionEpisode{
		SubscriptionID: subscriptionID,
		Episode:        episode,
		Status:         model.EpisodeStatusMissing,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}
	if err := r.db.Where("subscription_id = ? AND episode = ?", subscriptionID, episode).FirstOrCreate(&observed).Error; err != nil {
		return nil, err
	}
	return &observed, nil
}

func (r *episodeRepository) EnsureRange(subscriptionID uint, total int) error {
	if total <= 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for episode := 1; episode <= total; episode++ {
			entry := model.SubscriptionEpisode{
				SubscriptionID: subscriptionID,
				Episode:        episode,
				Status:         model.EpisodeStatusMissing,
				StatusSource:   model.EpisodeStatusSourceAutomatic,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *episodeRepository) RefreshSubscriptionProgress(subscriptionID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var subscription model.Subscription
		if err := tx.First(&subscription, subscriptionID).Error; err != nil {
			return err
		}
		var episodes []model.SubscriptionEpisode
		if err := tx.Where("subscription_id = ?", subscriptionID).Order("episode ASC").Find(&episodes).Error; err != nil {
			return err
		}

		continuousOwned := 0
		latest := 0
		for _, episode := range episodes {
			if episode.Episode > latest {
				latest = episode.Episode
			}
			if episode.Episode == continuousOwned+1 && episodeStatusCountsAsOwned(episode.Status) {
				continuousOwned++
			}
		}
		currentEpisode := progressWithOffset(continuousOwned, subscription.EpisodeOffset)
		latestEpisode := progressWithOffset(latest, subscription.EpisodeOffset)
		subscription.CurrentEpisode = currentEpisode
		completedAt := subscription.CompletedAt
		if subscription.IsCompleted() {
			if completedAt == nil {
				now := time.Now()
				completedAt = &now
			}
		} else {
			completedAt = nil
		}

		return tx.Model(&model.Subscription{}).
			Where("id = ?", subscriptionID).
			Updates(map[string]any{
				"current_episode": currentEpisode,
				"latest_episode":  latestEpisode,
				"completed_at":    completedAt,
			}).Error
	})
}

func clearActiveDownloadUpdates(status, source string) map[string]any {
	return map[string]any{
		"status":              status,
		"status_source":       source,
		"active_download_id":  nil,
		"active_torrent_hash": "",
		"active_torrent_url":  "",
		"active_title":        "",
		"downloaded_at":       nil,
	}
}

func episodeStatusCountsAsOwned(status string) bool {
	switch status {
	case model.EpisodeStatusDownloaded, model.EpisodeStatusMarkedDownloaded, model.EpisodeStatusIgnored:
		return true
	default:
		return false
	}
}

func progressWithOffset(relativeEpisode, offset int) int {
	if relativeEpisode == 0 {
		return 0
	}
	if offset < 0 {
		offset = 0
	}
	return relativeEpisode + offset
}
