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

const (
	DefaultEpisodeCandidateLimit = 100
	MaxEpisodeCandidateLimit     = 500
	episodeStatusChunkSize       = 200
	episodeStatusInsertBatchSize = 50
)

type EpisodeRepository interface {
	RunInTransaction(fn func(*gorm.DB) error) error
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
	SetUserStatus(subscriptionID uint, episodes []int, status string) error
	UpsertCandidate(episodeID uint, candidate *model.EpisodeResourceCandidate) (*model.EpisodeResourceCandidate, bool, error)
	ListCandidates(episodeID uint) ([]model.EpisodeResourceCandidate, error)
	ListCandidatesByScope(subscriptionID uint, episode, offset, limit int) ([]model.EpisodeResourceCandidate, error)
	KeepCandidate(subscriptionID uint, episode int, candidateID uint) (*model.EpisodeResourceCandidate, error)
	UpdateCandidate(candidate *model.EpisodeResourceCandidate) error
	GetCandidateByID(candidateID uint) (*model.EpisodeResourceCandidate, error)
	ClaimCandidateForReplacement(candidateID uint) (*model.EpisodeResourceCandidate, error)
	ListIncompleteReplacements() ([]model.EpisodeResourceCandidate, error)
	ObserveEpisode(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error)
	EnsureRange(subscriptionID uint, total int) error
	EnsureRangeInTx(tx *gorm.DB, subscriptionID uint, total int) error
	RefreshSubscriptionProgress(subscriptionID uint) error
	RefreshSubscriptionProgressInTx(tx *gorm.DB, subscriptionID uint) error
}

var ErrActiveDownloadMustBeResolved = errors.New("active download must be resolved")
var ErrCandidateStateConflict = errors.New("candidate state conflict")
var ErrReplacementInProgress = errors.New("replacement already in progress")

type episodeRepository struct {
	db *gorm.DB
}

func NewEpisodeRepository(db *gorm.DB) EpisodeRepository {
	return &episodeRepository{db: db}
}

func (r *episodeRepository) RunInTransaction(fn func(*gorm.DB) error) error {
	return r.db.Transaction(fn)
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
		var current model.SubscriptionEpisode
		if err := tx.Where("id = ?", episodeID).First(&current).Error; err == nil &&
			current.Status == model.EpisodeStatusDownloaded &&
			current.ActiveDownloadID != nil && *current.ActiveDownloadID == downloadID &&
			sameEpisodeResource(current, resource) {
			return nil
		}
		return fmt.Errorf("download %d is not active for episode %d", downloadID, episodeID)
	}
	return nil
}

func sameEpisodeResource(episode model.SubscriptionEpisode, resource model.EpisodeResource) bool {
	currentHash := strings.TrimSpace(episode.ActiveTorrentHash)
	requestedHash := strings.TrimSpace(resource.Hash)
	if currentHash != "" || requestedHash != "" {
		return currentHash != "" && requestedHash != "" && strings.EqualFold(currentHash, requestedHash)
	}
	currentURL := strings.TrimSpace(episode.ActiveTorrentURL)
	requestedURL := strings.TrimSpace(resource.URL)
	return currentURL != "" && requestedURL != "" && currentURL == requestedURL
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

func (r *episodeRepository) SetUserStatus(subscriptionID uint, episodes []int, status string) error {
	if len(episodes) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		entries := make([]model.SubscriptionEpisode, 0, len(episodes))
		for _, episode := range episodes {
			entries = append(entries, model.SubscriptionEpisode{
				SubscriptionID: subscriptionID,
				Episode:        episode,
				Status:         model.EpisodeStatusMissing,
				StatusSource:   model.EpisodeStatusSourceUser,
			})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&entries, episodeStatusInsertBatchSize).Error; err != nil {
			return err
		}

		if status == model.EpisodeStatusMissing {
			for start := 0; start < len(episodes); start += episodeStatusChunkSize {
				end := min(start+episodeStatusChunkSize, len(episodes))
				var activeCount int64
				if err := tx.Model(&model.SubscriptionEpisode{}).
					Joins("JOIN downloads ON downloads.id = subscription_episodes.active_download_id").
					Where("subscription_episodes.subscription_id = ? AND subscription_episodes.episode IN ?", subscriptionID, episodes[start:end]).
					Where("downloads.status IN ?", []string{
						model.DownloadStatusPending,
						model.DownloadStatusDownloading,
						model.DownloadStatusStalled,
						model.DownloadStatusOrganizing,
					}).Count(&activeCount).Error; err != nil {
					return err
				}
				if activeCount > 0 {
					return ErrActiveDownloadMustBeResolved
				}
			}
		}

		updates := clearActiveDownloadUpdates(status, model.EpisodeStatusSourceUser)
		for start := 0; start < len(episodes); start += episodeStatusChunkSize {
			end := min(start+episodeStatusChunkSize, len(episodes))
			if err := tx.Model(&model.SubscriptionEpisode{}).
				Where("subscription_id = ? AND episode IN ?", subscriptionID, episodes[start:end]).
				Updates(updates).Error; err != nil {
				return err
			}
		}
		return r.RefreshSubscriptionProgressInTx(tx, subscriptionID)
	})
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

