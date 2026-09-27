package subscriptiondiscovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscription"
	"github.com/WormW/auto-rss/internal/service/subscriptionfeed"
	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
	"gorm.io/gorm"
)

var ErrConflict = errors.New("draft changed, expired, or was already confirmed with another choice; prepare and review again")
var ErrNotReady = errors.New("select a verified subject and a feed with matching samples before confirming")

type Service struct {
	db             *gorm.DB
	provider       Provider
	gate           chan struct{}
	prepareTimeout time.Duration
}

func New(db *gorm.DB, provider Provider) *Service {
	return &Service{db: db, provider: provider, gate: make(chan struct{}, 2), prepareTimeout: 45 * time.Second}
}

func (s *Service) Prepare(ctx context.Context, input PrepareInput) (Draft, error) {
	if s.db == nil || s.provider == nil {
		return Draft{}, fmt.Errorf("discovery is unavailable")
	}
	if err := normalizeInput(&input); err != nil {
		return Draft{}, err
	}
	requestCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, s.prepareTimeout)
	defer cancel()
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return Draft{}, ctx.Err()
	}
	if input.DraftID != "" {
		var existing model.SubscriptionDraft
		if err := s.db.WithContext(ctx).First(&existing, "id = ?", input.DraftID).Error; err != nil {
			return Draft{}, err
		}
		if existing.Revision != input.Revision || existing.SubscriptionID != nil || time.Now().After(existing.ExpiresAt) {
			return Draft{}, ErrConflict
		}
	}
	draft := Draft{Status: "needs_subject_selection", Intent: input, Candidates: []Subject{}, Feeds: []FeedPreview{}, Warnings: []string{}, StartPolicy: "future_only", ExpiresAt: time.Now().Add(30 * time.Minute)}
	if input.BangumiID == 0 {
		candidates, err := s.provider.Search(ctx, input)
		if err != nil {
			return Draft{}, err
		}
		draft.Candidates = candidates
		if len(candidates) == 0 {
			draft.Status = "no_subject_found"
		}
		draft.Warnings = append(draft.Warnings, "搜索排序不能证明季数；先选定动画条目，再预览资源。")
	} else {
		subject, err := s.provider.Subject(ctx, input.BangumiID)
		if err != nil {
			return Draft{}, err
		}
		draft.Subject = &subject
		draft.Status = "needs_resource_review"
		if subject.SeasonHint != 0 && subject.SeasonHint != input.Season {
			draft.Warnings = append(draft.Warnings, "条目名称的季数与请求不同，请核对是否为媒体库编号映射。")
		}
		if input.Year > 0 && !strings.HasPrefix(subject.Date, fmt.Sprintf("%d-", input.Year)) {
			draft.Warnings = append(draft.Warnings, "选定条目的开播年份与请求不同。")
		}
		if subject.Episodes == 0 {
			draft.Warnings = append(draft.Warnings, "总集数未知，不自动推定。")
		}
		feeds, warnings, err := s.provider.Feeds(ctx, subject, input)
		if err != nil {
			return Draft{}, err
		}
		draft.Warnings = append(draft.Warnings, warnings...)
		for _, feed := range feeds {
			rules := subscriptionmatch.Rules{Version: 2, Titles: subject.Aliases, Season: input.Season, ScopedFeed: feed.Source == "mikan" && feed.BangumiID == subject.ID, ScopedFeedURL: feed.URL, EpisodeOffset: input.EpisodeOffset, TotalEpisodes: subject.Episodes, Language: input.Language, Resolution: input.Resolution, QualityPolicy: input.QualityPolicy, Fansub: input.Fansub, Required: input.RequiredKeywords, Excluded: input.ExcludeKeywords, TrustedOnly: feed.Source == "nyaa" && input.TrustedOnly}
			if err := rules.Validate(); err != nil {
				return Draft{}, err
			}
			items, fetchErr := s.provider.Fetch(ctx, feed)
			preview := makePreview(feed, rules, items, subject)
			if fetchErr != nil {
				preview.Status = "source_unavailable"
				preview.Confirmable = false
				preview.Warnings = []string{fetchErr.Error()}
			}
			draft.Feeds = append(draft.Feeds, preview)
		}
		draft.Warnings = append(draft.Warnings, "仅验证当前有限 RSS 样本，不代表全集覆盖。确认后只追新，历史补集需单独指定。")
	}
	if requestCtx.Err() != nil {
		return Draft{}, requestCtx.Err()
	}
	if ctx.Err() != nil {
		draft.Warnings = append(draft.Warnings, "查询时间预算已用完；已取得的候选和预览已保留，其余来源可稍后重新准备。")
	}
	saveCtx, saveCancel := context.WithTimeout(requestCtx, 5*time.Second)
	defer saveCancel()
	if err := s.save(saveCtx, &draft, input); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func normalizeInput(input *PrepareInput) error {
	input.Query = strings.TrimSpace(input.Query)
	input.Fansub = strings.TrimSpace(input.Fansub)
	input.NyaaQuery = strings.TrimSpace(input.NyaaQuery)
	input.NyaaUploader = strings.TrimSpace(input.NyaaUploader)
	input.DmhyQuery = strings.TrimSpace(input.DmhyQuery)
	if len(input.Query) < 2 || len(input.Query) > 300 || len(input.NyaaQuery) > 500 || len(input.DmhyQuery) > 500 || len(input.NyaaUploader) > 100 || input.BangumiID < 0 || input.DmhyTeamID < 0 || input.Year < 0 || (input.Year != 0 && (input.Year < 1900 || input.Year > 2200)) {
		return fmt.Errorf("invalid discovery query or year")
	}
	if (input.DraftID == "") != (input.Revision == 0) || len(input.DraftID) > 64 {
		return fmt.Errorf("draft_id and revision must be supplied together")
	}
	if input.Season == 0 {
		input.Season = 1
	}
	if input.Language == "" {
		input.Language = "any"
	}
	if input.QualityPolicy == nil && input.Resolution == 0 {
		input.QualityPolicy = subscriptionmatch.DefaultQualityPolicy()
	}
	if len(input.Sources) == 0 {
		input.Sources = []string{"mikan", "nyaa", "dmhy"}
	}
	seen := map[string]bool{}
	for _, source := range input.Sources {
		if (source != "mikan" && source != "nyaa" && source != "dmhy") || seen[source] {
			return fmt.Errorf("sources must contain mikan, nyaa and/or dmhy without duplicates")
		}
		seen[source] = true
	}
	if input.DmhyTeamID > 0 && !seen["dmhy"] {
		return fmt.Errorf("dmhy_team_id requires dmhy in sources")
	}
	rules := subscriptionmatch.Rules{Version: 2, Titles: []string{input.Query}, Season: input.Season, EpisodeOffset: input.EpisodeOffset, Language: input.Language, Resolution: input.Resolution, QualityPolicy: input.QualityPolicy, Fansub: input.Fansub, Required: input.RequiredKeywords, Excluded: input.ExcludeKeywords}
	return rules.Validate()
}

