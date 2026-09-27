package subscriptiondiscovery

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/database"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fixtureProvider struct {
	items      []rss.RSSItem
	fetchError error
}

func (f *fixtureProvider) Search(context.Context, PrepareInput) ([]Subject, error) {
	return []Subject{{ID: 42, Name: "Ledger Anime", Aliases: []string{"Ledger Anime"}}, {ID: 43, Name: "Ledger Anime 2", Aliases: []string{"Ledger Anime 2"}}}, nil
}
func (f *fixtureProvider) Subject(context.Context, int) (Subject, error) {
	return Subject{ID: 42, Name: "Ledger Anime", Aliases: []string{"Ledger Anime"}, Episodes: 12, Date: "2026-07-01"}, nil
}
func (f *fixtureProvider) Feeds(context.Context, Subject, PrepareInput) ([]Feed, []string, error) {
	return []Feed{{ID: "feed-a", Source: "nyaa", URL: "https://nyaa.si/?page=rss&q=Ledger+Anime"}}, nil, nil
}
func (f *fixtureProvider) Fetch(context.Context, Feed) ([]rss.RSSItem, error) {
	return f.items, f.fetchError
}

func newDiscoveryFixture(t *testing.T) (*Service, *gorm.DB, *fixtureProvider) {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "discovery.db"))
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	provider := &fixtureProvider{items: []rss.RSSItem{{Title: "[Group] Ledger Anime - 01 [1080p]", Episode: 1, TorrentURL: "https://example.test/1.torrent"}, {Title: "[Group] Other Anime - 01 [1080p]", Episode: 1, TorrentURL: "https://example.test/2.torrent"}, {Title: "[Group] Ledger Anime - 02 [720p]", Episode: 2, TorrentURL: "https://example.test/3.torrent"}}}
	return New(db, provider), db, provider
}

func TestDiscoveryRequiresReviewedSubjectAndChoiceBeforeCreating(t *testing.T) {
	s, db, _ := newDiscoveryFixture(t)
	ctx := context.Background()
	input := PrepareInput{Query: "Ledger Anime", Resolution: 1080}
	search, err := s.Prepare(ctx, input)
	require.NoError(t, err)
	require.Len(t, search.Candidates, 2)
	require.Empty(t, search.Feeds)
	require.Equal(t, "needs_subject_selection", search.Status)
	_, err = s.Confirm(ctx, ConfirmInput{search.ID, search.Revision, "feed-a"})
	require.ErrorIs(t, err, ErrNotReady)
	input.BangumiID = 42
	input.DraftID = search.ID
	input.Revision = search.Revision
	draft, err := s.Prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, 2, draft.Revision)
	require.Equal(t, "any", draft.Intent.Language)
	require.Equal(t, "future_only", draft.StartPolicy)
	require.Equal(t, 1, draft.Feeds[0].Matched)
	require.Equal(t, 1, draft.Feeds[0].Rejected)
	require.Equal(t, 1, draft.Feeds[0].NeedsReview)
	for _, table := range []string{"subscriptions", "subscription_feeds", "downloads"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		require.Zero(t, count, "prepare must not mutate business state")
	}
	_, err = s.Confirm(ctx, ConfirmInput{draft.ID, 1, "feed-a"})
	require.ErrorIs(t, err, ErrConflict)
	sub, err := s.Confirm(ctx, ConfirmInput{draft.ID, draft.Revision, "feed-a"})
	require.NoError(t, err)
	require.Equal(t, 42, sub.BangumiID)
	repeated, err := s.Confirm(ctx, ConfirmInput{draft.ID, draft.Revision, "feed-a"})
	require.NoError(t, err)
	require.Equal(t, sub.ID, repeated.ID)
	_, err = s.Confirm(ctx, ConfirmInput{draft.ID, draft.Revision, "another-feed"})
	require.ErrorIs(t, err, ErrConflict)
	var feeds []model.SubscriptionFeed
	require.NoError(t, db.Find(&feeds).Error)
	require.Len(t, feeds, 1)
	require.True(t, feeds[0].BaselinePending)
	var episodes, downloads int64
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Count(&episodes).Error)
	require.EqualValues(t, 12, episodes)
	require.NoError(t, db.Model(&model.Download{}).Count(&downloads).Error)
	require.Zero(t, downloads)
	rules, err := subscriptionmatch.Decode(sub.DiscoveryRules)
	require.NoError(t, err)
	require.Equal(t, 1080, rules.Resolution)
	input.DraftID = ""
	input.Revision = 0
	another, err := s.Prepare(ctx, input)
	require.NoError(t, err)
	_, err = s.Confirm(ctx, ConfirmInput{another.ID, another.Revision, "feed-a"})
	require.ErrorContains(t, err, "already tracks")
	var saved model.SubscriptionDraft
	require.NoError(t, db.First(&saved, "id = ?", another.ID).Error)
	require.Nil(t, saved.SubscriptionID, "failed transaction must not consume draft")
}

