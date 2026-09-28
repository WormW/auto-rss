package dailystatus

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/calendar"
	"gorm.io/gorm"
)

var (
	ErrInvalidDate     = errors.New("invalid date")
	ErrInvalidTimezone = errors.New("invalid timezone")
)

// Service assembles the read-only daily view used by REST and external agents.
// It keeps the meaning of each state in one place instead of making callers
// combine subscription, feed, download, and episode tables themselves.
type Service struct {
	db               *gorm.DB
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	now              func() time.Time
}

// New creates a daily status service.
func New(db *gorm.DB, subscriptionRepo repository.SubscriptionRepository, downloadRepo repository.DownloadRepository) *Service {
	return &Service{
		db:               db,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		now:              time.Now,
	}
}

// Status is a point-in-time, read-only summary for one civil day.
type Status struct {
	Date            string                  `json:"date"`
	Timezone        string                  `json:"timezone"`
	StartAt         time.Time               `json:"start_at"`
	EndAt           time.Time               `json:"end_at"`
	Notes           []string                `json:"notes"`
	DueToday        []calendar.CalendarItem `json:"due_today"`
	CheckedToday    []CheckedSubscription   `json:"checked_today"`
	CollectedToday  []CollectedSubscription `json:"collected_today"`
	DownloadedToday []DownloadedItem        `json:"downloaded_today"`
	Errors          []DailyError            `json:"errors"`
}

type FeedCheck struct {
	FeedID          uint       `json:"feed_id"`
	Name            string     `json:"name"`
	Fansub          string     `json:"fansub,omitempty"`
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	SuccessAt       *time.Time `json:"success_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	BaselinePending bool       `json:"baseline_pending"`
}

type CheckedSubscription struct {
	SubscriptionID uint        `json:"subscription_id"`
	Name           string      `json:"name"`
	FeedsChecked   int         `json:"feeds_checked"`
	FeedsSucceeded int         `json:"feeds_succeeded"`
	FeedsFailed    int         `json:"feeds_failed"`
	Feeds          []FeedCheck `json:"feeds"`
}

// CollectedSubscription means processing created a download task or a
// reviewable replacement candidate on that day. It does not mean the file is
// already complete; use downloaded_today for that state.
type CollectedSubscription struct {
	SubscriptionID  uint       `json:"subscription_id"`
	Name            string     `json:"name"`
	DownloadCount   int        `json:"download_count"`
	CandidateCount  int        `json:"candidate_count"`
	Episodes        []int      `json:"episodes"`
	LatestCollected *time.Time `json:"latest_collected,omitempty"`
}

type DownloadedItem struct {
	ID              uint       `json:"id"`
	SubscriptionID  uint       `json:"subscription_id"`
	Name            string     `json:"name"`
	Episode         int        `json:"episode"`
	OriginalEpisode int        `json:"original_episode,omitempty"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	RenamedPath     string     `json:"renamed_path,omitempty"`
	DownloadedAt    *time.Time `json:"downloaded_at"`
}

type DailyError struct {
	Kind           string     `json:"kind"`
	SubscriptionID uint       `json:"subscription_id"`
	Name           string     `json:"name"`
	FeedID         uint       `json:"feed_id,omitempty"`
	DownloadID     uint       `json:"download_id,omitempty"`
	At             *time.Time `json:"at,omitempty"`
	Message        string     `json:"message"`
}

type candidateRow struct {
	SubscriptionID uint
	Name           string
	Episode        int
	CreatedAt      time.Time
	Status         string
	FailureReason  string
}

