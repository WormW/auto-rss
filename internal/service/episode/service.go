package episode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

const (
	DecisionDownload  = "download"
	DecisionSkip      = "skip"
	DecisionCandidate = "candidate"
	DecisionIgnored   = "ignored"
	DecisionBaseline  = "baseline_missing"
)

type RSSResource struct {
	OriginalEpisode int
	Resource        model.EpisodeResource
	Fansub          string
	Language        string
	PubTime         time.Time
	SourceRSSURL    string
}

type RSSDecision struct {
	Action      string
	EpisodeID   uint
	CandidateID uint
	Reason      string
}

type Service struct {
	repository repository.EpisodeRepository
}

func NewService(repository repository.EpisodeRepository) *Service {
	return &Service{repository: repository}
}

func ResourceKey(resource model.EpisodeResource) string {
	if hash := strings.TrimSpace(resource.Hash); hash != "" {
		return "hash:" + strings.ToLower(hash)
	}
	if url := strings.TrimSpace(resource.URL); url != "" {
		return "url:" + url
	}
	return ""
}

func (s *Service) ObserveRSSItem(sub *model.Subscription, originalEpisode int) (*model.SubscriptionEpisode, error) {
	relativeEpisode := sub.RelativeEpisode(originalEpisode)
	if relativeEpisode <= 0 {
		return nil, nil
	}
	return s.repository.ObserveEpisode(sub.ID, relativeEpisode)
}

func (s *Service) EvaluateRSSItem(ctx context.Context, sub *model.Subscription, item RSSResource, baseline bool) (RSSDecision, error) {
	if err := ctx.Err(); err != nil {
		return RSSDecision{}, err
	}
	return s.evaluate(sub, item, baseline, true)
}

func (s *Service) PreviewRSSItem(sub *model.Subscription, item RSSResource) (RSSDecision, error) {
	return s.evaluate(sub, item, false, false)
}

func (s *Service) evaluate(sub *model.Subscription, item RSSResource, baseline, mutate bool) (RSSDecision, error) {
	relativeEpisode := sub.RelativeEpisode(item.OriginalEpisode)
	if relativeEpisode <= 0 {
		return RSSDecision{Action: DecisionSkip, Reason: "non_positive_relative_episode"}, nil
	}

	ledger, err := s.repository.GetBySubscriptionAndEpisode(sub.ID, relativeEpisode)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return RSSDecision{}, err
	}
	if err == nil && ledger.Status == model.EpisodeStatusIgnored {
		return RSSDecision{Action: DecisionIgnored, EpisodeID: ledger.ID, Reason: "episode_ignored"}, nil
	}

	resourceKey := ResourceKey(item.Resource)
	if resourceKey == "" {
		return RSSDecision{Action: DecisionSkip, Reason: "resource_identity_missing"}, nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) || ledger.Status == model.EpisodeStatusMissing {
		if baseline {
			if !mutate {
				return RSSDecision{Action: DecisionBaseline, Reason: "baseline_observed"}, nil
			}
			observed, observeErr := s.repository.ObserveEpisode(sub.ID, relativeEpisode)
			if observeErr != nil {
				return RSSDecision{}, observeErr
			}
			return RSSDecision{Action: DecisionBaseline, EpisodeID: observed.ID, Reason: "baseline_observed"}, nil
		}
		if !mutate {
			episodeID := uint(0)
			if ledger != nil {
				episodeID = ledger.ID
			}
			return RSSDecision{Action: DecisionDownload, EpisodeID: episodeID, Reason: "episode_missing"}, nil
		}
		claimedEpisode, claimed, claimErr := s.repository.ClaimForDownload(sub.ID, relativeEpisode, item.Resource)
		if claimErr != nil {
			return RSSDecision{}, claimErr
		}
		if claimed {
			return RSSDecision{Action: DecisionDownload, EpisodeID: claimedEpisode.ID, Reason: "episode_missing"}, nil
		}
		ledger = claimedEpisode
	}

	switch ledger.Status {
	case model.EpisodeStatusIgnored:
		return RSSDecision{Action: DecisionIgnored, EpisodeID: ledger.ID, Reason: "episode_ignored"}, nil
	case model.EpisodeStatusDownloading, model.EpisodeStatusDownloaded, model.EpisodeStatusMarkedDownloaded:
		if sameResource(episodeResource(ledger), item.Resource) {
			return RSSDecision{Action: DecisionSkip, EpisodeID: ledger.ID, Reason: "resource_already_known"}, nil
		}
		if !mutate {
			return RSSDecision{Action: DecisionCandidate, EpisodeID: ledger.ID, Reason: "different_resource"}, nil
		}
		candidate := &model.EpisodeResourceCandidate{
			ResourceKey:  resourceKey,
			TorrentHash:  item.Resource.Hash,
			TorrentURL:   item.Resource.URL,
			Title:        item.Resource.Title,
			Fansub:       item.Fansub,
			Language:     item.Language,
			SourceRSSURL: item.SourceRSSURL,
			Status:       model.CandidateStatusPending,
		}
		if !item.PubTime.IsZero() {
			pubTime := item.PubTime
			candidate.PubTime = &pubTime
		}
		persisted, _, candidateErr := s.repository.UpsertCandidate(ledger.ID, candidate)
		if candidateErr != nil {
			return RSSDecision{}, candidateErr
		}
		return RSSDecision{
			Action:      DecisionCandidate,
			EpisodeID:   ledger.ID,
			CandidateID: persisted.ID,
			Reason:      "different_resource",
		}, nil
	default:
		return RSSDecision{Action: DecisionSkip, EpisodeID: ledger.ID, Reason: "unsupported_episode_status"}, nil
	}
}

