package subscriptionfeed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
	"gorm.io/gorm"
)

const maxPreviewItems = 50

var (
	ErrInvalidURL         = errors.New("invalid feed URL")
	ErrNegativeOffset     = errors.New("episode offset must be non-negative")
	ErrNoMappableEpisodes = errors.New("feed contains items but none map to a positive relative episode")
)

type FetchError struct {
	Err error
}

func (e *FetchError) Error() string {
	return "fetch feed: " + e.Err.Error()
}

func (e *FetchError) Unwrap() error {
	return e.Err
}

type Input struct {
	Name          string `json:"name"`
	Fansub        string `json:"fansub"`
	RSSURL        string `json:"rss_url"`
	EpisodeOffset int    `json:"episode_offset"`
	Enabled       bool   `json:"enabled"`
}

type PreviewItem struct {
	Title           string `json:"title"`
	OriginalEpisode int    `json:"original_episode"`
	EpisodeOffset   int    `json:"episode_offset"`
	RelativeEpisode int    `json:"relative_episode"`
	Valid           bool   `json:"valid"`
	InvalidReason   string `json:"invalid_reason"`
}

type Preview struct {
	Items       []PreviewItem `json:"items"`
	ParsedItems int           `json:"parsed_items"`
	ValidItems  int           `json:"valid_items"`
	Warning     string        `json:"warning,omitempty"`
}

type Prepared struct {
	Feed    model.SubscriptionFeed
	Preview Preview
}

type Service struct {
	db         *gorm.DB
	repo       repository.SubscriptionFeedRepository
	parser     rss.Parser
	configRepo repository.ConfigRepository
}

func NewService(db *gorm.DB, repo repository.SubscriptionFeedRepository, parser rss.Parser) *Service {
	return &Service{db: db, repo: repo, parser: parser}
}

func NewServiceWithConfig(
	db *gorm.DB,
	repo repository.SubscriptionFeedRepository,
	parser rss.Parser,
	configRepo repository.ConfigRepository,
) *Service {
	return &Service{db: db, repo: repo, parser: parser, configRepo: configRepo}
}

func (s *Service) Preview(ctx context.Context, input Input) (Preview, error) {
	if err := validateInput(input); err != nil {
		return Preview{}, err
	}
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	if err := s.applySystemProxy(); err != nil {
		return Preview{}, err
	}
	items, err := s.parser.FetchAndParse(strings.TrimSpace(input.RSSURL))
	if err != nil {
		return Preview{}, &FetchError{Err: err}
	}
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}

	preview := Preview{ParsedItems: len(items)}
	if len(items) == 0 {
		preview.Warning = "empty_feed"
		return preview, nil
	}
	preview.Items = make([]PreviewItem, 0, min(len(items), maxPreviewItems))
	for _, item := range items {
		relativeEpisode := item.Episode - input.EpisodeOffset
		previewItem := PreviewItem{
			Title:           item.Title,
			OriginalEpisode: item.Episode,
			EpisodeOffset:   input.EpisodeOffset,
			RelativeEpisode: relativeEpisode,
			Valid:           relativeEpisode > 0,
		}
		if previewItem.Valid {
			preview.ValidItems++
		} else {
			previewItem.InvalidReason = "relative_episode_not_positive"
		}
		if len(preview.Items) < maxPreviewItems {
			preview.Items = append(preview.Items, previewItem)
		}
	}
	if preview.ValidItems == 0 {
		return Preview{}, ErrNoMappableEpisodes
	}
	return preview, nil
}

func (s *Service) applySystemProxy() error {
	if s.configRepo == nil {
		return nil
	}

	proxyURL := ""
	config, err := s.configRepo.Get("system_proxy")
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load system proxy: %w", err)
	}
	if config != nil {
		proxyURL = strings.TrimSpace(config.Value)
	}
	if err := s.parser.SetProxy(proxyURL); err != nil {
		return fmt.Errorf("apply system proxy: %w", err)
	}
	return nil
}

