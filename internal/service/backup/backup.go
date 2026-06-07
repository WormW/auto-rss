package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

const (
	SchemaVersion = "1.0"
	RedactedValue = "__AUTO_RSS_REDACTED__"

	StrategySkip      = "skip"
	StrategyOverwrite = "overwrite"
	StrategyMerge     = "merge"

	SourceAuto        = "auto"
	SourceAutoRSS     = "auto-rss"
	SourceAutoBangumi = "auto-bangumi"
)

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
	SubscriptionTags     []SubscriptionTagRecord     `json:"subscription_tags"`
	NotificationSettings []NotificationSettingRecord `json:"notification_settings"`
}

type PackageSummary struct {
	Configs              int `json:"configs"`
	RSSSources           int `json:"rss_sources"`
	Groups               int `json:"groups"`
	Tags                 int `json:"tags"`
	Subscriptions        int `json:"subscriptions"`
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
	GroupName     string `json:"group_name,omitempty"`
	RSSSourceName string `json:"rss_source_name,omitempty"`
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
	subscriptionByID := make(map[uint]model.Subscription, len(subscriptions))
	for _, sub := range subscriptions {
		subscriptionByID[sub.ID] = sub
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
		record := SubscriptionRecord{Subscription: sub}
		if sub.GroupID != nil {
			record.GroupName = groupNames[*sub.GroupID]
		}
		if sub.RSSSourceID != nil {
			record.RSSSourceName = sourceNames[*sub.RSSSourceID]
		}
		record.Group = nil
		record.RSSSource = nil
		record.Downloads = nil
		pkg.Subscriptions = append(pkg.Subscriptions, record)
	}

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
		SubscriptionTags:     len(pkg.SubscriptionTags),
		NotificationSettings: len(pkg.NotificationSettings),
	}

	return pkg, nil
}

func (s *Service) Preview(data json.RawMessage, sourceFormat, strategy string) (*ImportPlan, error) {
	pkg, detectedFormat, err := ParsePackage(data, sourceFormat)
	if err != nil {
		return nil, err
	}
	state, err := s.loadCurrentState(s.db)
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
		state, err := s.loadCurrentState(tx)
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
	sourceFormat = strings.TrimSpace(strings.ToLower(sourceFormat))
	if sourceFormat == "" {
		sourceFormat = SourceAuto
	}

	if sourceFormat == SourceAutoRSS || sourceFormat == SourceAuto {
		pkg, err := parseAutoRSSPackage(data)
		if err == nil {
			return pkg, SourceAutoRSS, nil
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
	if len(pkg.Subscriptions) == 0 && len(pkg.Configs) == 0 && len(pkg.RSSSources) == 0 &&
		len(pkg.Groups) == 0 && len(pkg.Tags) == 0 && len(pkg.NotificationSettings) == 0 {
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
		pkg.SchemaVersion = SchemaVersion
	}
	if pkg.App == "" {
		pkg.App = "auto-rss"
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
			state.subscriptions[key] = sub
			continue
		}
		if strategy == StrategyOverwrite {
			sub.ID = existing.ID
			sub.CreatedAt = existing.CreatedAt
			if err := tx.Save(&sub).Error; err != nil {
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

func (s *Service) loadCurrentState(db *gorm.DB) (*currentState, error) {
	state := &currentState{
		configs:       map[string]model.Config{},
		rssSources:    map[string]model.RSSSource{},
		groups:        map[string]model.SubscriptionGroup{},
		tags:          map[string]model.SubscriptionTag{},
		subscriptions: map[string]model.Subscription{},
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
	for _, sub := range subscriptions {
		state.subscriptions[subscriptionKeyFromModel(sub)] = sub
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
