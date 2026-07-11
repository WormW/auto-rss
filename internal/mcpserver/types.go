package mcpserver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type PageInfo struct {
	Total      int64  `json:"total" jsonschema:"Total number of matching records."`
	Limit      int    `json:"limit" jsonschema:"Number of records returned in this response."`
	NextCursor string `json:"next_cursor,omitempty" jsonschema:"Opaque cursor to pass to the next list call. Empty means there are no more results."`
}

type SubscriptionSummary struct {
	ID                   uint       `json:"id"`
	Name                 string     `json:"name"`
	Season               int        `json:"season"`
	Status               string     `json:"status"`
	Enabled              bool       `json:"enabled"`
	RSSURL               string     `json:"rss_url,omitempty"`
	Fansub               string     `json:"fansub,omitempty"`
	Language             string     `json:"language,omitempty"`
	LanguagePreference   string     `json:"language_preference,omitempty"`
	RenameEnabled        bool       `json:"rename_enabled"`
	CurrentEpisode       int        `json:"current_episode"`
	LatestEpisode        int        `json:"latest_episode"`
	TotalEpisodes        int        `json:"total_episodes"`
	BangumiID            int        `json:"bangumi_id,omitempty"`
	BangumiScore         float64    `json:"bangumi_score,omitempty"`
	AirDate              string     `json:"air_date,omitempty"`
	AirDay               string     `json:"air_day,omitempty"`
	AirTime              string     `json:"air_time,omitempty"`
	LastCheckTime        *time.Time `json:"last_check_time,omitempty"`
	LastDownloadAt       *time.Time `json:"last_download_at,omitempty"`
	LastRSSPubTime       *time.Time `json:"last_rss_pub_time,omitempty"`
	CollectionConfigured bool       `json:"collection_configured"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type DownloadSummary struct {
	ID               uint       `json:"id"`
	SubscriptionID   uint       `json:"subscription_id"`
	SubscriptionName string     `json:"subscription_name,omitempty"`
	Title            string     `json:"title"`
	Episode          int        `json:"episode"`
	Fansub           string     `json:"fansub,omitempty"`
	Language         string     `json:"language,omitempty"`
	Status           string     `json:"status"`
	FilePath         string     `json:"file_path,omitempty"`
	RenamedPath      string     `json:"renamed_path,omitempty"`
	TorrentHash      string     `json:"torrent_hash,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	RetryCount       int        `json:"retry_count"`
	MaxRetries       int        `json:"max_retries"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
	DownloadedAt     *time.Time `json:"downloaded_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type RSSSourceSummary struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	BaseURL     string    `json:"base_url"`
	Enabled     bool      `json:"enabled"`
	Description string    `json:"description,omitempty"`
	TimeoutMS   int64     `json:"timeout_ms"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type LogSummary struct {
	ID        uint      `json:"id"`
	Level     string    `json:"level"`
	Module    string    `json:"module,omitempty"`
	Message   string    `json:"message"`
	Context   string    `json:"context,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type SystemOverview struct {
	Subscriptions struct {
		Total    int64 `json:"total"`
		Active   int   `json:"active"`
		Paused   int   `json:"paused"`
		Disabled int   `json:"disabled"`
	} `json:"subscriptions"`
	Downloads struct {
		Total       int64          `json:"total"`
		ByStatus    map[string]int `json:"by_status"`
		FailedReady int            `json:"failed_ready"`
	} `json:"downloads"`
	RSSSources struct {
		Total   int64 `json:"total"`
		Enabled int   `json:"enabled"`
	} `json:"rss_sources"`
	Disk any `json:"disk,omitempty"`
}

type ListSubscriptionsInput struct {
	Cursor  string `json:"cursor,omitempty" jsonschema:"Opaque cursor returned by a previous list_subscriptions call. Leave empty for the first page."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum records to return. Defaults to 20 and is capped at 100."`
	Status  string `json:"status,omitempty" jsonschema:"Optional status filter. Use active or paused when you only want subscriptions in that state."`
	Enabled *bool  `json:"enabled,omitempty" jsonschema:"Optional enabled filter. true returns enabled subscriptions, false returns disabled subscriptions."`
	Query   string `json:"query,omitempty" jsonschema:"Optional case-insensitive substring filter matched against subscription name, fansub, and RSS URL."`
}

type ListSubscriptionsOutput struct {
	Items    []SubscriptionSummary `json:"items"`
	PageInfo PageInfo              `json:"page_info"`
}

type GetSubscriptionInput struct {
	ID uint `json:"id" jsonschema:"Subscription ID."`
}

type GetSubscriptionOutput struct {
	Subscription    SubscriptionSummary `json:"subscription"`
	RecentDownloads []DownloadSummary   `json:"recent_downloads"`
}