func (s *Service) Prepare(ctx context.Context, input Input) (Prepared, error) {
	preview, err := s.Preview(ctx, input)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{
		Feed: model.SubscriptionFeed{
			Name:             defaultFeedName(input.Name),
			Fansub:           strings.TrimSpace(input.Fansub),
			RSSURL:           strings.TrimSpace(input.RSSURL),
			RSSURLNormalized: utils.NormalizeFeedURL(input.RSSURL),
			EpisodeOffset:    input.EpisodeOffset,
			Enabled:          input.Enabled,
			BaselinePending:  true,
		},
		Preview: preview,
	}, nil
}

func (s *Service) Create(ctx context.Context, subscriptionID uint, input Input) (*model.SubscriptionFeed, error) {
	prepared, err := s.Prepare(ctx, input)
	if err != nil {
		return nil, err
	}
	var created *model.SubscriptionFeed
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var createErr error
		created, createErr = s.CreatePreparedInTx(tx, subscriptionID, prepared)
		return createErr
	})
	return created, err
}

func (s *Service) CreatePreparedInTx(
	tx *gorm.DB,
	subscriptionID uint,
	prepared Prepared,
) (*model.SubscriptionFeed, error) {
	feed := prepared.Feed
	feed.ID = 0
	feed.SubscriptionID = subscriptionID
	if err := s.repo.CreateInTx(tx, &feed); err != nil {
		return nil, err
	}
	if err := refreshLegacyProjectionInTx(tx, subscriptionID); err != nil {
		return nil, err
	}
	return &feed, nil
}

func (s *Service) Update(ctx context.Context, feedID uint, input Input) (*model.SubscriptionFeed, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetByID(feedID)
	if err != nil {
		return nil, err
	}
	normalizedURL := utils.NormalizeFeedURL(input.RSSURL)
	mappingChanged := normalizedURL != existing.RSSURLNormalized || input.EpisodeOffset != existing.EpisodeOffset

	updated := *existing
	if mappingChanged {
		prepared, prepareErr := s.Prepare(ctx, input)
		if prepareErr != nil {
			return nil, prepareErr
		}
		updated.Name = prepared.Feed.Name
		updated.Fansub = prepared.Feed.Fansub
		updated.RSSURL = prepared.Feed.RSSURL
		updated.RSSURLNormalized = prepared.Feed.RSSURLNormalized
		updated.EpisodeOffset = prepared.Feed.EpisodeOffset
		updated.Enabled = prepared.Feed.Enabled
		updated.LastRSSPubTime = nil
		updated.BaselinePending = true
	} else {
		updated.Name = defaultFeedName(input.Name)
		updated.Fansub = strings.TrimSpace(input.Fansub)
		updated.RSSURL = strings.TrimSpace(input.RSSURL)
		updated.RSSURLNormalized = normalizedURL
		updated.Enabled = input.Enabled
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if mappingChanged {
			if err := tx.Where("subscription_feed_id = ?", feedID).
				Delete(&model.SubscriptionFeedSeenItem{}).Error; err != nil {
				return err
			}
		}
		if err := s.repo.UpdateInTx(tx, &updated); err != nil {
			return err
		}
		return refreshLegacyProjectionInTx(tx, updated.SubscriptionID)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *Service) Delete(feedID uint) error {
	feed, err := s.repo.GetByID(feedID)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeleteInTx(tx, feedID); err != nil {
			return err
		}
		return refreshLegacyProjectionInTx(tx, feed.SubscriptionID)
	})
}

func validateInput(input Input) error {
	if input.EpisodeOffset < 0 {
		return ErrNegativeOffset
	}
	if utils.NormalizeFeedURL(input.RSSURL) == "" {
		return ErrInvalidURL
	}
	return nil
}

func defaultFeedName(name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return "默认 RSS"
}

func refreshLegacyProjectionInTx(tx *gorm.DB, subscriptionID uint) error {
	var feed model.SubscriptionFeed
	err := tx.Where("subscription_id = ?", subscriptionID).
		Order("created_at ASC, id ASC").First(&feed).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Model(&model.Subscription{}).Where("id = ?", subscriptionID).Updates(map[string]any{
			"rss_url":        "",
			"fansub":         "",
			"episode_offset": 0,
		}).Error
	}
	if err != nil {
		return err
	}
	return tx.Model(&model.Subscription{}).Where("id = ?", subscriptionID).Updates(map[string]any{
		"rss_url":        feed.RSSURL,
		"fansub":         feed.Fansub,
		"episode_offset": feed.EpisodeOffset,
	}).Error
}