func (r *episodeRepository) ListCandidatesByScope(subscriptionID uint, episode, offset, limit int) ([]model.EpisodeResourceCandidate, error) {
	if limit <= 0 {
		limit = DefaultEpisodeCandidateLimit
	}
	if limit > MaxEpisodeCandidateLimit {
		limit = MaxEpisodeCandidateLimit
	}
	if offset < 0 {
		offset = 0
	}
	var candidates []model.EpisodeResourceCandidate
	err := r.db.Model(&model.EpisodeResourceCandidate{}).
		Joins("JOIN subscription_episodes ON subscription_episodes.id = episode_resource_candidates.subscription_episode_id").
		Where("subscription_episodes.subscription_id = ? AND subscription_episodes.episode = ?", subscriptionID, episode).
		Order("episode_resource_candidates.created_at ASC, episode_resource_candidates.id ASC").
		Offset(offset).
		Limit(limit).
		Find(&candidates).Error
	return candidates, err
}

func (r *episodeRepository) KeepCandidate(subscriptionID uint, episode int, candidateID uint) (*model.EpisodeResourceCandidate, error) {
	scopedEpisodeIDs := r.db.Model(&model.SubscriptionEpisode{}).
		Select("id").
		Where("subscription_id = ? AND episode = ?", subscriptionID, episode)
	result := r.db.Model(&model.EpisodeResourceCandidate{}).
		Where("id = ? AND subscription_episode_id IN (?) AND status = ?", candidateID, scopedEpisodeIDs, model.CandidateStatusPending).
		Update("status", model.CandidateStatusKeptExisting)
	if result.Error != nil {
		return nil, result.Error
	}

	var candidate model.EpisodeResourceCandidate
	err := r.db.Model(&model.EpisodeResourceCandidate{}).
		Joins("JOIN subscription_episodes ON subscription_episodes.id = episode_resource_candidates.subscription_episode_id").
		Where(
			"subscription_episodes.subscription_id = ? AND subscription_episodes.episode = ? AND episode_resource_candidates.id = ?",
			subscriptionID,
			episode,
			candidateID,
		).First(&candidate).Error
	if err != nil {
		return nil, err
	}
	if candidate.Status != model.CandidateStatusKeptExisting {
		return nil, fmt.Errorf("%w: candidate %d has status %s", ErrCandidateStateConflict, candidateID, candidate.Status)
	}
	return &candidate, nil
}

func (r *episodeRepository) UpdateCandidate(candidate *model.EpisodeResourceCandidate) error {
	if candidate == nil {
		return errors.New("candidate is required")
	}
	return r.db.Save(candidate).Error
}

func (r *episodeRepository) GetCandidateByID(candidateID uint) (*model.EpisodeResourceCandidate, error) {
	var candidate model.EpisodeResourceCandidate
	if err := r.db.First(&candidate, candidateID).Error; err != nil {
		return nil, err
	}
	return &candidate, nil
}