func makePreview(feed Feed, rules subscriptionmatch.Rules, items []rss.RSSItem, subject Subject) FeedPreview {
	preview := FeedPreview{Feed: feed, Rules: rules, Samples: []Sample{}, MatchedEpisodes: []int{}, Status: "no_matching_samples", CheckedAt: time.Now()}
	episodes := map[int]bool{}
	groups := map[string]*GroupCandidate{}
	groupEpisodes := map[string]map[int]bool{}
	groupMatchedEpisodes := map[string]map[int]bool{}
	if len(items) == 0 {
		preview.Status = "empty_feed"
		preview.Warnings = append(preview.Warnings, "当前 RSS 为空，不能据此验证匹配效果。")
	}
	if len(items) >= 100 {
		preview.Warnings = append(preview.Warnings, "最多检查前 100 条；RSS 本身也可能截断。")
	}
	if len(items) > 100 {
		items = items[:100]
	}
	preview.Fetched = len(items)
	name := subject.NameCN
	if name == "" {
		name = subject.Name
	}
	name, _ = utils.NormalizeMediaTitleAndSeason(name, rules.Season)
	for _, item := range rules.Prioritize(items) {
		decision := rules.Match(item)
		if item.TorrentURL == "" {
			decision = subscriptionmatch.Decision{Action: "review", Reason: "missing_resource_url"}
		}
		switch decision.Action {
		case "match":
			preview.Matched++
			episodes[item.Episode-rules.EpisodeOffset] = true
		case "reject":
			preview.Rejected++
		default:
			preview.NeedsReview++
		}
		groupID := item.SourceGroupID
		groupName := strings.TrimSpace(item.SourceGroupName)
		if feed.Source == "dmhy" && groupName == "" {
			groupName = strings.TrimSpace(item.Fansub)
		}
		if feed.Source == "dmhy" && (groupID != "" || groupName != "") {
			groupKey := groupID + "\x00" + strings.ToLower(groupName)
			group := groups[groupKey]
			if group == nil {
				group = &GroupCandidate{Name: groupName, Samples: []string{}, Evidence: "dmhy_topic_team_link"}
				if parsed, err := strconv.Atoi(groupID); err == nil && parsed > 0 {
					group.ID = parsed
				}
				groups[groupKey] = group
				groupEpisodes[groupKey] = map[int]bool{}
				groupMatchedEpisodes[groupKey] = map[int]bool{}
			}
			group.ItemCount++
			if item.Episode > 0 {
				groupEpisodes[groupKey][item.Episode-rules.EpisodeOffset] = true
			}
			if decision.Action == "match" {
				group.MatchedItems++
				if item.Episode > 0 {
					groupMatchedEpisodes[groupKey][item.Episode-rules.EpisodeOffset] = true
				}
			}
			if len(group.Samples) < 3 {
				example := item.Title
				if runes := []rune(example); len(runes) > 280 {
					example = string(runes[:280])
				}
				group.Samples = append(group.Samples, example)
			}
		}
		sample := Sample{Title: item.Title, Episode: item.Episode, RelativeEpisode: item.Episode - rules.EpisodeOffset, Decision: decision, CategoryID: item.CategoryID, Trusted: item.Trusted}
		if len(sample.Title) > 700 {
			sample.Title = string([]rune(sample.Title)[:min(180, len([]rune(sample.Title)))])
		}
		if decision.Action == "match" {
			sample.RenamePreview = downloader.NewRenameService("").GenerateFileName(&downloader.RenameContext{Subscription: &model.Subscription{Name: name, Season: rules.Season, EpisodeOffset: rules.EpisodeOffset}, Download: &model.Download{Episode: item.Episode - rules.EpisodeOffset}, OriginalName: item.Title, Extension: ".mkv"})
		}
		// Include examples of every decision class, not just the first few matches.
		count := 0
		for _, existing := range preview.Samples {
			if existing.Decision.Action == decision.Action {
				count++
			}
		}
		if count < 4 {
			preview.Samples = append(preview.Samples, sample)
		}
	}
	for key, group := range groups {
		for episode := range groupEpisodes[key] {
			if episode > 0 {
				group.Episodes = append(group.Episodes, episode)
			}
		}
		for episode := range groupMatchedEpisodes[key] {
			if episode > 0 {
				group.MatchedEpisodes = append(group.MatchedEpisodes, episode)
			}
		}
		sort.Ints(group.Episodes)
		sort.Ints(group.MatchedEpisodes)
		preview.GroupCandidates = append(preview.GroupCandidates, *group)
	}
	sort.Slice(preview.GroupCandidates, func(i, j int) bool {
		if preview.GroupCandidates[i].MatchedItems != preview.GroupCandidates[j].MatchedItems {
			return preview.GroupCandidates[i].MatchedItems > preview.GroupCandidates[j].MatchedItems
		}
		return preview.GroupCandidates[i].Name < preview.GroupCandidates[j].Name
	})
	if len(preview.GroupCandidates) > 32 {
		preview.GroupCandidates = preview.GroupCandidates[:32]
		preview.Warnings = append(preview.Warnings, "字幕组候选超过 32 个，预览只展示覆盖度最高的团队。")
	}
	if feed.Source == "dmhy" && len(items) > 0 && len(preview.GroupCandidates) == 0 {
		preview.Warnings = append(preview.Warnings, "动漫花园搜索页没有提供可关联的 team_id；可继续复核资源，但不能自动生成字幕组窄查询。")
	}
	if preview.Matched > 0 {
		for episode := range episodes {
			preview.MatchedEpisodes = append(preview.MatchedEpisodes, episode)
		}
		sort.Ints(preview.MatchedEpisodes)
		preview.DuplicateEpisodeItems = preview.Matched - len(preview.MatchedEpisodes)
		preview.Status = "samples_verified"
		preview.Confirmable = true
	}
	return preview
}

