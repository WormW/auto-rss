package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/episode"
	"gorm.io/gorm"
)

const (
	SchemaVersion       = "1.2"
	legacySchemaVersion = "1.0"
	ledgerSchemaVersion = "1.1"
	RedactedValue       = "__AUTO_RSS_REDACTED__"
	MaxPackageBytes     = 8 << 20

	MaxSubscriptionsPerPackage = 10_000
	MaxEpisodesPerPackage      = 100_000
	MaxCandidatesPerPackage    = 200_000
	MaxPackageItems            = 300_000

	MaxSubscriptionKeyBytes = 4 << 10
	MaxTorrentHashBytes     = 256
	MaxURLBytes             = 16 << 10
	MaxTitleBytes           = 4 << 10
	MaxFansubBytes          = 1 << 10
	MaxLanguageBytes        = 256
	MaxStatusBytes          = 256
	MaxFailureReasonBytes   = 16 << 10

	StrategySkip      = "skip"
	StrategyOverwrite = "overwrite"
	StrategyMerge     = "merge"

	SourceAuto        = "auto"
	SourceAutoRSS     = "auto-rss"
	SourceAutoBangumi = "auto-bangumi"
)

var ErrPackageTooLarge = errors.New("backup package too large")

type PackageLimitError struct {
	Field  string
	Actual int
	Limit  int
}

func (e *PackageLimitError) Error() string {
	return fmt.Sprintf("%s: %s is %d, limit is %d", ErrPackageTooLarge, e.Field, e.Actual, e.Limit)
}

func (e *PackageLimitError) Unwrap() error {
	return ErrPackageTooLarge
}

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

type Package struct {
	SchemaVersion        string                      `json:"schema_version"`
	App                  string                      `json:"app"`
	ExportedAt           time.Time                   `json:"exported_at"`
	IncludesSensitive    bool                        `json:"includes_sensitive"`
	SensitivePlaceholder string                      `json:"sensitive_placeholder"`
	Summary              PackageSummary              `json:"summary"`
	Configs              []ConfigRecord              `json:"configs"`
	RSSSources           []model.RSSSource           `json:"rss_sources"`
	Groups               []model.SubscriptionGroup   `json:"groups"`
	Tags                 []model.SubscriptionTag     `json:"tags"`
	Subscriptions        []SubscriptionRecord        `json:"subscriptions"`
	Episodes             []EpisodeRecord             `json:"episodes,omitempty"`
	EpisodeCandidates    []CandidateRecord           `json:"episode_candidates,omitempty"`
	SubscriptionTags     []SubscriptionTagRecord     `json:"subscription_tags"`
	NotificationSettings []NotificationSettingRecord `json:"notification_settings"`
}

type PackageSummary struct {
	Configs              int `json:"configs"`
	RSSSources           int `json:"rss_sources"`
	Groups               int `json:"groups"`
	Tags                 int `json:"tags"`
	Subscriptions        int `json:"subscriptions"`
	Episodes             int `json:"episodes"`
	EpisodeCandidates    int `json:"episode_candidates"`
	SubscriptionTags     int `json:"subscription_tags"`
	NotificationSettings int `json:"notification_settings"`
}

type ConfigRecord struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description,omitempty"`
	Sensitive   bool      `json:"sensitive"`
	Redacted    bool      `json:"redacted"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type SubscriptionRecord struct {
	model.Subscription
	GroupName     string                   `json:"group_name,omitempty"`
	RSSSourceName string                   `json:"rss_source_name,omitempty"`
	Feeds         []model.SubscriptionFeed `json:"feeds,omitempty"`
}

type EpisodeRecord struct {
	SubscriptionKey   string     `json:"subscription_key"`
	Episode           int        `json:"episode"`
	Status            string     `json:"status"`
	ActiveTorrentHash string     `json:"active_torrent_hash,omitempty"`
	ActiveTorrentURL  string     `json:"active_torrent_url,omitempty"`
	ActiveTitle       string     `json:"active_title,omitempty"`
	StatusSource      string     `json:"status_source"`
	DownloadedAt      *time.Time `json:"downloaded_at,omitempty"`
}

type CandidateRecord struct {
	SubscriptionKey string     `json:"subscription_key"`
	Episode         int        `json:"episode"`
	TorrentHash     string     `json:"torrent_hash,omitempty"`
	TorrentURL      string     `json:"torrent_url"`
	Title           string     `json:"title"`
	Fansub          string     `json:"fansub,omitempty"`
	Language        string     `json:"language,omitempty"`
	PubTime         *time.Time `json:"pub_time,omitempty"`
	SourceRSSURL    string     `json:"source_rss_url,omitempty"`
	Status          string     `json:"status"`
	FailureReason   string     `json:"failure_reason,omitempty"`

	runtimeStage string
}

func (record *CandidateRecord) UnmarshalJSON(data []byte) error {
	type stableCandidateRecord CandidateRecord
	var wire struct {
		stableCandidateRecord
		ReplacementStage string `json:"replacement_stage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*record = CandidateRecord(wire.stableCandidateRecord)
	record.runtimeStage = strings.TrimSpace(wire.ReplacementStage)
	return nil
}

type SubscriptionTagRecord struct {
	SubscriptionKey string `json:"subscription_key"`
	Subscription    string `json:"subscription"`
	TagName         string `json:"tag_name"`
}

type NotificationSettingRecord struct {
	Channel   string    `json:"channel"`
	Enabled   bool      `json:"enabled"`
	Config    string    `json:"config"`
	Sensitive bool      `json:"sensitive"`
	Redacted  bool      `json:"redacted"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type ImportPlan struct {
	SourceFormat string        `json:"source_format"`
	Strategy     string        `json:"strategy"`
	Summary      ImportSummary `json:"summary"`
	Items        []ImportItem  `json:"items"`
}

type ImportSummary struct {
	Total            int `json:"total"`
	Create           int `json:"create"`
	Overwrite        int `json:"overwrite"`
	Merge            int `json:"merge"`
	Skip             int `json:"skip"`
	SensitiveSkipped int `json:"sensitive_skipped"`
}

type ImportItem struct {
	Resource  string `json:"resource"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
	Conflict  bool   `json:"conflict"`
	Sensitive bool   `json:"sensitive"`
}

type currentState struct {
	configs       map[string]model.Config
	rssSources    map[string]model.RSSSource
	groups        map[string]model.SubscriptionGroup
	tags          map[string]model.SubscriptionTag
	subscriptions map[string]model.Subscription
	episodes      map[string]model.SubscriptionEpisode
	candidates    map[string]model.EpisodeResourceCandidate
	settings      map[string]model.NotificationSetting
}