func (r *episodeRepository) ClaimCandidateForReplacement(candidateID uint) (*model.EpisodeResourceCandidate, error) {
	result := r.db.Exec(`
		UPDATE episode_resource_candidates
		SET status = ?, replacement_stage = ?, failure_reason = '',
			staged_path = '', old_resource_path = '', rollback_path = '',
			replacement_download_id = NULL, old_download_id = NULL, old_torrent_hash = '',
			updated_at = ?
		WHERE id = ?
		  AND status IN (?, ?)
		  AND NOT EXISTS (
			SELECT 1 FROM episode_resource_candidates active
			WHERE active.subscription_episode_id = episode_resource_candidates.subscription_episode_id
			  AND active.id <> episode_resource_candidates.id
			  AND active.status = ?
		  )`,
		model.CandidateStatusReplacing, "queued", time.Now(), candidateID,
		model.CandidateStatusPending, model.CandidateStatusFailed, model.CandidateStatusReplacing,
	)
	if result.Error != nil {
		var candidate model.EpisodeResourceCandidate
		if err := r.db.First(&candidate, candidateID).Error; err == nil {
			var active int64
			if countErr := r.db.Model(&model.EpisodeResourceCandidate{}).
				Where("subscription_episode_id = ? AND id <> ? AND status = ?", candidate.SubscriptionEpisodeID, candidate.ID, model.CandidateStatusReplacing).
				Count(&active).Error; countErr == nil && active > 0 {
				return nil, ErrReplacementInProgress
			}
		}
		return nil, result.Error
	}
	candidate, err := r.GetCandidateByID(candidateID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected == 1 {
		return candidate, nil
	}
	if candidate.Status == model.CandidateStatusReplacing {
		return nil, ErrReplacementInProgress
	}
	var active int64
	if err := r.db.Model(&model.EpisodeResourceCandidate{}).
		Where("subscription_episode_id = ? AND id <> ? AND status = ?", candidate.SubscriptionEpisodeID, candidate.ID, model.CandidateStatusReplacing).
		Count(&active).Error; err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, ErrReplacementInProgress
	}
	return nil, fmt.Errorf("%w: candidate %d has status %s", ErrCandidateStateConflict, candidateID, candidate.Status)
}

func (r *episodeRepository) ListIncompleteReplacements() ([]model.EpisodeResourceCandidate, error) {
	var candidates []model.EpisodeResourceCandidate
	err := r.db.Where("status IN ?", []string{
		model.CandidateStatusReplacing,
		model.CandidateStatusAcceptedCleanupFailed,
	}).Order("updated_at ASC, id ASC").Find(&candidates).Error
	return candidates, err
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
	return r.RunInTransaction(func(tx *gorm.DB) error {
		return r.EnsureRangeInTx(tx, subscriptionID, total)
	})
}

func (r *episodeRepository) EnsureRangeInTx(tx *gorm.DB, subscriptionID uint, total int) error {
	if total <= 0 {
		return nil
	}
	if total > model.MaxSubscriptionEpisodes {
		return fmt.Errorf("total episodes cannot exceed %d", model.MaxSubscriptionEpisodes)
	}

	var existingEpisodes []int
	if err := tx.Model(&model.SubscriptionEpisode{}).
		Where("subscription_id = ? AND episode BETWEEN ? AND ?", subscriptionID, 1, total).
		Pluck("episode", &existingEpisodes).Error; err != nil {
		return err
	}
	present := make([]bool, total+1)
	for _, episode := range existingEpisodes {
		if episode > 0 && episode <= total {
			present[episode] = true
		}
	}

	entries := make([]model.SubscriptionEpisode, 0, total-len(existingEpisodes))
	for episode := 1; episode <= total; episode++ {
		if present[episode] {
			continue
		}
		entries = append(entries, model.SubscriptionEpisode{
			SubscriptionID: subscriptionID,
			Episode:        episode,
			Status:         model.EpisodeStatusMissing,
			StatusSource:   model.EpisodeStatusSourceAutomatic,
		})
	}
	if len(entries) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&entries, 200).Error
}

func (r *episodeRepository) RefreshSubscriptionProgress(subscriptionID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return r.RefreshSubscriptionProgressInTx(tx, subscriptionID)
	})
}

func (r *episodeRepository) RefreshSubscriptionProgressInTx(tx *gorm.DB, subscriptionID uint) error {
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