func TestUnverifiedExpiredAndChangedFeedsCannotBeConfirmed(t *testing.T) {
	for _, failure := range []string{"empty", "unavailable", "expired", "changed"} {
		t.Run(failure, func(t *testing.T) {
			s, db, provider := newDiscoveryFixture(t)
			if failure == "empty" {
				provider.items = nil
			}
			if failure == "unavailable" {
				provider.fetchError = errors.New("upstream unavailable")
			}
			draft, err := s.Prepare(context.Background(), PrepareInput{Query: "Ledger Anime", BangumiID: 42, Resolution: 1080})
			require.NoError(t, err)
			if failure == "expired" {
				require.NoError(t, db.Model(&model.SubscriptionDraft{}).Where("id = ?", draft.ID).Update("expires_at", time.Now().Add(-time.Second)).Error)
			}
			if failure == "changed" {
				provider.items = nil
			}
			_, err = s.Confirm(context.Background(), ConfirmInput{draft.ID, draft.Revision, "feed-a"})
			require.Error(t, err)
			var count int64
			require.NoError(t, db.Model(&model.Subscription{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestConcurrentConfirmationIsIdempotent(t *testing.T) {
	s, db, _ := newDiscoveryFixture(t)
	draft, err := s.Prepare(context.Background(), PrepareInput{Query: "Ledger Anime", BangumiID: 42, Resolution: 1080})
	require.NoError(t, err)
	input := ConfirmInput{draft.ID, draft.Revision, "feed-a"}
	results := make(chan uint, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub, e := s.Confirm(context.Background(), input)
			if e != nil {
				errs <- e
				return
			}
			results <- sub.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for e := range errs {
		require.NoError(t, e)
	}
	var ids []uint
	for id := range results {
		ids = append(ids, id)
	}
	require.Len(t, ids, 2)
	require.Equal(t, ids[0], ids[1])
	var count int64
	require.NoError(t, db.Model(&model.Subscription{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

type timeoutProvider struct{ fixtureProvider }

func (p *timeoutProvider) Feeds(context.Context, Subject, PrepareInput) ([]Feed, []string, error) {
	return []Feed{{ID: "good", Source: "nyaa", URL: "https://nyaa.si/?page=rss&q=good"}, {ID: "slow", Source: "nyaa", URL: "https://nyaa.si/?page=rss&q=slow"}}, nil, nil
}
func (p *timeoutProvider) Fetch(ctx context.Context, feed Feed) ([]rss.RSSItem, error) {
	if feed.ID == "slow" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.items, nil
}
func TestSlowSourceDoesNotDiscardAlreadyVerifiedDraftResults(t *testing.T) {
	s, db, provider := newDiscoveryFixture(t)
	s.provider = &timeoutProvider{*provider}
	s.prepareTimeout = 40 * time.Millisecond
	draft, err := s.Prepare(context.Background(), PrepareInput{Query: "Ledger Anime", BangumiID: 42, Resolution: 1080})
	require.NoError(t, err)
	require.True(t, draft.Feeds[0].Confirmable)
	require.False(t, draft.Feeds[1].Confirmable)
	require.Equal(t, "source_unavailable", draft.Feeds[1].Status)
	require.NotEmpty(t, draft.Warnings)
	var saved model.SubscriptionDraft
	require.NoError(t, db.First(&saved, "id = ?", draft.ID).Error)
}

func TestDefaultQualityPolicyIsReviewedAndPersisted(t *testing.T) {
	s, db, provider := newDiscoveryFixture(t)
	provider.items = append([]rss.RSSItem{{Title: "Ledger Anime - 01 [2160p]", Episode: 1, TorrentURL: "https://example.test/4k.torrent"}}, provider.items...)
	draft, err := s.Prepare(context.Background(), PrepareInput{Query: "Ledger Anime", BangumiID: 42})
	require.NoError(t, err)
	require.Equal(t, subscriptionmatch.DefaultQualityPolicy(), draft.Intent.QualityPolicy)
	preview := draft.Feeds[0]
	require.Equal(t, 1, preview.Matched)
	require.Equal(t, 2, preview.Rejected)
	require.Equal(t, 1, preview.NeedsReview)
	require.Contains(t, preview.Samples[0].Title, "1080p")
	var count int64
	require.NoError(t, db.Model(&model.Subscription{}).Count(&count).Error)
	require.Zero(t, count)
	sub, err := s.Confirm(context.Background(), ConfirmInput{draft.ID, draft.Revision, preview.Feed.ID})
	require.NoError(t, err)
	rules, err := subscriptionmatch.Decode(sub.DiscoveryRules)
	require.NoError(t, err)
	require.Equal(t, preview.Rules, rules)
	require.Equal(t, 2, rules.Version)
}

func TestExplicitQualityChoiceOverridesDefaults(t *testing.T) {
	input := PrepareInput{Query: "Anime", Resolution: 720}
	require.NoError(t, normalizeInput(&input))
	require.Nil(t, input.QualityPolicy, "an explicit hard resolution overrides implicit defaults")
	input = PrepareInput{Query: "Anime", QualityPolicy: &subscriptionmatch.QualityPolicy{ExcludedResolutions: []int{480, 720}, PreferredResolution: 1080, Fallback: "allow"}}
	require.NoError(t, normalizeInput(&input))
	require.Equal(t, "allow", input.QualityPolicy.Fallback)
	require.Equal(t, []int{480, 720}, input.QualityPolicy.ExcludedResolutions)
	input.Resolution = 720
	require.ErrorContains(t, normalizeInput(&input), "conflicting")
}
