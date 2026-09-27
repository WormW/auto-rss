package subscription

import (
	"context"
	"errors"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/subscriptionfeed"
	"gorm.io/gorm"
)

var ErrDuplicateInitialFeed = errors.New("duplicate initial subscription feed")

type Creator interface {
	Create(ctx context.Context, sub *model.Subscription, feeds []subscriptionfeed.Input) error
}

type creator struct {
	db          *gorm.DB
	feedService *subscriptionfeed.Service
	episodeRepo repository.EpisodeRepository
}

func NewCreator(
	db *gorm.DB,
	feedService *subscriptionfeed.Service,
	episodeRepos ...repository.EpisodeRepository,
) Creator {
	var episodeRepo repository.EpisodeRepository
	if len(episodeRepos) > 0 {
		episodeRepo = episodeRepos[0]
	}
	return &creator{db: db, feedService: feedService, episodeRepo: episodeRepo}
}

func (c *creator) Create(
	ctx context.Context,
	sub *model.Subscription,
	feeds []subscriptionfeed.Input,
) error {
	if sub == nil {
		return errors.New("subscription is required")
	}
	prepared := make([]subscriptionfeed.Prepared, 0, len(feeds))
	seenURLs := make(map[string]struct{}, len(feeds))
	for _, input := range feeds {
		feed, err := c.feedService.Prepare(ctx, input)
		if err != nil {
			return err
		}
		if _, exists := seenURLs[feed.Feed.RSSURLNormalized]; exists {
			return ErrDuplicateInitialFeed
		}
		seenURLs[feed.Feed.RSSURLNormalized] = struct{}{}
		prepared = append(prepared, feed)
	}

	return c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return persistPrepared(tx, sub, prepared, c.episodeRepo, c.feedService)
	})
}

// CreatePreparedInTx lets a reviewed draft and its subscription commit atomically.
// Callers must prepare/validate feeds before entering the transaction.
func CreatePreparedInTx(ctx context.Context, tx *gorm.DB, sub *model.Subscription, prepared []subscriptionfeed.Prepared) error {
	if sub == nil || len(prepared) == 0 {
		return errors.New("subscription and prepared feeds are required")
	}
	feeds := subscriptionfeed.NewService(tx, repository.NewSubscriptionFeedRepository(tx), nil)
	return persistPrepared(tx.WithContext(ctx), sub, prepared, repository.NewEpisodeRepository(tx), feeds)
}

func persistPrepared(tx *gorm.DB, sub *model.Subscription, prepared []subscriptionfeed.Prepared, episodeRepo repository.EpisodeRepository, feeds *subscriptionfeed.Service) error {
	if len(prepared) > 0 {
		first := prepared[0].Feed
		sub.RssURL = first.RSSURL
		sub.Fansub = first.Fansub
		sub.EpisodeOffset = first.EpisodeOffset
		sub.LastRSSPubTime = nil
		sub.RSSBaselinePending = false
		if strings.TrimSpace(sub.SourceType) == "" || sub.SourceType == "calendar" {
			sub.SourceType = "manual"
		}
	}

	if err := tx.Create(sub).Error; err != nil {
		return err
	}
	for _, feed := range prepared {
		if _, err := feeds.CreatePreparedInTx(tx, sub.ID, feed); err != nil {
			return err
		}
	}
	if episodeRepo != nil && sub.TotalEpisodes > 0 {
		return episodeRepo.EnsureRangeInTx(tx, sub.ID, sub.TotalEpisodes)
	}
	return nil
}