func (s *Service) save(ctx context.Context, draft *Draft, input PrepareInput) error {
	if input.DraftID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return err
		}
		draft.ID = hex.EncodeToString(id[:])
		draft.Revision = 1
	} else {
		draft.ID = input.DraftID
		draft.Revision = input.Revision + 1
	}
	draft.Intent.DraftID = ""
	draft.Intent.Revision = 0
	payload, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	if len(payload) > 128<<10 {
		return fmt.Errorf("draft exceeds size limit")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.DraftID != "" {
			result := tx.Model(&model.SubscriptionDraft{}).Where("id = ? AND revision = ? AND subscription_id IS NULL AND expires_at > ?", input.DraftID, input.Revision, time.Now()).Updates(map[string]any{"revision": draft.Revision, "payload": string(payload), "expires_at": draft.ExpiresAt})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrConflict
			}
			return nil
		}
		// Retain compact confirmed rows for idempotency; bound outstanding previews.
		if err := tx.Where("subscription_id IS NULL AND expires_at < ?", time.Now()).Delete(&model.SubscriptionDraft{}).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.SubscriptionDraft{}).Where("subscription_id IS NULL").Count(&count).Error; err != nil {
			return err
		}
		if count >= 200 {
			return fmt.Errorf("too many outstanding drafts; revise an existing draft or wait for expiry")
		}
		return tx.Create(&model.SubscriptionDraft{ID: draft.ID, Revision: draft.Revision, Payload: string(payload), ExpiresAt: draft.ExpiresAt}).Error
	})
}