// Get returns the status for date in timezone. Empty date means today in the
// requested timezone; empty timezone means the process local timezone.
func (s *Service) Get(ctx context.Context, date, timezone string) (*Status, error) {
	if s == nil || s.db == nil || s.subscriptionRepo == nil || s.downloadRepo == nil {
		return nil, fmt.Errorf("daily status service is unavailable")
	}

	loc, err := resolveLocation(timezone)
	if err != nil {
		return nil, err
	}
	now := s.now()
	day, err := resolveDate(date, now, loc)
	if err != nil {
		return nil, err
	}
	start := day
	end := start.AddDate(0, 0, 1)

	var subs []model.Subscription
	for offset := 0; ; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, total, err := s.subscriptionRepo.List(offset, repository.MaxPageSize)
		if err != nil {
			return nil, fmt.Errorf("failed to list subscriptions: %w", err)
		}
		subs = append(subs, page...)
		offset += len(page)
		if len(page) == 0 || int64(offset) >= total {
			break
		}
	}
	subByID := make(map[uint]model.Subscription, len(subs))
	for _, sub := range subs {
		subByID[sub.ID] = sub
	}

	out := &Status{
		Date:     start.Format("2006-01-02"),
		Timezone: locationName(loc),
		StartAt:  start,
		EndAt:    end,
		Notes: []string{
			"due_today is a projection of current subscription settings, not confirmed release history; episode is the estimated next episode. Valid airing times are converted to timezone; missing or invalid time/timezone uses the configured weekday without conversion.",
			"checked_today and feed errors use only the last retained check, not a check history. Empty historical results do not prove no check or error occurred. Success and failure counts describe the last check of each feed.",
			"collected_today counts retained download tasks and replacement candidates created that day, including manual collection and baseline candidates; retrying an older task is not a new collection. downloaded_today uses completion time and current completed status. Errors are current errors on those records, not a historical failure timeline.",
		},
		DueToday:        make([]calendar.CalendarItem, 0),
		CheckedToday:    make([]CheckedSubscription, 0),
		CollectedToday:  make([]CollectedSubscription, 0),
		DownloadedToday: make([]DownloadedItem, 0),
		Errors:          make([]DailyError, 0),
	}

	if err := s.buildDueToday(subs, start, out); err != nil {
		return nil, err
	}
	if err := s.buildCheckedToday(ctx, start, end, subByID, out); err != nil {
		return nil, err
	}
	if err := s.buildCollectedToday(ctx, start, end, subByID, out); err != nil {
		return nil, err
	}
	if err := s.buildDownloadedToday(ctx, start, end, subByID, out); err != nil {
		return nil, err
	}

	sort.Slice(out.DueToday, func(i, j int) bool {
		if out.DueToday[i].AirTime == out.DueToday[j].AirTime {
			return out.DueToday[i].SubscriptionID < out.DueToday[j].SubscriptionID
		}
		return out.DueToday[i].AirTime < out.DueToday[j].AirTime
	})
	sort.Slice(out.CheckedToday, func(i, j int) bool {
		if out.CheckedToday[i].Name == out.CheckedToday[j].Name {
			return out.CheckedToday[i].SubscriptionID < out.CheckedToday[j].SubscriptionID
		}
		return out.CheckedToday[i].Name < out.CheckedToday[j].Name
	})
	sort.Slice(out.CollectedToday, func(i, j int) bool {
		if out.CollectedToday[i].Name == out.CollectedToday[j].Name {
			return out.CollectedToday[i].SubscriptionID < out.CollectedToday[j].SubscriptionID
		}
		return out.CollectedToday[i].Name < out.CollectedToday[j].Name
	})
	sort.Slice(out.DownloadedToday, func(i, j int) bool {
		if out.DownloadedToday[i].DownloadedAt == nil {
			return false
		}
		if out.DownloadedToday[j].DownloadedAt == nil {
			return true
		}
		if out.DownloadedToday[i].DownloadedAt.Equal(*out.DownloadedToday[j].DownloadedAt) {
			return out.DownloadedToday[i].ID < out.DownloadedToday[j].ID
		}
		return out.DownloadedToday[i].DownloadedAt.Before(*out.DownloadedToday[j].DownloadedAt)
	})
	return out, nil
}

func (s *Service) buildDueToday(subs []model.Subscription, start time.Time, out *Status) error {
	for _, sub := range subs {
		if sub.Status != "active" || !sub.Enabled || sub.IsCompleted() {
			continue
		}
		airDay := strings.TrimSpace(sub.AirDay)
		if airDay == "" {
			airDay = strings.TrimSpace(sub.UpdateDay)
		}
		airTime, due := scheduleForDay(sub, airDay, start)
		if !due {
			continue
		}

		originalEpisode := max(sub.CurrentEpisode, sub.EpisodeOffset) + 1
		item := calendar.CalendarItem{
			SubscriptionID: sub.ID,
			Name:           sub.Name,
			Episode:        sub.RelativeEpisode(originalEpisode),
			AirTime:        airTime,
			AirDay:         strconv.Itoa(int(start.Weekday())),
			CurrentEpisode: sub.RelativeCurrentEpisode(),
			TotalEpisodes:  sub.TotalEpisodes,
			IsCompleted:    false,
			Cover:          sub.BangumiCoverLocal,
		}
		if download, err := s.downloadRepo.GetBySubscriptionAndEpisode(sub.ID, originalEpisode); err == nil && download != nil {
			item.IsDownloaded = download.Status == model.DownloadStatusCompleted
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to read scheduled download: %w", err)
		}
		out.DueToday = append(out.DueToday, item)
	}
	return nil
}