func (s *Service) Export(includeSensitive bool) (*Package, error) {
	var configs []model.Config
	if err := s.db.Order("key ASC").Find(&configs).Error; err != nil {
		return nil, err
	}

	var rssSources []model.RSSSource
	if err := s.db.Order("name ASC, id ASC").Find(&rssSources).Error; err != nil {
		return nil, err
	}

	var groups []model.SubscriptionGroup
	if err := s.db.Order("sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}

	var tags []model.SubscriptionTag
	if err := s.db.Order("sort_order ASC, id ASC").Find(&tags).Error; err != nil {
		return nil, err
	}

	var subscriptions []model.Subscription
	if err := s.db.Order("name ASC, season ASC, id ASC").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	var feeds []model.SubscriptionFeed
	if err := s.db.Order("subscription_id ASC, id ASC").Find(&feeds).Error; err != nil {
		return nil, err
	}

	var episodes []model.SubscriptionEpisode
	if err := s.db.Order("subscription_id ASC, episode ASC").Find(&episodes).Error; err != nil {
		return nil, err
	}

	var candidates []model.EpisodeResourceCandidate
	if err := s.db.Order("subscription_episode_id ASC, id ASC").Find(&candidates).Error; err != nil {
		return nil, err
	}
	hasOwnership := len(episodes) > 0 || len(candidates) > 0

	var relations []model.SubscriptionTagRelation
	if err := s.db.Find(&relations).Error; err != nil {
		return nil, err
	}

	var settings []model.NotificationSetting
	if err := s.db.Order("channel ASC").Find(&settings).Error; err != nil {
		return nil, err
	}

	groupNames := make(map[uint]string, len(groups))
	for _, group := range groups {
		groupNames[group.ID] = group.Name
	}
	sourceNames := make(map[uint]string, len(rssSources))
	for _, source := range rssSources {
		sourceNames[source.ID] = source.Name
	}
	tagNames := make(map[uint]string, len(tags))
	for _, tag := range tags {
		tagNames[tag.ID] = tag.Name
	}
	feedsBySubscription := make(map[uint][]model.SubscriptionFeed)
	for _, feed := range feeds {
		feedsBySubscription[feed.SubscriptionID] = append(feedsBySubscription[feed.SubscriptionID], sanitizeFeedForBackup(feed))
	}
	subscriptionByID := make(map[uint]model.Subscription, len(subscriptions))
	subscriptionKeyByID := make(map[uint]string, len(subscriptions))
	seenSubscriptionKeys := make(map[string]uint, len(subscriptions))
	for _, sub := range subscriptions {
		key := subscriptionKeyFromModel(sub)
		if hasOwnership && key == "" {
			return nil, fmt.Errorf("subscription %d has no stable backup key", sub.ID)
		}
		if existingID, exists := seenSubscriptionKeys[key]; hasOwnership && exists && existingID != sub.ID {
			return nil, fmt.Errorf("subscription backup key collision %q for IDs %d and %d", key, existingID, sub.ID)
		}
		seenSubscriptionKeys[key] = sub.ID
		subscriptionByID[sub.ID] = sub
		subscriptionKeyByID[sub.ID] = key
	}

	pkg := &Package{
		SchemaVersion:        SchemaVersion,
		App:                  "auto-rss",
		ExportedAt:           time.Now().UTC(),
		IncludesSensitive:    includeSensitive,
		SensitivePlaceholder: RedactedValue,
	}

	for _, cfg := range configs {
		sensitive := isSensitiveConfigKey(cfg.Key)
		value := cfg.Value
		redacted := sensitive && !includeSensitive
		if redacted {
			value = RedactedValue
		}
		pkg.Configs = append(pkg.Configs, ConfigRecord{
			Key:         cfg.Key,
			Value:       value,
			Description: cfg.Description,
			Sensitive:   sensitive,
			Redacted:    redacted,
			UpdatedAt:   cfg.UpdatedAt,
		})
	}

	pkg.RSSSources = rssSources
	pkg.Groups = groups
	pkg.Tags = tags

	for _, sub := range subscriptions {
		record := SubscriptionRecord{Subscription: sub, Feeds: feedsBySubscription[sub.ID]}
		if sub.GroupID != nil {
			record.GroupName = groupNames[*sub.GroupID]
		}
		if sub.RSSSourceID != nil {
			record.RSSSourceName = sourceNames[*sub.RSSSourceID]
		}
		record.Group = nil
		record.RSSSource = nil
		record.Downloads = nil
		record.Subscription.Feeds = nil
		pkg.Subscriptions = append(pkg.Subscriptions, record)
	}

	episodeRecordByID := make(map[uint]EpisodeRecord, len(episodes))
	for _, ledger := range episodes {
		subscriptionKey, ok := subscriptionKeyByID[ledger.SubscriptionID]
		if !ok {
			return nil, fmt.Errorf("episode %d references missing subscription %d", ledger.ID, ledger.SubscriptionID)
		}
		record := EpisodeRecord{
			SubscriptionKey:   subscriptionKey,
			Episode:           ledger.Episode,
			Status:            ledger.Status,
			ActiveTorrentHash: ledger.ActiveTorrentHash,
			ActiveTorrentURL:  ledger.ActiveTorrentURL,
			ActiveTitle:       ledger.ActiveTitle,
			StatusSource:      ledger.StatusSource,
			DownloadedAt:      ledger.DownloadedAt,
		}
		normalizeEpisodeRecord(&record)
		pkg.Episodes = append(pkg.Episodes, record)
		episodeRecordByID[ledger.ID] = record
	}

	for _, candidate := range candidates {
		ledgerRecord, ok := episodeRecordByID[candidate.SubscriptionEpisodeID]
		if !ok {
			return nil, fmt.Errorf("candidate %d references missing episode %d", candidate.ID, candidate.SubscriptionEpisodeID)
		}
		record := CandidateRecord{
			SubscriptionKey: ledgerRecord.SubscriptionKey,
			Episode:         ledgerRecord.Episode,
			TorrentHash:     candidate.TorrentHash,
			TorrentURL:      candidate.TorrentURL,
			Title:           candidate.Title,
			Fansub:          candidate.Fansub,
			Language:        candidate.Language,
			PubTime:         candidate.PubTime,
			SourceRSSURL:    candidate.SourceRSSURL,
			Status:          candidate.Status,
			FailureReason:   candidate.FailureReason,
			runtimeStage:    candidate.ReplacementStage,
		}
		normalizeCandidateRecord(&record)
		pkg.EpisodeCandidates = append(pkg.EpisodeCandidates, record)
	}
	sort.Slice(pkg.Episodes, func(i, j int) bool {
		if pkg.Episodes[i].SubscriptionKey == pkg.Episodes[j].SubscriptionKey {
			return pkg.Episodes[i].Episode < pkg.Episodes[j].Episode
		}
		return pkg.Episodes[i].SubscriptionKey < pkg.Episodes[j].SubscriptionKey
	})
	sort.Slice(pkg.EpisodeCandidates, func(i, j int) bool {
		left, right := pkg.EpisodeCandidates[i], pkg.EpisodeCandidates[j]
		if left.SubscriptionKey != right.SubscriptionKey {
			return left.SubscriptionKey < right.SubscriptionKey
		}
		if left.Episode != right.Episode {
			return left.Episode < right.Episode
		}
		return candidateResourceKey(left) < candidateResourceKey(right)
	})

	for _, relation := range relations {
		sub, ok := subscriptionByID[relation.SubscriptionID]
		if !ok {
			continue
		}
		tagName := tagNames[relation.TagID]
		if tagName == "" {
			continue
		}
		pkg.SubscriptionTags = append(pkg.SubscriptionTags, SubscriptionTagRecord{
			SubscriptionKey: subscriptionKeyFromModel(sub),
			Subscription:    sub.Name,
			TagName:         tagName,
		})
	}

	for _, setting := range settings {
		config := setting.Config
		redacted := !includeSensitive && setting.Config != ""
		if redacted {
			config = RedactedValue
		}
		pkg.NotificationSettings = append(pkg.NotificationSettings, NotificationSettingRecord{
			Channel:   setting.Channel,
			Enabled:   setting.Enabled,
			Config:    config,
			Sensitive: true,
			Redacted:  redacted,
			UpdatedAt: setting.UpdatedAt,
		})
	}

	pkg.Summary = PackageSummary{
		Configs:              len(pkg.Configs),
		RSSSources:           len(pkg.RSSSources),
		Groups:               len(pkg.Groups),
		Tags:                 len(pkg.Tags),
		Subscriptions:        len(pkg.Subscriptions),
		Episodes:             len(pkg.Episodes),
		EpisodeCandidates:    len(pkg.EpisodeCandidates),
		SubscriptionTags:     len(pkg.SubscriptionTags),
		NotificationSettings: len(pkg.NotificationSettings),
	}
	if err := validatePackage(pkg); err != nil {
		return nil, fmt.Errorf("cannot export invalid episode ownership state: %w", err)
	}

	return pkg, nil
}

func (s *Service) Preview(data json.RawMessage, sourceFormat, strategy string) (*ImportPlan, error) {
	pkg, detectedFormat, err := ParsePackage(data, sourceFormat)
	if err != nil {
		return nil, err
	}
	hasOwnership := packageHasOwnership(pkg)
	state, err := s.loadCurrentState(s.db, hasOwnership, hasOwnership)
	if err != nil {
		return nil, err
	}
	return buildPlan(pkg, state, detectedFormat, normalizeStrategy(strategy)), nil
}

func (s *Service) Import(data json.RawMessage, sourceFormat, strategy string) (*ImportPlan, error) {
	pkg, detectedFormat, err := ParsePackage(data, sourceFormat)
	if err != nil {
		return nil, err
	}
	strategy = normalizeStrategy(strategy)

	var plan *ImportPlan
	err = s.db.Transaction(func(tx *gorm.DB) error {
		hasOwnership := packageHasOwnership(pkg)
		state, err := s.loadCurrentState(tx, hasOwnership, hasOwnership)
		if err != nil {
			return err
		}
		plan = buildPlan(pkg, state, detectedFormat, strategy)
		return applyPackage(tx, pkg, state, strategy)
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func ParsePackage(data json.RawMessage, sourceFormat string) (*Package, string, error) {
	if len(data) > MaxPackageBytes {
		return nil, "", newPackageLimitError("raw_bytes", len(data), MaxPackageBytes)
	}
	sourceFormat = strings.TrimSpace(strings.ToLower(sourceFormat))
	if sourceFormat == "" {
		sourceFormat = SourceAuto
	}

	if sourceFormat == SourceAutoRSS || sourceFormat == SourceAuto {
		pkg, err := parseAutoRSSPackage(data)
		if err == nil {
			return pkg, SourceAutoRSS, nil
		}
		if errors.Is(err, ErrPackageTooLarge) {
			return nil, "", err
		}
		if sourceFormat == SourceAutoRSS {
			return nil, "", err
		}
	}

	if sourceFormat == SourceAutoBangumi || sourceFormat == SourceAuto {
		pkg, err := parseAutoBangumiPackage(data)
		if err == nil {
			return pkg, SourceAutoBangumi, nil
		}
		if errors.Is(err, ErrPackageTooLarge) {
			return nil, "", err
		}
		if sourceFormat == SourceAutoBangumi {
			return nil, "", err
		}
	}

	return nil, "", errors.New("unsupported or unrecognized backup format")
}

func parseAutoRSSPackage(data json.RawMessage) (*Package, error) {
	var pkg Package
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("invalid auto-rss backup JSON: %w", err)
	}

	if pkg.App != "" && pkg.App != "auto-rss" {
		return nil, fmt.Errorf("not an auto-rss backup package: app=%s", pkg.App)
	}
	if len(pkg.Subscriptions) == 0 && len(pkg.Episodes) == 0 && len(pkg.EpisodeCandidates) == 0 &&
		len(pkg.Configs) == 0 && len(pkg.RSSSources) == 0 && len(pkg.Groups) == 0 &&
		len(pkg.Tags) == 0 && len(pkg.NotificationSettings) == 0 {
		var legacy struct {
			Data struct {
				Subscriptions []model.Subscription `json:"subscriptions"`
			} `json:"data"`
			Subscriptions []model.Subscription `json:"subscriptions"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, err
		}
		sourceSubs := legacy.Subscriptions
		if len(sourceSubs) == 0 {
			sourceSubs = legacy.Data.Subscriptions
		}
		if len(sourceSubs) == 0 {
			return nil, errors.New("auto-rss backup contains no importable resources")
		}
		for _, sub := range sourceSubs {
			pkg.Subscriptions = append(pkg.Subscriptions, SubscriptionRecord{Subscription: sub})
		}
	}
	if pkg.SchemaVersion == "" {
		pkg.SchemaVersion = legacySchemaVersion
	}
	if pkg.SchemaVersion != legacySchemaVersion && pkg.SchemaVersion != ledgerSchemaVersion && pkg.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported auto-rss backup schema version %q", pkg.SchemaVersion)
	}
	for i := range pkg.Subscriptions {
		ensureSubscriptionRecordFeeds(&pkg.Subscriptions[i])
	}
	if pkg.App == "" {
		pkg.App = "auto-rss"
	}
	if err := validatePackage(&pkg); err != nil {
		return nil, err
	}
	pkg.recount()
	return &pkg, nil
}

func parseAutoBangumiPackage(data json.RawMessage) (*Package, error) {
	rules, err := extractAutoBangumiRules(data)
	if err != nil {
		return nil, err
	}

	pkg := &Package{
		SchemaVersion:        SchemaVersion,
		App:                  "auto-rss",
		ExportedAt:           time.Now().UTC(),
		IncludesSensitive:    false,
		SensitivePlaceholder: RedactedValue,
	}

	for _, rule := range rules {
		name := firstNonEmpty(rule, "official_title", "officialTitle", "title", "name")
		links := stringSliceFromValue(rule["rss_link"])
		if len(links) == 0 {
			links = stringSliceFromValue(rule["rssLink"])
		}
		if len(links) == 0 {
			links = stringSliceFromValue(rule["rss_url"])
		}
		if len(links) == 0 {
			links = stringSliceFromValue(rule["rssUrl"])
		}
		if name == "" || len(links) == 0 {
			continue
		}

		season := intFromValue(rule["season"], 1)
		if season <= 0 {
			season = 1
		}
		filters := stringSliceFromValue(rule["filter"])
		if len(filters) == 0 {
			filters = stringSliceFromValue(rule["filters"])
		}
		filterJSON := ""
		if len(filters) > 0 {
			if b, err := json.Marshal(filters); err == nil {
				filterJSON = string(b)
			}
		}

		deleted := boolFromValue(rule["deleted"], false)
		disabled := boolFromValue(rule["disabled"], false)
		enabled := !deleted && !disabled
		if rawEnabled, ok := rule["enabled"]; ok {
			enabled = boolFromValue(rawEnabled, enabled)
		}

		for _, link := range links {
			link = strings.TrimSpace(link)
			if link == "" {
				continue
			}
			pkg.Subscriptions = append(pkg.Subscriptions, SubscriptionRecord{
				Subscription: model.Subscription{
					Name:           name,
					RssURL:         link,
					Season:         season,
					Status:         "active",
					Enabled:        enabled,
					Fansub:         firstNonEmpty(rule, "group_name", "groupName", "group", "fansub"),
					FilterKeywords: filterJSON,
					FilterRules:    strings.Join(filters, "\n"),
					BangumiCover:   firstNonEmpty(rule, "poster_link", "posterLink", "poster", "cover"),
					SourceType:     "manual",
				},
			})
		}
	}

	if len(pkg.Subscriptions) == 0 {
		return nil, errors.New("auto-bangumi data contains no importable RSS subscriptions")
	}
	if err := validatePackage(pkg); err != nil {
		return nil, err
	}
	pkg.recount()
	return pkg, nil
}

func extractAutoBangumiRules(data json.RawMessage) ([]map[string]any, error) {
	var list []map[string]any
	if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
		return list, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("invalid auto-bangumi JSON: %w", err)
	}

	for _, key := range []string{"bangumi", "bangumis", "rules", "items", "data", "list"} {
		raw, ok := object[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
			return list, nil
		}
	}

	var single map[string]any
	if err := json.Unmarshal(data, &single); err == nil {
		if _, ok := single["rss_link"]; ok {
			return []map[string]any{single}, nil
		}
		if _, ok := single["rssLink"]; ok {
			return []map[string]any{single}, nil
		}
	}

	return nil, errors.New("auto-bangumi JSON does not contain a bangumi rule list")
}

func buildPlan(pkg *Package, state *currentState, sourceFormat, strategy string) *ImportPlan {
	plan := &ImportPlan{
		SourceFormat: sourceFormat,
		Strategy:     strategy,
		Items:        []ImportItem{},
	}

	for _, group := range pkg.Groups {
		key := normalizedKey(group.Name)
		if key == "" {
			plan.add("group", "", group.Name, "skip", "missing group name", false, false)
			continue
		}
		_, exists := state.groups[key]
		plan.addExistingAware("group", key, group.Name, exists, strategy, false)
	}

	for _, tag := range pkg.Tags {
		key := normalizedKey(tag.Name)
		if key == "" {
			plan.add("tag", "", tag.Name, "skip", "missing tag name", false, false)
			continue
		}
		_, exists := state.tags[key]
		plan.addExistingAware("tag", key, tag.Name, exists, strategy, false)
	}

	for _, source := range pkg.RSSSources {
		key := rssSourceKey(source)
		if key == "" {
			plan.add("rss_source", "", source.Name, "skip", "missing RSS source name and URL", false, false)
			continue
		}
		_, exists := state.rssSources[key]
		plan.addExistingAware("rss_source", key, source.Name, exists, strategy, false)
	}

	for _, cfg := range pkg.Configs {
		key := normalizedKey(cfg.Key)
		if key == "" {
			plan.add("config", "", cfg.Key, "skip", "missing config key", false, false)
			continue
		}
		if isRedactedConfig(cfg) {
			plan.add("config", key, cfg.Key, "skip", "sensitive value redacted in backup", false, true)
			continue
		}
		_, exists := state.configs[key]
		plan.addExistingAware("config", key, cfg.Key, exists, strategy, cfg.Sensitive)
	}

	for _, setting := range pkg.NotificationSettings {
		key := normalizedKey(setting.Channel)
		if key == "" {
			plan.add("notification_setting", "", setting.Channel, "skip", "missing channel", false, true)
			continue
		}
		if isRedactedNotification(setting) {
			plan.add("notification_setting", key, setting.Channel, "skip", "notification config redacted in backup", false, true)
			continue
		}
		_, exists := state.settings[key]
		plan.addExistingAware("notification_setting", key, setting.Channel, exists, strategy, true)
	}

	for _, sub := range pkg.Subscriptions {
		key := subscriptionKeyFromRecord(sub)
		if key == "" {
			plan.add("subscription", "", sub.Name, "skip", "missing subscription name or RSS URL", false, false)
			continue
		}
		_, exists := state.subscriptions[key]
		plan.addExistingAware("subscription", key, sub.Name, exists, strategy, false)
	}

	for _, relation := range pkg.SubscriptionTags {
		key := normalizedKey(relation.SubscriptionKey + "|" + relation.TagName)
		if key == "" || relation.TagName == "" {
			plan.add("subscription_tag", key, relation.Subscription, "skip", "missing subscription or tag", false, false)
			continue
		}
		plan.add("subscription_tag", key, relation.Subscription, "merge", "tag relation will be ensured when both records exist", false, false)
	}

	for _, record := range pkg.Episodes {
		key := episodeRecordKey(record.SubscriptionKey, record.Episode)
		_, exists := state.episodes[key]
		plan.addExistingAware("episode", key, fmt.Sprintf("episode %d", record.Episode), exists, strategy, false)
	}

	for _, record := range pkg.EpisodeCandidates {
		episodeKey := episodeRecordKey(record.SubscriptionKey, record.Episode)
		key := candidateStateKey(episodeKey, candidateResourceKey(record))
		_, exists := state.candidates[key]
		plan.addExistingAware("episode_candidate", key, record.Title, exists, strategy, false)
	}

	return plan
}

func applyPackage(tx *gorm.DB, pkg *Package, state *currentState, strategy string) error {
	for _, group := range pkg.Groups {
		key := normalizedKey(group.Name)
		if key == "" {
			continue
		}
		existing, exists := state.groups[key]
		group.ID = 0
		group.CreatedAt = time.Time{}
		group.UpdatedAt = time.Time{}
		if !exists {
			if err := tx.Create(&group).Error; err != nil {
				return err
			}
			state.groups[key] = group
			continue
		}
		if strategy == StrategyOverwrite {
			group.ID = existing.ID
			group.CreatedAt = existing.CreatedAt
			if err := tx.Save(&group).Error; err != nil {
				return err
			}
			state.groups[key] = group
		} else if strategy == StrategyMerge {
			changed := false
			if existing.Description == "" && group.Description != "" {
				existing.Description = group.Description
				changed = true
			}
			if (existing.Color == "" || existing.Color == "#18a058") && group.Color != "" {
				existing.Color = group.Color
				changed = true
			}
			if existing.SortOrder == 0 && group.SortOrder != 0 {
				existing.SortOrder = group.SortOrder
				changed = true
			}
			if changed {
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				state.groups[key] = existing
			}
		}
	}

	for _, tag := range pkg.Tags {
		key := normalizedKey(tag.Name)
		if key == "" {
			continue
		}
		existing, exists := state.tags[key]
		tag.ID = 0
		tag.CreatedAt = time.Time{}
		tag.UpdatedAt = time.Time{}
		if !exists {
			if err := tx.Create(&tag).Error; err != nil {
				return err
			}
			state.tags[key] = tag
			continue
		}
		if strategy == StrategyOverwrite {
			tag.ID = existing.ID
			tag.CreatedAt = existing.CreatedAt
			if err := tx.Save(&tag).Error; err != nil {
				return err
			}
			state.tags[key] = tag
		} else if strategy == StrategyMerge {
			changed := false
			if existing.Description == "" && tag.Description != "" {
				existing.Description = tag.Description
				changed = true
			}
			if (existing.Color == "" || existing.Color == "#18a058") && tag.Color != "" {
				existing.Color = tag.Color
				changed = true
			}
			if existing.SortOrder == 0 && tag.SortOrder != 0 {
				existing.SortOrder = tag.SortOrder
				changed = true
			}
			if changed {
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				state.tags[key] = existing
			}
		}
	}

	for _, source := range pkg.RSSSources {
		key := rssSourceKey(source)
		if key == "" {
			continue
		}
		existing, exists := state.rssSources[key]
		source.ID = 0
		source.CreatedAt = time.Time{}
		source.UpdatedAt = time.Time{}
		if !exists {
			if err := tx.Create(&source).Error; err != nil {
				return err
			}
			state.rssSources[key] = source
			continue
		}
		if strategy == StrategyOverwrite {
			source.ID = existing.ID
			source.CreatedAt = existing.CreatedAt
			if err := tx.Save(&source).Error; err != nil {
				return err
			}
			state.rssSources[key] = source
		} else if strategy == StrategyMerge {
			changed := false
			if existing.BaseURL == "" && source.BaseURL != "" {
				existing.BaseURL = source.BaseURL
				changed = true
			}
			if existing.Description == "" && source.Description != "" {
				existing.Description = source.Description
				changed = true
			}
			if changed {
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				state.rssSources[key] = existing
			}
		}
	}

	for _, cfg := range pkg.Configs {
		key := normalizedKey(cfg.Key)
		if key == "" || isRedactedConfig(cfg) {
			continue
		}
		existing, exists := state.configs[key]
		if !exists {
			next := model.Config{Key: cfg.Key, Value: cfg.Value, Description: cfg.Description}
			if err := tx.Create(&next).Error; err != nil {
				return err
			}
			state.configs[key] = next
			continue
		}
		if strategy == StrategyOverwrite {
			existing.Value = cfg.Value
			if cfg.Description != "" {
				existing.Description = cfg.Description
			}
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
			state.configs[key] = existing
		}
	}

	for _, setting := range pkg.NotificationSettings {
		key := normalizedKey(setting.Channel)
		if key == "" || isRedactedNotification(setting) {
			continue
		}
		existing, exists := state.settings[key]
		if !exists {
			next := model.NotificationSetting{
				Channel: setting.Channel,
				Enabled: setting.Enabled,
				Config:  setting.Config,
			}
			if err := tx.Create(&next).Error; err != nil {
				return err
			}
			state.settings[key] = next
			continue
		}
		if strategy == StrategyOverwrite {
			existing.Enabled = setting.Enabled
			existing.Config = setting.Config
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
			state.settings[key] = existing
		}
	}

	for _, record := range pkg.Subscriptions {
		key := subscriptionKeyFromRecord(record)
		if key == "" {
			continue
		}
		existing, exists := state.subscriptions[key]
		sub := prepareSubscription(record, state)
		if !exists {
			if err := tx.Create(&sub).Error; err != nil {
				return err
			}
			if err := restoreSubscriptionFeeds(tx, record, sub.ID, StrategyOverwrite); err != nil {
				return err
			}
			state.subscriptions[key] = sub
			continue
		}
		if strategy == StrategyOverwrite {
			sub.ID = existing.ID
			sub.CreatedAt = existing.CreatedAt
			if err := tx.Save(&sub).Error; err != nil {
				return err
			}
			if err := restoreSubscriptionFeeds(tx, record, sub.ID, StrategyOverwrite); err != nil {
				return err
			}
			state.subscriptions[key] = sub
		} else if strategy == StrategyMerge {
			if mergeSubscription(&existing, sub) {
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				state.subscriptions[key] = existing
			}
			if err := restoreSubscriptionFeeds(tx, record, existing.ID, StrategyMerge); err != nil {
				return err
			}
		}
	}

	for _, record := range pkg.Episodes {
		subscriptionKey := normalizedKey(record.SubscriptionKey)
		sub := state.subscriptions[subscriptionKey]
		key := episodeRecordKey(subscriptionKey, record.Episode)
		existing, exists := state.episodes[key]
		if !exists || strategy == StrategyOverwrite {
			normalizeEpisodeRecord(&record)
		}
		next := model.SubscriptionEpisode{
			SubscriptionID:    sub.ID,
			Episode:           record.Episode,
			Status:            record.Status,
			ActiveDownloadID:  nil,
			ActiveTorrentHash: record.ActiveTorrentHash,
			ActiveTorrentURL:  record.ActiveTorrentURL,
			ActiveTitle:       record.ActiveTitle,
			StatusSource:      record.StatusSource,
			DownloadedAt:      record.DownloadedAt,
		}
		if !exists {
			if err := tx.Create(&next).Error; err != nil {
				return err
			}
			state.episodes[key] = next
			continue
		}
		if strategy == StrategyOverwrite {
			next.ID = existing.ID
			next.CreatedAt = existing.CreatedAt
			if err := tx.Save(&next).Error; err != nil {
				return err
			}
			state.episodes[key] = next
		}
	}

	for _, record := range pkg.EpisodeCandidates {
		normalizeCandidateRecord(&record)
		episodeKey := episodeRecordKey(record.SubscriptionKey, record.Episode)
		ledger := state.episodes[episodeKey]
		resourceKey := candidateResourceKey(record)
		key := candidateStateKey(episodeKey, resourceKey)
		existing, exists := state.candidates[key]
		next := model.EpisodeResourceCandidate{
			SubscriptionEpisodeID: ledger.ID,
			ResourceKey:           resourceKey,
			TorrentHash:           record.TorrentHash,
			TorrentURL:            record.TorrentURL,
			Title:                 record.Title,
			Fansub:                record.Fansub,
			Language:              record.Language,
			PubTime:               record.PubTime,
			SourceRSSURL:          record.SourceRSSURL,
			Status:                record.Status,
			FailureReason:         record.FailureReason,
		}
		if !exists {
			if err := tx.Create(&next).Error; err != nil {
				return err
			}
			state.candidates[key] = next
			continue
		}
		if strategy == StrategyOverwrite {
			next.ID = existing.ID
			next.CreatedAt = existing.CreatedAt
			if err := tx.Save(&next).Error; err != nil {
				return err
			}
			state.candidates[key] = next
		}
	}

	for _, relation := range pkg.SubscriptionTags {
		tag, ok := state.tags[normalizedKey(relation.TagName)]
		if !ok {
			continue
		}
		sub, ok := state.subscriptions[normalizedKey(relation.SubscriptionKey)]
		if !ok {
			continue
		}
		next := model.SubscriptionTagRelation{
			SubscriptionID: sub.ID,
			TagID:          tag.ID,
		}
		err := tx.Where("subscription_id = ? AND tag_id = ?", next.SubscriptionID, next.TagID).
			FirstOrCreate(&next).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func prepareSubscription(record SubscriptionRecord, state *currentState) model.Subscription {
	sub := record.Subscription
	sub.ID = 0
	sub.CreatedAt = time.Time{}
	sub.UpdatedAt = time.Time{}
	sub.LastCheckTime = nil
	sub.LastDownloadAt = nil
	sub.LastRSSPubTime = nil
	sub.CompletedAt = nil
	sub.Group = nil
	sub.RSSSource = nil
	sub.Downloads = nil
	sub.Feeds = nil

	if record.GroupName != "" {
		if group, ok := state.groups[normalizedKey(record.GroupName)]; ok {
			groupID := group.ID
			sub.GroupID = &groupID
		}
	}
	if record.RSSSourceName != "" {
		if source, ok := state.rssSources[normalizedKey(record.RSSSourceName)]; ok {
			sourceID := source.ID
			sub.RSSSourceID = &sourceID
		}
	}
	if sub.Season <= 0 {
		sub.Season = 1
	}
	if sub.Status == "" {
		sub.Status = "active"
	}
	if sub.SourceType == "" {
		sub.SourceType = "manual"
	}
	return sub
}

func sanitizeFeedForBackup(feed model.SubscriptionFeed) model.SubscriptionFeed {
	feed.ID = 0
	feed.SubscriptionID = 0
	feed.RSSURLNormalized = ""
	feed.LastRSSPubTime = nil
	feed.BaselinePending = false
	feed.LastCheckTime = nil
	feed.LastSuccessAt = nil
	feed.LastError = ""
	feed.CreatedAt = time.Time{}
	feed.UpdatedAt = time.Time{}
	return feed
}

func ensureSubscriptionRecordFeeds(record *SubscriptionRecord) {
	if len(record.Feeds) > 0 || strings.TrimSpace(record.RssURL) == "" {
		return
	}
	record.Feeds = []model.SubscriptionFeed{{
		Name:          "Default",
		Fansub:        record.Fansub,
		RSSURL:        record.RssURL,
		EpisodeOffset: record.EpisodeOffset,
		Enabled:       true,
	}}
}

func restoreSubscriptionFeeds(tx *gorm.DB, record SubscriptionRecord, subscriptionID uint, strategy string) error {
	feeds := append([]model.SubscriptionFeed(nil), record.Feeds...)
	repo := repository.NewSubscriptionFeedRepository(tx)
	if strategy == StrategyOverwrite {
		existing, err := repo.ListBySubscription(subscriptionID)
		if err != nil {
			return err
		}
		for _, feed := range existing {
			if err := repo.DeleteInTx(tx, feed.ID); err != nil {
				return err
			}
		}
	}
	if len(feeds) == 0 {
		return nil
	}
	existingByURL := make(map[string]struct{})
	if strategy == StrategyMerge {
		existing, err := repo.ListBySubscription(subscriptionID)
		if err != nil {
			return err
		}
		for _, feed := range existing {
			existingByURL[feed.RSSURLNormalized] = struct{}{}
		}
	}
	for _, feed := range feeds {
		normalizedURL := utils.NormalizeFeedURL(feed.RSSURL)
		if normalizedURL == "" {
			return fmt.Errorf("subscription %d has invalid feed URL %q", subscriptionID, feed.RSSURL)
		}
		if _, exists := existingByURL[normalizedURL]; exists {
			continue
		}
		feed = sanitizeFeedForBackup(feed)
		feed.SubscriptionID = subscriptionID
		feed.RSSURL = strings.TrimSpace(feed.RSSURL)
		feed.RSSURLNormalized = normalizedURL
		if strings.TrimSpace(feed.Name) == "" {
			feed.Name = "Default"
		}
		feed.BaselinePending = true
		if err := repo.CreateInTx(tx, &feed); err != nil {
			return err
		}
		existingByURL[normalizedURL] = struct{}{}
	}
	return nil
}

func mergeSubscription(dst *model.Subscription, src model.Subscription) bool {
	changed := false
	mergeString := func(target *string, incoming string) {
		if *target == "" && incoming != "" {
			*target = incoming
			changed = true
		}
	}
	mergeInt := func(target *int, incoming int) {
		if *target == 0 && incoming != 0 {
			*target = incoming
			changed = true
		}
	}
	mergeFloat := func(target *float64, incoming float64) {
		if *target == 0 && incoming != 0 {
			*target = incoming
			changed = true
		}
	}

	mergeString(&dst.RssURL, src.RssURL)
	mergeString(&dst.Status, src.Status)
	mergeString(&dst.FilterKeywords, src.FilterKeywords)
	mergeString(&dst.ExcludeKeywords, src.ExcludeKeywords)
	mergeString(&dst.Fansub, src.Fansub)
	mergeString(&dst.Language, src.Language)
	mergeString(&dst.UpdateDay, src.UpdateDay)
	mergeString(&dst.FilterRules, src.FilterRules)
	mergeString(&dst.CollectionTorrent, src.CollectionTorrent)
	mergeString(&dst.LanguagePreference, src.LanguagePreference)
	mergeString(&dst.BangumiSummary, src.BangumiSummary)
	mergeString(&dst.BangumiCover, src.BangumiCover)
	mergeString(&dst.BangumiCoverLocal, src.BangumiCoverLocal)
	mergeString(&dst.AirDate, src.AirDate)
	mergeString(&dst.AirDay, src.AirDay)
	mergeString(&dst.AirTime, src.AirTime)
	mergeString(&dst.AirTimezone, src.AirTimezone)
	mergeString(&dst.Tags, src.Tags)
	mergeString(&dst.SourceType, src.SourceType)

	mergeInt(&dst.Season, src.Season)
	mergeInt(&dst.TotalEpisodes, src.TotalEpisodes)
	mergeInt(&dst.CurrentEpisode, src.CurrentEpisode)
	mergeInt(&dst.LatestEpisode, src.LatestEpisode)
	mergeInt(&dst.EpisodeOffset, src.EpisodeOffset)
	mergeInt(&dst.BangumiID, src.BangumiID)
	mergeInt(&dst.BangumiRank, src.BangumiRank)
	mergeInt(&dst.BangumiSeason, src.BangumiSeason)
	mergeInt(&dst.AirYear, src.AirYear)
	mergeInt(&dst.NotifyBeforeMin, src.NotifyBeforeMin)
	mergeFloat(&dst.BangumiScore, src.BangumiScore)

	if dst.GroupID == nil && src.GroupID != nil {
		dst.GroupID = src.GroupID
		changed = true
	}
	if dst.RSSSourceID == nil && src.RSSSourceID != nil {
		dst.RSSSourceID = src.RSSSourceID
		changed = true
	}
	if dst.SubgroupID == nil && src.SubgroupID != nil {
		dst.SubgroupID = src.SubgroupID
		changed = true
	}
	return changed
}

func (s *Service) loadCurrentState(db *gorm.DB, includeOwnership, requireUnique bool) (*currentState, error) {
	state := &currentState{
		configs:       map[string]model.Config{},
		rssSources:    map[string]model.RSSSource{},
		groups:        map[string]model.SubscriptionGroup{},
		tags:          map[string]model.SubscriptionTag{},
		subscriptions: map[string]model.Subscription{},
		episodes:      map[string]model.SubscriptionEpisode{},
		candidates:    map[string]model.EpisodeResourceCandidate{},
		settings:      map[string]model.NotificationSetting{},
	}

	var configs []model.Config
	if err := db.Find(&configs).Error; err != nil {
		return nil, err
	}
	for _, cfg := range configs {
		state.configs[normalizedKey(cfg.Key)] = cfg
	}

	var rssSources []model.RSSSource
	if err := db.Find(&rssSources).Error; err != nil {
		return nil, err
	}
	for _, source := range rssSources {
		state.rssSources[rssSourceKey(source)] = source
	}

	var groups []model.SubscriptionGroup
	if err := db.Find(&groups).Error; err != nil {
		return nil, err
	}
	for _, group := range groups {
		state.groups[normalizedKey(group.Name)] = group
	}

	var tags []model.SubscriptionTag
	if err := db.Find(&tags).Error; err != nil {
		return nil, err
	}
	for _, tag := range tags {
		state.tags[normalizedKey(tag.Name)] = tag
	}

	var subscriptions []model.Subscription
	if err := db.Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	subscriptionKeyByID := make(map[uint]string, len(subscriptions))
	for _, sub := range subscriptions {
		key := subscriptionKeyFromModel(sub)
		if requireUnique && key == "" {
			return nil, fmt.Errorf("subscription %d has no stable backup key", sub.ID)
		}
		if existing, exists := state.subscriptions[key]; requireUnique && exists && existing.ID != sub.ID {
			return nil, fmt.Errorf("subscription backup key collision %q", key)
		}
		state.subscriptions[key] = sub
		subscriptionKeyByID[sub.ID] = key
	}
	if includeOwnership {
		var episodes []model.SubscriptionEpisode
		if err := db.Find(&episodes).Error; err != nil {
			return nil, err
		}
		episodeKeyByID := make(map[uint]string, len(episodes))
		for _, ledger := range episodes {
			subscriptionKey, ok := subscriptionKeyByID[ledger.SubscriptionID]
			if !ok {
				return nil, fmt.Errorf("episode %d references missing subscription %d", ledger.ID, ledger.SubscriptionID)
			}
			key := episodeRecordKey(subscriptionKey, ledger.Episode)
			if existing, exists := state.episodes[key]; exists && existing.ID != ledger.ID {
				return nil, fmt.Errorf("episode backup key collision %q", key)
			}
			state.episodes[key] = ledger
			episodeKeyByID[ledger.ID] = key
		}

		var candidates []model.EpisodeResourceCandidate
		if err := db.Find(&candidates).Error; err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			episodeKey, ok := episodeKeyByID[candidate.SubscriptionEpisodeID]
			if !ok {
				return nil, fmt.Errorf("candidate %d references missing episode %d", candidate.ID, candidate.SubscriptionEpisodeID)
			}
			resourceKey := episode.ResourceKey(model.EpisodeResource{
				Hash: candidate.TorrentHash,
				URL:  candidate.TorrentURL,
			})
			if resourceKey == "" {
				return nil, fmt.Errorf("candidate %d has no stable resource identity", candidate.ID)
			}
			key := candidateStateKey(episodeKey, resourceKey)
			if existing, exists := state.candidates[key]; exists && existing.ID != candidate.ID {
				return nil, fmt.Errorf("candidate backup key collision %q", key)
			}
			state.candidates[key] = candidate
		}
	}

	var settings []model.NotificationSetting
	if err := db.Find(&settings).Error; err != nil {
		return nil, err
	}
	for _, setting := range settings {
		state.settings[normalizedKey(setting.Channel)] = setting
	}

	return state, nil
}

func (pkg *Package) recount() {
	pkg.Summary = PackageSummary{
		Configs:              len(pkg.Configs),
		RSSSources:           len(pkg.RSSSources),
		Groups:               len(pkg.Groups),
		Tags:                 len(pkg.Tags),
		Subscriptions:        len(pkg.Subscriptions),
		Episodes:             len(pkg.Episodes),
		EpisodeCandidates:    len(pkg.EpisodeCandidates),
		SubscriptionTags:     len(pkg.SubscriptionTags),
		NotificationSettings: len(pkg.NotificationSettings),
	}
}

func (plan *ImportPlan) addExistingAware(resource, key, name string, exists bool, strategy string, sensitive bool) {
	if !exists {
		plan.add(resource, key, name, "create", "not present locally", false, sensitive)
		return
	}
	switch strategy {
	case StrategyOverwrite:
		plan.add(resource, key, name, "overwrite", "matching local record will be overwritten", true, sensitive)
	case StrategyMerge:
		plan.add(resource, key, name, "merge", "matching local record will be merged", true, sensitive)
	default:
		plan.add(resource, key, name, "skip", "matching local record already exists", true, sensitive)
	}
}

func (plan *ImportPlan) add(resource, key, name, action, reason string, conflict, sensitive bool) {
	item := ImportItem{
		Resource:  resource,
		Key:       key,
		Name:      name,
		Action:    action,
		Reason:    reason,
		Conflict:  conflict,
		Sensitive: sensitive,
	}
	plan.Items = append(plan.Items, item)
	plan.Summary.Total++
	switch action {
	case "create":
		plan.Summary.Create++
	case "overwrite":
		plan.Summary.Overwrite++
	case "merge":
		plan.Summary.Merge++
	case "skip":
		plan.Summary.Skip++
	}
	if sensitive && action == "skip" && strings.Contains(reason, "redacted") {
		plan.Summary.SensitiveSkipped++
	}
}

func isSensitiveConfigKey(key string) bool {
	lower := strings.ToLower(key)
	for _, token := range []string{"password", "token", "secret", "api_key", "apikey", "cookie", "credential", "access_key", "refresh_token", "jwt"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func isRedactedConfig(cfg ConfigRecord) bool {
	return cfg.Redacted || cfg.Value == RedactedValue
}

func isRedactedNotification(setting NotificationSettingRecord) bool {
	return setting.Redacted || setting.Config == RedactedValue
}

func normalizeStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case StrategyOverwrite:
		return StrategyOverwrite
	case StrategyMerge:
		return StrategyMerge
	default:
		return StrategySkip
	}
}

func normalizedKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func rssSourceKey(source model.RSSSource) string {
	if source.Name != "" {
		return normalizedKey(source.Name)
	}
	return normalizedKey(source.BaseURL)
}

func subscriptionKeyFromRecord(record SubscriptionRecord) string {
	return subscriptionKey(record.RssURL, record.Name, record.Season)
}

func subscriptionKeyFromModel(sub model.Subscription) string {
	return subscriptionKey(sub.RssURL, sub.Name, sub.Season)
}

func subscriptionKey(rssURL, name string, season int) string {
	if season <= 0 {
		season = 1
	}
	base := normalizedKey(rssURL)
	if base == "" {
		base = normalizedKey(name)
	}
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s|season:%d", base, season)
}

func episodeRecordKey(subscriptionKey string, episodeNumber int) string {
	return fmt.Sprintf("%s|episode:%d", normalizedKey(subscriptionKey), episodeNumber)
}

func candidateResourceKey(record CandidateRecord) string {
	return episode.ResourceKey(model.EpisodeResource{
		Hash: record.TorrentHash,
		URL:  record.TorrentURL,
	})
}

func candidateStateKey(episodeKey, resourceKey string) string {
	return episodeKey + "|resource:" + resourceKey
}

func packageHasOwnership(pkg *Package) bool {
	return len(pkg.Episodes) > 0 || len(pkg.EpisodeCandidates) > 0
}

type packageItemCounts struct {
	Configs              int
	RSSSources           int
	Groups               int
	Tags                 int
	Subscriptions        int
	Episodes             int
	Candidates           int
	SubscriptionTags     int
	NotificationSettings int
}

func packageCounts(pkg *Package) packageItemCounts {
	return packageItemCounts{
		Configs:              len(pkg.Configs),
		RSSSources:           len(pkg.RSSSources),
		Groups:               len(pkg.Groups),
		Tags:                 len(pkg.Tags),
		Subscriptions:        len(pkg.Subscriptions),
		Episodes:             len(pkg.Episodes),
		Candidates:           len(pkg.EpisodeCandidates),
		SubscriptionTags:     len(pkg.SubscriptionTags),
		NotificationSettings: len(pkg.NotificationSettings),
	}
}

func (counts packageItemCounts) total() int {
	return counts.Configs + counts.RSSSources + counts.Groups + counts.Tags + counts.Subscriptions +
		counts.Episodes + counts.Candidates + counts.SubscriptionTags + counts.NotificationSettings
}

func validatePackageItemCounts(counts packageItemCounts) error {
	for _, limit := range []struct {
		field  string
		actual int
		max    int
	}{
		{field: "subscriptions", actual: counts.Subscriptions, max: MaxSubscriptionsPerPackage},
		{field: "episodes", actual: counts.Episodes, max: MaxEpisodesPerPackage},
		{field: "episode_candidates", actual: counts.Candidates, max: MaxCandidatesPerPackage},
		{field: "total_items", actual: counts.total(), max: MaxPackageItems},
	} {
		if limit.actual > limit.max {
			return newPackageLimitError(limit.field, limit.actual, limit.max)
		}
	}
	return nil
}

func validateStableStringBytes(field, value string, limit int) error {
	if len(value) > limit {
		return newPackageLimitError(field, len(value), limit)
	}
	return nil
}

func validateOwnershipStringLimits(pkg *Package) error {
	for i, record := range pkg.Episodes {
		for _, field := range []struct {
			name  string
			value string
			max   int
		}{
			{name: "subscription_key", value: record.SubscriptionKey, max: MaxSubscriptionKeyBytes},
			{name: "active_torrent_hash", value: record.ActiveTorrentHash, max: MaxTorrentHashBytes},
			{name: "active_torrent_url", value: record.ActiveTorrentURL, max: MaxURLBytes},
			{name: "active_title", value: record.ActiveTitle, max: MaxTitleBytes},
			{name: "status", value: record.Status, max: MaxStatusBytes},
			{name: "status_source", value: record.StatusSource, max: MaxStatusBytes},
		} {
			if err := validateStableStringBytes(fmt.Sprintf("episodes[%d].%s", i, field.name), field.value, field.max); err != nil {
				return err
			}
		}
	}
	for i, record := range pkg.EpisodeCandidates {
		for _, field := range []struct {
			name  string
			value string
			max   int
		}{
			{name: "subscription_key", value: record.SubscriptionKey, max: MaxSubscriptionKeyBytes},
			{name: "torrent_hash", value: record.TorrentHash, max: MaxTorrentHashBytes},
			{name: "torrent_url", value: record.TorrentURL, max: MaxURLBytes},
			{name: "title", value: record.Title, max: MaxTitleBytes},
			{name: "fansub", value: record.Fansub, max: MaxFansubBytes},
			{name: "language", value: record.Language, max: MaxLanguageBytes},
			{name: "source_rss_url", value: record.SourceRSSURL, max: MaxURLBytes},
			{name: "status", value: record.Status, max: MaxStatusBytes},
			{name: "failure_reason", value: record.FailureReason, max: MaxFailureReasonBytes},
			{name: "replacement_stage", value: record.runtimeStage, max: MaxStatusBytes},
		} {
			if err := validateStableStringBytes(fmt.Sprintf("episode_candidates[%d].%s", i, field.name), field.value, field.max); err != nil {
				return err
			}
		}
	}
	return nil
}

func newPackageLimitError(field string, actual, limit int) error {
	return &PackageLimitError{Field: field, Actual: actual, Limit: limit}
}

func validatePackage(pkg *Package) error {
	// Raw JSON is capped first. Count and field limits remain defense in depth for
	// exports, direct validation, and future non-JSON inputs; they do not promise
	// that a 200k-candidate JSON package fits within MaxPackageBytes.
	if err := validatePackageItemCounts(packageCounts(pkg)); err != nil {
		return err
	}
	if err := validateOwnershipStringLimits(pkg); err != nil {
		return err
	}
	hasOwnership := packageHasOwnership(pkg)
	subscriptionKeys := make(map[string]struct{}, len(pkg.Subscriptions))
	for i, record := range pkg.Subscriptions {
		key := subscriptionKeyFromRecord(record)
		if hasOwnership && key == "" {
			return fmt.Errorf("subscriptions[%d]: missing stable subscription key", i)
		}
		if _, exists := subscriptionKeys[key]; hasOwnership && exists {
			return fmt.Errorf("subscriptions[%d]: subscription key collision %q", i, key)
		}
		subscriptionKeys[key] = struct{}{}
	}

	episodeKeys := make(map[string]struct{}, len(pkg.Episodes))
	for i, record := range pkg.Episodes {
		subscriptionKey := normalizedKey(record.SubscriptionKey)
		if _, exists := subscriptionKeys[subscriptionKey]; !exists {
			return fmt.Errorf("episodes[%d]: missing subscription %q", i, record.SubscriptionKey)
		}
		if record.Episode <= 0 || record.Episode > model.MaxSubscriptionEpisodes {
			return fmt.Errorf("episodes[%d]: invalid episode %d", i, record.Episode)
		}
		if !validEpisodeStatus(record.Status) {
			return fmt.Errorf("episodes[%d]: invalid status %q", i, record.Status)
		}
		if !validEpisodeStatusSource(record.StatusSource) {
			return fmt.Errorf("episodes[%d]: invalid status_source %q", i, record.StatusSource)
		}
		key := episodeRecordKey(subscriptionKey, record.Episode)
		if _, exists := episodeKeys[key]; exists {
			return fmt.Errorf("episodes[%d]: episode key collision %q", i, key)
		}
		episodeKeys[key] = struct{}{}
		pkg.Episodes[i].SubscriptionKey = subscriptionKey
	}

	candidateKeys := make(map[string]struct{}, len(pkg.EpisodeCandidates))
	for i, record := range pkg.EpisodeCandidates {
		subscriptionKey := normalizedKey(record.SubscriptionKey)
		episodeKey := episodeRecordKey(subscriptionKey, record.Episode)
		if _, exists := episodeKeys[episodeKey]; !exists {
			return fmt.Errorf("episode_candidates[%d]: missing episode %q", i, episodeKey)
		}
		if !validCandidateStatus(record.Status) {
			return fmt.Errorf("episode_candidates[%d]: invalid status %q", i, record.Status)
		}
		resourceKey := candidateResourceKey(record)
		if resourceKey == "" {
			return fmt.Errorf("episode_candidates[%d]: missing resource identity", i)
		}
		key := candidateStateKey(episodeKey, resourceKey)
		if _, exists := candidateKeys[key]; exists {
			return fmt.Errorf("episode_candidates[%d]: candidate key collision %q", i, key)
		}
		candidateKeys[key] = struct{}{}
		pkg.EpisodeCandidates[i].SubscriptionKey = subscriptionKey
	}

	return nil
}

func validEpisodeStatus(status string) bool {
	switch status {
	case model.EpisodeStatusMissing,
		model.EpisodeStatusDownloading,
		model.EpisodeStatusDownloaded,
		model.EpisodeStatusMarkedDownloaded,
		model.EpisodeStatusIgnored:
		return true
	default:
		return false
	}
}

func validEpisodeStatusSource(source string) bool {
	switch source {
	case model.EpisodeStatusSourceAutomatic,
		model.EpisodeStatusSourceUser,
		model.EpisodeStatusSourceMigration:
		return true
	default:
		return false
	}
}

func validCandidateStatus(status string) bool {
	switch status {
	case model.CandidateStatusPending,
		model.CandidateStatusKeptExisting,
		model.CandidateStatusReplacing,
		model.CandidateStatusAccepted,
		model.CandidateStatusAcceptedCleanupFailed,
		model.CandidateStatusFailed:
		return true
	default:
		return false
	}
}

func normalizeEpisodeRecord(record *EpisodeRecord) {
	if record.Status != model.EpisodeStatusDownloading {
		return
	}
	record.Status = model.EpisodeStatusMissing
	record.ActiveTorrentHash = ""
	record.ActiveTorrentURL = ""
	record.ActiveTitle = ""
	record.DownloadedAt = nil
}

func normalizeCandidateRecord(record *CandidateRecord) {
	if record.Status != model.CandidateStatusReplacing &&
		record.Status != model.CandidateStatusAcceptedCleanupFailed &&
		record.runtimeStage == "" {
		return
	}
	record.Status = model.CandidateStatusFailed
	const reason = "restored_without_runtime_task"
	if !strings.Contains(record.FailureReason, reason) {
		if strings.TrimSpace(record.FailureReason) == "" {
			record.FailureReason = reason
		} else {
			record.FailureReason = strings.TrimSpace(record.FailureReason) + "; " + reason
		}
	}
	record.runtimeStage = ""
}

func firstNonEmpty(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func stringSliceFromValue(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return v
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		if strings.HasPrefix(v, "[") {
			var parsed []string
			if err := json.Unmarshal([]byte(v), &parsed); err == nil {
				return parsed
			}
		}
		return []string{v}
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return nil
		}
		return []string{text}
	}
}

func intFromValue(value any, fallback int) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

func boolFromValue(value any, fallback bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "enabled":
			return true
		case "false", "0", "no", "disabled":
			return false
		}
	}
	return fallback
}