type CreateSubscriptionInput struct {
	Name               string `json:"name" jsonschema:"Anime title to subscribe to. Include season in season field when possible."`
	RSSURL             string `json:"rss_url" jsonschema:"RSS feed URL. Usually a Mikan fansub RSS URL or another anime RSS feed."`
	Season             int    `json:"season,omitempty" jsonschema:"Season number. Defaults to 1."`
	Fansub             string `json:"fansub,omitempty" jsonschema:"Preferred fansub group name."`
	LanguagePreference string `json:"language_preference,omitempty" jsonschema:"Language preference: auto, chs, cht, or both."`
	TotalEpisodes      int    `json:"total_episodes,omitempty" jsonschema:"Known total episode count. Leave 0 if unknown."`
	BangumiID          int    `json:"bangumi_id,omitempty" jsonschema:"Bangumi subject ID, if already known."`
	RenameEnabled      *bool  `json:"rename_enabled,omitempty" jsonschema:"Whether Auto-RSS should rename completed files. Defaults to true."`
}

type CreateSubscriptionOutput struct {
	Subscription SubscriptionSummary `json:"subscription"`
}

type ToggleSubscriptionInput struct {
	ID      uint  `json:"id" jsonschema:"Subscription ID."`
	Enabled *bool `json:"enabled,omitempty" jsonschema:"Desired enabled state. Leave empty to toggle the current state."`
}

type ToggleSubscriptionOutput struct {
	Subscription SubscriptionSummary `json:"subscription"`
}

type ListDownloadsInput struct {
	Cursor         string `json:"cursor,omitempty" jsonschema:"Opaque cursor returned by a previous list_downloads call. Leave empty for the first page."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum records to return. Defaults to 20 and is capped at 100."`
	Status         string `json:"status,omitempty" jsonschema:"Optional status filter: pending, downloading, stalled, completed, failed, or organizing."`
	SubscriptionID uint   `json:"subscription_id,omitempty" jsonschema:"Optional subscription ID filter."`
}

type ListDownloadsOutput struct {
	Items    []DownloadSummary `json:"items"`
	PageInfo PageInfo          `json:"page_info"`
}

type GetDownloadInput struct {
	ID uint `json:"id" jsonschema:"Download ID."`
}

type GetDownloadOutput struct {
	Download DownloadSummary `json:"download"`
}

type RetryDownloadInput struct {
	ID uint `json:"id" jsonschema:"Failed or stalled download ID to reset for retry."`
}

type RetryDownloadOutput struct {
	Download DownloadSummary `json:"download"`
	Message  string          `json:"message"`
}

type RefreshRSSInput struct{}

type RefreshRSSOutput struct {
	Message string `json:"message"`
}

type PreviewRecoveryInput struct {
	SubscriptionID uint `json:"subscription_id,omitempty" jsonschema:"Optional subscription ID filter. Leave empty to preview recovery candidates for all subscriptions."`
}

type RecoveryPreviewOutput struct {
	DryRun                 bool                          `json:"dry_run"`
	PreviewOnly            bool                          `json:"preview_only"`
	Applied                bool                          `json:"applied"`
	ScannedFiles           int                           `json:"scanned_files"`
	MatchedFiles           int                           `json:"matched_files"`
	OrphanFileCount        int                           `json:"orphan_file_count"`
	OrphanFileSamples      []string                      `json:"orphan_file_samples,omitempty"`
	OrphanFileOmittedCount int                           `json:"orphan_file_omitted_count,omitempty"`
	SubscriptionCount      int                           `json:"subscription_count"`
	DownloadsToUpdateCount int                           `json:"downloads_to_update_count"`
	DownloadsToCreateCount int                           `json:"downloads_to_create_count"`
	DownloadsMissingCount  int                           `json:"downloads_missing_count"`
	Subscriptions          []RecoverySubscriptionPreview `json:"subscriptions"`
}

type RecoverySubscriptionPreview struct {
	SubscriptionID         uint   `json:"subscription_id"`
	Name                   string `json:"name"`
	CurrentEpisodeOld      int    `json:"current_episode_old"`
	CurrentEpisodeNew      int    `json:"current_episode_new"`
	LatestEpisodeOld       int    `json:"latest_episode_old"`
	LatestEpisodeNew       int    `json:"latest_episode_new"`
	EpisodesOnDiskCount    int    `json:"episodes_on_disk_count"`
	EpisodeSamples         []int  `json:"episode_samples,omitempty"`
	EpisodeOmittedCount    int    `json:"episode_omitted_count,omitempty"`
	MatchedFileCount       int    `json:"matched_file_count"`
	DownloadsToUpdateCount int    `json:"downloads_to_update_count"`
	DownloadsToUpdateIDs   []uint `json:"downloads_to_update_ids,omitempty"`
	DownloadsToCreateCount int    `json:"downloads_to_create_count"`
	DownloadsToCreate      []int  `json:"downloads_to_create,omitempty"`
	DownloadsMissingCount  int    `json:"downloads_missing_count"`
	DownloadsMissingIDs    []uint `json:"downloads_missing_ids,omitempty"`
}

type SearchMikanInput struct {
	Query string `json:"query" jsonschema:"Anime title keyword. Must be at least 2 characters."`
}