func (s *Service) Confirm(ctx context.Context, input ConfirmInput) (*model.Subscription, error) {
	if s.db == nil || s.provider == nil {
		return nil, fmt.Errorf("discovery is unavailable")
	}
	if input.DraftID == "" || input.Revision <= 0 || input.FeedID == "" {
		return nil, ErrNotReady
	}
	var record model.SubscriptionDraft
	if err := s.db.WithContext(ctx).First(&record, "id = ?", input.DraftID).Error; err != nil {
		return nil, err
	}
	if record.Revision != input.Revision {
		return nil, ErrConflict
	}
	if record.SubscriptionID != nil {
		return s.confirmed(ctx, record, input)
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, ErrConflict
	}
	var draft Draft
	if json.Unmarshal([]byte(record.Payload), &draft) != nil || draft.Subject == nil {
		return nil, ErrNotReady
	}
	var choice *FeedPreview
	for i := range draft.Feeds {
		if draft.Feeds[i].Feed.ID == input.FeedID {
			choice = &draft.Feeds[i]
			break
		}
	}
	if choice == nil || !choice.Confirmable {
		return nil, ErrNotReady
	}
	if err := choice.Rules.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, err := s.provider.Fetch(ctx, choice.Feed)
	if err != nil {
		return nil, err
	}
	if !makePreview(choice.Feed, choice.Rules, items, *draft.Subject).Confirmable {
		return nil, fmt.Errorf("feed no longer has verified samples; prepare and review again")
	}
	rulesJSON, _ := json.Marshal(choice.Rules)
	name := draft.Subject.NameCN
	if name == "" {
		name = draft.Subject.Name
	}
	name, _ = utils.NormalizeMediaTitleAndSeason(name, draft.Intent.Season)
	sub := &model.Subscription{Name: name, Season: draft.Intent.Season, BangumiID: draft.Subject.ID, BangumiCover: draft.Subject.Cover, AirDate: draft.Subject.Date, TotalEpisodes: draft.Subject.Episodes, Enabled: true, Status: "active", RenameEnabled: true, LanguagePreference: "auto", DiscoveryRules: string(rulesJSON), DiscoveryDraftID: &record.ID}
	prepared := subscriptionfeed.Prepared{Feed: model.SubscriptionFeed{Name: choice.Feed.Source, Fansub: choice.Feed.Fansub, RSSURL: choice.Feed.URL, RSSURLNormalized: utils.NormalizeFeedURL(choice.Feed.URL), EpisodeOffset: draft.Intent.EpisodeOffset, Enabled: true, BaselinePending: true}}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Claim with a write before reading, so concurrent confirmations cannot both create.
		claim := tx.Model(&model.SubscriptionDraft{}).Where("id = ? AND revision = ? AND subscription_id IS NULL AND expires_at > ?", input.DraftID, input.Revision, time.Now()).Update("confirmed_choice", input.FeedID)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrConflict
		}
		var existing model.Subscription
		err := tx.Where("bangumi_id = ? AND season = ?", sub.BangumiID, sub.Season).First(&existing).Error
		if err == nil {
			return fmt.Errorf("subscription %d already tracks this subject and season; review its feeds instead", existing.ID)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err = subscription.CreatePreparedInTx(ctx, tx, sub, []subscriptionfeed.Prepared{prepared}); err != nil {
			return err
		}
		return tx.Model(&model.SubscriptionDraft{}).Where("id = ?", record.ID).Updates(map[string]any{"subscription_id": sub.ID, "payload": "{}"}).Error
	})
	if errors.Is(err, ErrConflict) {
		if load := s.db.WithContext(ctx).First(&record, "id = ?", input.DraftID).Error; load == nil && record.SubscriptionID != nil {
			return s.confirmed(ctx, record, input)
		}
	}
	return sub, err
}

func (s *Service) confirmed(ctx context.Context, record model.SubscriptionDraft, input ConfirmInput) (*model.Subscription, error) {
	if record.ConfirmedChoice != input.FeedID || record.Revision != input.Revision {
		return nil, ErrConflict
	}
	var sub model.Subscription
	if err := s.db.WithContext(ctx).First(&sub, *record.SubscriptionID).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}