func (s *Service) AttachDownload(episodeID, downloadID uint) error {
	return s.repository.AttachDownload(episodeID, downloadID)
}

func (s *Service) AttachDownloadInTx(tx *gorm.DB, episodeID, downloadID uint) error {
	return s.repository.AttachDownloadInTx(tx, episodeID, downloadID)
}

func (s *Service) ReleaseDownloadClaim(episodeID uint, resource model.EpisodeResource) error {
	return s.repository.ReleaseDownloadClaim(episodeID, resource)
}

func (s *Service) MarkDownloadCompleted(download *model.Download, sub *model.Subscription, completedAt time.Time) error {
	ledger, resource, err := s.downloadCompletionDetails(s.repository.GetBySubscriptionAndEpisode, download, sub)
	if err != nil {
		return err
	}
	return s.repository.MarkDownloaded(ledger.ID, download.ID, resource, completedAt)
}

func (s *Service) MarkDownloadCompletedInTx(tx *gorm.DB, download *model.Download, sub *model.Subscription, completedAt time.Time) error {
	getEpisode := func(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error) {
		var ledger model.SubscriptionEpisode
		err := tx.Where("subscription_id = ? AND episode = ?", subscriptionID, episode).First(&ledger).Error
		return &ledger, err
	}
	ledger, resource, err := s.downloadCompletionDetails(getEpisode, download, sub)
	if err != nil {
		return err
	}
	return s.repository.MarkDownloadedInTx(tx, ledger.ID, download.ID, resource, completedAt)
}

func (s *Service) MarkDownloadFailed(downloadID uint) error {
	return s.repository.MarkMissingIfActiveDownload(downloadID)
}

func (s *Service) DetachDownload(downloadID uint) error {
	return s.repository.DetachDownload(downloadID)
}

func (s *Service) EnsureRange(subscriptionID uint, totalEpisodes int) error {
	return s.repository.EnsureRange(subscriptionID, totalEpisodes)
}

func (s *Service) RefreshSubscriptionProgress(subscriptionID uint) error {
	return s.repository.RefreshSubscriptionProgress(subscriptionID)
}

func episodeResource(episode *model.SubscriptionEpisode) model.EpisodeResource {
	return model.EpisodeResource{
		Hash:  episode.ActiveTorrentHash,
		URL:   episode.ActiveTorrentURL,
		Title: episode.ActiveTitle,
	}
}

func sameResource(current, candidate model.EpisodeResource) bool {
	currentHash := strings.TrimSpace(current.Hash)
	candidateHash := strings.TrimSpace(candidate.Hash)
	if currentHash != "" || candidateHash != "" {
		return currentHash != "" && candidateHash != "" && strings.EqualFold(currentHash, candidateHash)
	}
	currentURL := strings.TrimSpace(current.URL)
	candidateURL := strings.TrimSpace(candidate.URL)
	return currentURL != "" && candidateURL != "" && currentURL == candidateURL
}

func (s *Service) downloadCompletionDetails(
	getEpisode func(uint, int) (*model.SubscriptionEpisode, error),
	download *model.Download,
	sub *model.Subscription,
) (*model.SubscriptionEpisode, model.EpisodeResource, error) {
	if download == nil {
		return nil, model.EpisodeResource{}, errors.New("download is required")
	}
	if sub == nil {
		return nil, model.EpisodeResource{}, errors.New("subscription is required")
	}
	relativeEpisode := sub.RelativeEpisode(download.Episode)
	if relativeEpisode <= 0 {
		return nil, model.EpisodeResource{}, fmt.Errorf("download episode %d is outside subscription range", download.Episode)
	}
	ledger, err := getEpisode(sub.ID, relativeEpisode)
	if err != nil {
		return nil, model.EpisodeResource{}, err
	}
	return ledger, model.EpisodeResource{
		Hash:  download.TorrentHash,
		URL:   download.TorrentURL,
		Title: download.Title,
	}, nil
}