func (s *Service) buildCheckedToday(ctx context.Context, start, end time.Time, subByID map[uint]model.Subscription, out *Status) error {
	var feeds []model.SubscriptionFeed
	if err := s.db.WithContext(ctx).Where("last_check_time IS NOT NULL").Order("subscription_id ASC, id ASC").Find(&feeds).Error; err != nil {
		return fmt.Errorf("failed to list subscription feed checks: %w", err)
	}
	bySubscription := make(map[uint]*CheckedSubscription)
	for _, feed := range feeds {
		sub, ok := subByID[feed.SubscriptionID]
		if !ok || feed.LastCheckTime == nil || !withinDay(*feed.LastCheckTime, start, end) {
			continue
		}
		item := bySubscription[feed.SubscriptionID]
		if item == nil {
			item = &CheckedSubscription{SubscriptionID: sub.ID, Name: sub.Name, Feeds: make([]FeedCheck, 0)}
			bySubscription[feed.SubscriptionID] = item
		}
		item.FeedsChecked++
		if strings.TrimSpace(feed.LastError) == "" && feed.LastSuccessAt != nil && withinDay(*feed.LastSuccessAt, start, end) {
			item.FeedsSucceeded++
		}
		if strings.TrimSpace(feed.LastError) != "" {
			item.FeedsFailed++
			lastError := strings.TrimSpace(feed.LastError)
			checkedAt := feed.LastCheckTime
			out.Errors = append(out.Errors, DailyError{Kind: "feed_check", SubscriptionID: sub.ID, Name: sub.Name, FeedID: feed.ID, At: checkedAt, Message: lastError})
		}
		item.Feeds = append(item.Feeds, FeedCheck{FeedID: feed.ID, Name: feed.Name, Fansub: feed.Fansub, CheckedAt: feed.LastCheckTime, SuccessAt: feed.LastSuccessAt, LastError: feed.LastError, BaselinePending: feed.BaselinePending})
	}
	for _, item := range bySubscription {
		sort.Slice(item.Feeds, func(i, j int) bool {
			if item.Feeds[i].Name == item.Feeds[j].Name {
				return item.Feeds[i].FeedID < item.Feeds[j].FeedID
			}
			return item.Feeds[i].Name < item.Feeds[j].Name
		})
		out.CheckedToday = append(out.CheckedToday, *item)
	}
	return nil
}

func (s *Service) buildCollectedToday(ctx context.Context, start, end time.Time, subByID map[uint]model.Subscription, out *Status) error {
	var downloads []model.Download
	queryStart, queryEnd := queryWindow(start, end)
	if err := s.db.WithContext(ctx).Where("created_at >= ? AND created_at < ?", queryStart, queryEnd).Order("created_at ASC, id ASC").Find(&downloads).Error; err != nil {
		return fmt.Errorf("failed to list daily collected downloads: %w", err)
	}
	var candidates []candidateRow
	err := s.db.WithContext(ctx).Table("episode_resource_candidates").
		Select("subscription_episodes.subscription_id, subscriptions.name, subscription_episodes.episode, episode_resource_candidates.created_at, episode_resource_candidates.status, episode_resource_candidates.failure_reason").
		Joins("JOIN subscription_episodes ON subscription_episodes.id = episode_resource_candidates.subscription_episode_id").
		Joins("JOIN subscriptions ON subscriptions.id = subscription_episodes.subscription_id").
		Where("episode_resource_candidates.created_at >= ? AND episode_resource_candidates.created_at < ?", queryStart, queryEnd).
		Order("episode_resource_candidates.created_at ASC, episode_resource_candidates.id ASC").
		Scan(&candidates).Error
	if err != nil {
		return fmt.Errorf("failed to list daily collected candidates: %w", err)
	}

	bySubscription := make(map[uint]*CollectedSubscription)
	add := func(id uint, name string, episode int, at time.Time, candidate bool) {
		sub, ok := subByID[id]
		if !ok {
			return
		}
		item := bySubscription[id]
		if item == nil {
			item = &CollectedSubscription{SubscriptionID: id, Name: name, Episodes: make([]int, 0)}
			bySubscription[id] = item
		}
		if candidate {
			item.CandidateCount++
		} else {
			item.DownloadCount++
		}
		if episode > 0 {
			relative := episode // Candidate episodes already use season-relative numbering.
			if !candidate {
				relative = sub.RelativeEpisode(episode)
			}
			if relative > 0 && !containsInt(item.Episodes, relative) {
				item.Episodes = append(item.Episodes, relative)
			}
		}
		if item.LatestCollected == nil || item.LatestCollected.Before(at) {
			value := at
			item.LatestCollected = &value
		}
	}
	for _, download := range downloads {
		if !withinDay(download.CreatedAt, start, end) {
			continue
		}
		if sub, ok := subByID[download.SubscriptionID]; ok {
			add(download.SubscriptionID, sub.Name, download.Episode, download.CreatedAt, false)
			if download.Status == model.DownloadStatusFailed {
				message := strings.TrimSpace(download.LastError)
				if message == "" {
					message = strings.TrimSpace(download.ErrorMessage)
				}
				if message != "" {
					at := download.CreatedAt
					out.Errors = append(out.Errors, DailyError{Kind: "download", SubscriptionID: sub.ID, Name: sub.Name, DownloadID: download.ID, At: &at, Message: message})
				}
			}
		}
	}
	for _, candidate := range candidates {
		if !withinDay(candidate.CreatedAt, start, end) {
			continue
		}
		add(candidate.SubscriptionID, candidate.Name, candidate.Episode, candidate.CreatedAt, true)
		if candidate.Status == model.CandidateStatusFailed || candidate.Status == model.CandidateStatusAcceptedCleanupFailed {
			message := strings.TrimSpace(candidate.FailureReason)
			if message == "" {
				message = candidate.Status
			}
			at := candidate.CreatedAt
			out.Errors = append(out.Errors, DailyError{Kind: "candidate", SubscriptionID: candidate.SubscriptionID, Name: candidate.Name, At: &at, Message: message})
		}
	}
	for _, item := range bySubscription {
		sort.Ints(item.Episodes)
		out.CollectedToday = append(out.CollectedToday, *item)
	}
	return nil
}

