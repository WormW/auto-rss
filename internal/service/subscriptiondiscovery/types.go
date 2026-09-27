package subscriptiondiscovery

import (
	"context"
	"time"

	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
)

type PrepareInput struct {
	Query            string                           `json:"query" jsonschema:"Anime title from the user's request. Required."`
	Season           int                              `json:"season,omitempty" jsonschema:"Requested library season number. Defaults to 1; never infer it from an airing quarter."`
	BangumiID        int                              `json:"bangumi_id,omitempty" jsonschema:"Select an anime ID returned by a previous prepare call. Leave empty to search candidates first."`
	Year             int                              `json:"year,omitempty" jsonschema:"Optional airing year to narrow metadata candidates."`
	Language         string                           `json:"language,omitempty" jsonschema:"Required subtitle language: any, chs, cht, en. Defaults to any; do not invent a language requirement."`
	Resolution       int                              `json:"resolution,omitempty" jsonschema:"Hard required resolution: 480, 720, 1080, 2160. Nonzero overrides the default quality policy; never use this field for a soft preference."`
	QualityPolicy    *subscriptionmatch.QualityPolicy `json:"quality_policy,omitempty" jsonschema:"When resolution and this object are omitted: exclude 720, prefer 1080, wait for preferred. An explicit object replaces those defaults; include all three fields. Leave subtitle language and fansub unrestricted unless requested."`
	Fansub           string                           `json:"fansub,omitempty" jsonschema:"Optional exact required fansub. Do not use this for a soft preference."`
	Sources          []string                         `json:"sources,omitempty" jsonschema:"mikan, nyaa and/or dmhy. Defaults to all three."`
	NyaaQuery        string                           `json:"nyaa_query,omitempty" jsonschema:"Optional user-adjusted Nyaa search expression. Leave empty to derive searches from verified aliases."`
	NyaaUploader     string                           `json:"nyaa_uploader,omitempty" jsonschema:"Optional exact Nyaa upload account, separate from fansub."`
	DmhyQuery        string                           `json:"dmhy_query,omitempty" jsonschema:"Optional user-adjusted DMHY keyword expression. Leave empty to derive Chinese and native title searches from verified metadata. No Nyaa-specific filters are applied."`
	DmhyTeamID       int                              `json:"dmhy_team_id,omitempty" jsonschema:"Optional DMHY subtitle-group team_id selected from a previous preview. Leave empty to learn groups from a broad search first."`
	TrustedOnly      bool                             `json:"trusted_only,omitempty" jsonschema:"Require Nyaa's trusted uploader mark; this does not establish title identity."`
	EpisodeOffset    int                              `json:"episode_offset,omitempty" jsonschema:"Subtract this from source episode numbers. Only set when the mapping is known."`
	RequiredKeywords []string                         `json:"required_keywords,omitempty" jsonschema:"All of these title fragments must match (AND)."`
	ExcludeKeywords  []string                         `json:"exclude_keywords,omitempty"`
	DraftID          string                           `json:"draft_id,omitempty" jsonschema:"Revise an existing draft using its current revision. Supply the full desired input."`
	Revision         int                              `json:"revision,omitempty"`
}

type Subject struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	NameCN     string     `json:"name_cn"`
	Aliases    []string   `json:"aliases"`
	Date       string     `json:"date"`
	Platform   string     `json:"platform"`
	Episodes   int        `json:"episodes"`
	URL        string     `json:"url"`
	Cover      string     `json:"cover,omitempty"`
	SeasonHint int        `json:"season_hint,omitempty"`
	Relations  []Relation `json:"relations,omitempty"`
}
type Relation struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Relation string `json:"relation"`
}
type Feed struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	URL             string `json:"url"`
	PageURL         string `json:"page_url"`
	Fansub          string `json:"fansub,omitempty"`
	BangumiID       int    `json:"bangumi_id,omitempty"`
	Query           string `json:"query,omitempty"`
	MappingEvidence string `json:"mapping_evidence,omitempty"`
	TeamID          int    `json:"team_id,omitempty" jsonschema:"DMHY team_id when this feed was narrowed to a selected group."`
	TeamName        string `json:"team_name,omitempty"`
}
type GroupCandidate struct {
	ID              int      `json:"id,omitempty"`
	Name            string   `json:"name"`
	ItemCount       int      `json:"item_count"`
	MatchedItems    int      `json:"matched_items"`
	Episodes        []int    `json:"episodes"`
	MatchedEpisodes []int    `json:"matched_episodes"`
	Samples         []string `json:"samples,omitempty"`
	Evidence        string   `json:"evidence,omitempty"`
}
type Sample struct {
	Title           string                     `json:"title"`
	Episode         int                        `json:"episode"`
	RelativeEpisode int                        `json:"relative_episode"`
	Decision        subscriptionmatch.Decision `json:"decision"`
	CategoryID      string                     `json:"category_id,omitempty"`
	Trusted         bool                       `json:"trusted"`
	RenamePreview   string                     `json:"rename_preview,omitempty"`
}
type FeedPreview struct {
	Feed                  Feed                    `json:"feed"`
	Rules                 subscriptionmatch.Rules `json:"rules"`
	Samples               []Sample                `json:"samples"`
	Fetched               int                     `json:"fetched"`
	Matched               int                     `json:"matched"`
	MatchedEpisodes       []int                   `json:"matched_episodes"`
	DuplicateEpisodeItems int                     `json:"duplicate_episode_items"`
	Rejected              int                     `json:"rejected"`
	NeedsReview           int                     `json:"needs_review"`
	Confirmable           bool                    `json:"confirmable"`
	Status                string                  `json:"status"`
	CheckedAt             time.Time               `json:"checked_at"`
	Warnings              []string                `json:"warnings,omitempty"`
	GroupCandidates       []GroupCandidate        `json:"group_candidates,omitempty"`
}
type Draft struct {
	ID          string        `json:"draft_id"`
	Revision    int           `json:"revision"`
	ExpiresAt   time.Time     `json:"expires_at"`
	Status      string        `json:"status"`
	Intent      PrepareInput  `json:"intent"`
	Candidates  []Subject     `json:"candidates"`
	Subject     *Subject      `json:"subject,omitempty"`
	Feeds       []FeedPreview `json:"feeds"`
	Warnings    []string      `json:"warnings"`
	StartPolicy string        `json:"start_policy"`
}
type ConfirmInput struct {
	DraftID  string `json:"draft_id"`
	Revision int    `json:"revision" jsonschema:"Exact revision reviewed by the user."`
	FeedID   string `json:"feed_id" jsonschema:"Exact feed choice reviewed by the user."`
}

// Provider isolates external lookups from draft persistence and confirmation.
type Provider interface {
	Search(context.Context, PrepareInput) ([]Subject, error)
	Subject(context.Context, int) (Subject, error)
	Feeds(context.Context, Subject, PrepareInput) ([]Feed, []string, error)
	Fetch(context.Context, Feed) ([]rss.RSSItem, error)
}