type GetMikanSeasonInput struct {
	Year   int    `json:"year" jsonschema:"Calendar year, for example 2026."`
	Season string `json:"season" jsonschema:"Mikan season key, for example spring, summer, autumn, or winter."`
}

type GetMikanFansubsInput struct {
	URL string `json:"url" jsonschema:"Mikan anime detail page URL returned by search_mikan or get_mikan_season."`
}

type SearchBangumiInput struct {
	Query    string `json:"query" jsonschema:"Anime title keyword."`
	BestOnly bool   `json:"best_only,omitempty" jsonschema:"When true, return only the best matching subject."`
}

type GetBangumiSubjectInput struct {
	ID int `json:"id" jsonschema:"Bangumi subject ID."`
}

type GetCalendarInput struct {
	TodayOnly  bool `json:"today_only,omitempty" jsonschema:"When true, return only today's airing schedule."`
	WeekOffset int  `json:"week_offset,omitempty" jsonschema:"Week offset for the full week view. 0 is the current week, 1 is next week."`
}

type GetDiskStatusInput struct{}

type ListLogsInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque cursor returned by a previous list_logs call. Leave empty for the first page."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum records to return. Defaults to 20 and is capped at 100."`
	Level  string `json:"level,omitempty" jsonschema:"Optional log level filter such as DEBUG, INFO, WARN, or ERROR."`
	Module string `json:"module,omitempty" jsonschema:"Optional module filter such as rss, download, organizer, or system."`
}

type ListLogsOutput struct {
	Items    []LogSummary `json:"items"`
	PageInfo PageInfo     `json:"page_info"`
}

func summarizeSubscription(sub model.Subscription) SubscriptionSummary {
	return SubscriptionSummary{
		ID:                   sub.ID,
		Name:                 sub.Name,
		Season:               sub.Season,
		Status:               sub.Status,
		Enabled:              sub.Enabled,
		RSSURL:               sub.RssURL,
		Fansub:               sub.Fansub,
		Language:             sub.Language,
		LanguagePreference:   sub.LanguagePreference,
		RenameEnabled:        sub.RenameEnabled,
		CurrentEpisode:       sub.CurrentEpisode,
		LatestEpisode:        sub.LatestEpisode,
		TotalEpisodes:        sub.TotalEpisodes,
		BangumiID:            sub.BangumiID,
		BangumiScore:         sub.BangumiScore,
		AirDate:              sub.AirDate,
		AirDay:               sub.AirDay,
		AirTime:              sub.AirTime,
		LastCheckTime:        sub.LastCheckTime,
		LastDownloadAt:       sub.LastDownloadAt,
		LastRSSPubTime:       sub.LastRSSPubTime,
		CollectionConfigured: sub.CollectionTorrent != "",
		CreatedAt:            sub.CreatedAt,
		UpdatedAt:            sub.UpdatedAt,
	}
}

func summarizeDownload(download model.Download) DownloadSummary {
	return DownloadSummary{
		ID:               download.ID,
		SubscriptionID:   download.SubscriptionID,
		SubscriptionName: download.Subscription.Name,
		Title:            download.Title,
		Episode:          download.Episode,
		Fansub:           download.Fansub,
		Language:         download.Language,
		Status:           download.Status,
		FilePath:         download.FilePath,
		RenamedPath:      download.RenamedPath,
		TorrentHash:      download.TorrentHash,
		ErrorMessage:     download.ErrorMessage,
		LastError:        download.LastError,
		RetryCount:       download.RetryCount,
		MaxRetries:       download.MaxRetries,
		NextRetryAt:      download.NextRetryAt,
		DownloadedAt:     download.DownloadedAt,
		CreatedAt:        download.CreatedAt,
		UpdatedAt:        download.UpdatedAt,
	}
}

func summarizeRSSSource(source model.RSSSource) RSSSourceSummary {
	return RSSSourceSummary{
		ID:          source.ID,
		Name:        source.Name,
		BaseURL:     source.BaseURL,
		Enabled:     source.Enabled,
		Description: source.Description,
		TimeoutMS:   source.Timeout.Milliseconds(),
		CreatedAt:   source.CreatedAt,
		UpdatedAt:   source.UpdatedAt,
	}
}

func summarizeLog(log *model.Log) LogSummary {
	return LogSummary{
		ID:        log.ID,
		Level:     log.Level,
		Module:    log.Module,
		Message:   log.Message,
		Context:   log.Context,
		CreatedAt: log.CreatedAt,
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func encodeCursor(offset int) string {
	if offset <= 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("offset:%d", offset)))
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor: pass the next_cursor returned by the previous call")
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || parts[0] != "offset" {
		return 0, fmt.Errorf("invalid cursor: pass the next_cursor returned by the previous call")
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor: pass the next_cursor returned by the previous call")
	}
	return offset, nil
}

func nextCursor(offset, count int, total int64) string {
	next := offset + count
	if int64(next) >= total || count == 0 {
		return ""
	}
	return encodeCursor(next)
}

func toJSONText(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", value)
	}
	return string(data)
}