func (s *Service) buildDownloadedToday(ctx context.Context, start, end time.Time, subByID map[uint]model.Subscription, out *Status) error {
	var downloads []model.Download
	queryStart, queryEnd := queryWindow(start, end)
	if err := s.db.WithContext(ctx).Where("status = ? AND downloaded_at >= ? AND downloaded_at < ?", model.DownloadStatusCompleted, queryStart, queryEnd).Order("downloaded_at ASC, id ASC").Find(&downloads).Error; err != nil {
		return fmt.Errorf("failed to list daily completed downloads: %w", err)
	}
	for _, download := range downloads {
		sub, ok := subByID[download.SubscriptionID]
		if !ok || download.DownloadedAt == nil || !withinDay(*download.DownloadedAt, start, end) {
			continue
		}
		out.DownloadedToday = append(out.DownloadedToday, DownloadedItem{
			ID: download.ID, SubscriptionID: download.SubscriptionID, Name: sub.Name,
			Episode: sub.RelativeEpisode(download.Episode), OriginalEpisode: download.Episode,
			Title: download.Title, Status: download.Status, RenamedPath: download.RenamedPath,
			DownloadedAt: download.DownloadedAt,
		})
	}
	return nil
}

// SQLite compares timestamp text lexically even when rows use different UTC
// offsets. Widen the SQL range and filter decoded instants with withinDay.
func queryWindow(start, end time.Time) (time.Time, time.Time) {
	return start.Add(-48 * time.Hour), end.Add(48 * time.Hour)
}

func scheduleForDay(sub model.Subscription, airDay string, start time.Time) (string, bool) {
	weekday, err := strconv.Atoi(airDay)
	if err != nil || weekday < 0 || weekday > 6 {
		return "", false
	}
	sourceZone := strings.TrimSpace(sub.AirTimezone)
	switch sourceZone {
	case "", "JST":
		sourceZone = "Asia/Tokyo"
	case "CST":
		sourceZone = "Asia/Shanghai"
	}
	source, zoneErr := time.LoadLocation(sourceZone)
	air, timeErr := time.Parse("15:04", strings.TrimSpace(sub.AirTime))
	if zoneErr != nil || timeErr != nil {
		return "", weekday == int(start.Weekday())
	}
	end := start.AddDate(0, 0, 1)
	localDay := start.In(source)
	for delta := -1; delta <= 2; delta++ {
		candidate := time.Date(localDay.Year(), localDay.Month(), localDay.Day()+delta, air.Hour(), air.Minute(), 0, 0, source)
		if int(candidate.Weekday()) == weekday && withinDay(candidate, start, end) {
			if firstAir, err := time.ParseInLocation("2006-01-02", sub.AirDate, source); err == nil && candidate.Before(firstAir) {
				continue
			}
			return candidate.In(start.Location()).Format("15:04"), true
		}
	}
	return "", false
}

func resolveLocation(raw string) (*time.Location, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(raw)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %v", ErrInvalidTimezone, raw, err)
	}
	return loc, nil
}

func resolveDate(raw string, now time.Time, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		current := now.In(loc)
		return time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, loc), nil
	}
	day, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil || day.Format("2006-01-02") != raw {
		return time.Time{}, fmt.Errorf("%w %q; expected YYYY-MM-DD", ErrInvalidDate, raw)
	}
	return day, nil
}

func locationName(loc *time.Location) string {
	name := loc.String()
	if name == "Local" {
		return time.Now().Location().String()
	}
	return name
}

func withinDay(value, start, end time.Time) bool {
	value = value.In(start.Location())
	return !value.Before(start) && value.Before(end)
}

func containsInt(values []int, value int) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
