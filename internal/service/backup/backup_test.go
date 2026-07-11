package backup

import (
	"encoding/json"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newBackupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Config{},
		&model.RSSSource{},
		&model.Subscription{},
		&model.SubscriptionGroup{},
		&model.SubscriptionTag{},
		&model.SubscriptionTagRelation{},
		&model.NotificationSetting{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func TestExportRedactsSensitiveConfigAndNotificationSettings(t *testing.T) {
	db := newBackupTestDB(t)
	if err := db.Create(&model.Config{Key: "qbittorrent_password", Value: "secret"}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.Create(&model.Config{Key: "download_path", Value: "/downloads"}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.Create(&model.NotificationSetting{Channel: "telegram", Enabled: true, Config: `{"token":"abc"}`}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	pkg, err := NewService(db).Export(false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	configs := map[string]ConfigRecord{}
	for _, cfg := range pkg.Configs {
		configs[cfg.Key] = cfg
	}
	if got := configs["qbittorrent_password"].Value; got != RedactedValue {
		t.Fatalf("expected password redacted, got %q", got)
	}
	if !configs["qbittorrent_password"].Sensitive || !configs["qbittorrent_password"].Redacted {
		t.Fatalf("expected password config to be marked sensitive/redacted: %#v", configs["qbittorrent_password"])
	}
	if got := configs["download_path"].Value; got != "/downloads" {
		t.Fatalf("expected non-sensitive value preserved, got %q", got)
	}
	if len(pkg.NotificationSettings) != 1 {
		t.Fatalf("expected one notification setting, got %d", len(pkg.NotificationSettings))
	}
	if got := pkg.NotificationSettings[0].Config; got != RedactedValue {
		t.Fatalf("expected notification config redacted, got %q", got)
	}
}

func TestPreviewSkipsRedactedSensitiveValuesAndDetectsConflicts(t *testing.T) {
	db := newBackupTestDB(t)
	if err := db.Create(&model.Config{Key: "download_path", Value: "/old"}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}

	pkg := Package{
		App:           "auto-rss",
		SchemaVersion: SchemaVersion,
		Configs: []ConfigRecord{
			{Key: "download_path", Value: "/new"},
			{Key: "qbittorrent_password", Value: RedactedValue, Sensitive: true, Redacted: true},
		},
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	plan, err := NewService(db).Preview(data, SourceAutoRSS, StrategyOverwrite)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	var sawOverwrite bool
	var sawSensitiveSkip bool
	for _, item := range plan.Items {
		if item.Resource == "config" && item.Key == "download_path" && item.Action == "overwrite" && item.Conflict {
			sawOverwrite = true
		}
		if item.Resource == "config" && item.Key == "qbittorrent_password" && item.Action == "skip" && item.Sensitive {
			sawSensitiveSkip = true
		}
	}
	if !sawOverwrite {
		t.Fatalf("expected download_path overwrite conflict in plan: %#v", plan.Items)
	}
	if !sawSensitiveSkip {
		t.Fatalf("expected redacted password skip in plan: %#v", plan.Items)
	}
	if plan.Summary.SensitiveSkipped != 1 {
		t.Fatalf("expected one sensitive skip, got %d", plan.Summary.SensitiveSkipped)
	}
}

func TestPreviewConflictActionsFollowImportStrategy(t *testing.T) {
	db := newBackupTestDB(t)
	if err := db.Create(&model.Config{Key: "download_path", Value: "/old"}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.Create(&model.Subscription{
		Name:    "既存番剧",
		RssURL:  "https://example.com/existing.xml",
		Season:  1,
		Status:  "active",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	pkg := Package{
		App:           "auto-rss",
		SchemaVersion: SchemaVersion,
		Configs: []ConfigRecord{
			{Key: "download_path", Value: "/new"},
			{Key: "qbittorrent_password", Value: RedactedValue, Sensitive: true, Redacted: true},
		},
		Subscriptions: []SubscriptionRecord{
			{
				Subscription: model.Subscription{
					Name:    "既存番剧",
					RssURL:  "https://example.com/existing.xml",
					Season:  1,
					Status:  "paused",
					Enabled: true,
				},
			},
		},
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	tests := []struct {
		name     string
		strategy string
		action   string
	}{
		{
			name:     "default skip",
			strategy: "",
			action:   "skip",
		},
		{
			name:     "overwrite",
			strategy: StrategyOverwrite,
			action:   "overwrite",
		},
		{
			name:     "merge",
			strategy: StrategyMerge,
			action:   "merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := NewService(db).Preview(data, SourceAutoRSS, tt.strategy)
			if err != nil {
				t.Fatalf("preview: %v", err)
			}

			assertPlanItem(t, plan, "config", "download_path", tt.action, true, false)
			assertPlanItem(t, plan, "subscription", "https://example.com/existing.xml|season:1", tt.action, true, false)
			assertPlanItem(t, plan, "config", "qbittorrent_password", "skip", false, true)
			if got := countPlanItems(plan, tt.action, true); got != 2 {
				t.Fatalf("expected two %s conflict actions, got %d in %#v", tt.action, got, plan.Items)
			}
			if plan.Summary.SensitiveSkipped != 1 {
				t.Fatalf("expected one sensitive skip, got summary %#v", plan.Summary)
			}
		})
	}
}

func TestImportOverwriteCreatesRelationsAndDoesNotImportRedactedSecrets(t *testing.T) {
	db := newBackupTestDB(t)
	if err := db.Create(&model.Config{Key: "download_path", Value: "/old"}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}

	pkg := Package{
		App:           "auto-rss",
		SchemaVersion: SchemaVersion,
		Configs: []ConfigRecord{
			{Key: "download_path", Value: "/new"},
			{Key: "qbittorrent_password", Value: RedactedValue, Sensitive: true, Redacted: true},
		},
		Groups: []model.SubscriptionGroup{
			{Name: "新番", Color: "#18a058"},
		},
		Tags: []model.SubscriptionTag{
			{Name: "追更", Color: "#2080f0"},
		},
		Subscriptions: []SubscriptionRecord{
			{
				Subscription: model.Subscription{
					Name:    "测试番剧",
					RssURL:  "https://example.com/rss.xml",
					Season:  1,
					Enabled: true,
					Status:  "active",
				},
				GroupName: "新番",
			},
		},
		SubscriptionTags: []SubscriptionTagRecord{
			{
				SubscriptionKey: "https://example.com/rss.xml|season:1",
				Subscription:    "测试番剧",
				TagName:         "追更",
			},
		},
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	plan, err := NewService(db).Import(data, SourceAutoRSS, StrategyOverwrite)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if plan.Summary.Create < 3 {
		t.Fatalf("expected created resources in plan, got %#v", plan.Summary)
	}

	var cfg model.Config
	if err := db.Where("key = ?", "download_path").First(&cfg).Error; err != nil {
		t.Fatalf("load download_path: %v", err)
	}
	if cfg.Value != "/new" {
		t.Fatalf("expected download_path overwritten, got %q", cfg.Value)
	}
	var count int64
	if err := db.Model(&model.Config{}).Where("key = ?", "qbittorrent_password").Count(&count).Error; err != nil {
		t.Fatalf("count password config: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected redacted password not imported, count=%d", count)
	}
	if err := db.Model(&model.SubscriptionTagRelation{}).Count(&count).Error; err != nil {
		t.Fatalf("count tag relation: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one tag relation, count=%d", count)
	}
}

func TestImportMergeFillsMissingFieldsWithoutOverwritingExistingValues(t *testing.T) {
	db := newBackupTestDB(t)
	if err := db.Create(&model.Config{Key: "download_path", Value: "/old"}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.Create(&model.NotificationSetting{Channel: "telegram", Enabled: true, Config: `{"token":"old"}`}).Error; err != nil {
		t.Fatalf("seed notification setting: %v", err)
	}
	if err := db.Create(&model.Subscription{
		Name:           "既存番剧",
		RssURL:         "https://example.com/existing.xml",
		Season:         1,
		Status:         "active",
		Enabled:        true,
		Fansub:         "本地字幕组",
		FilterKeywords: "",
	}).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	pkg := Package{
		App:           "auto-rss",
		SchemaVersion: SchemaVersion,
		Configs: []ConfigRecord{
			{Key: "download_path", Value: "/new"},
			{Key: "api_token", Value: RedactedValue, Sensitive: true, Redacted: true},
		},
		NotificationSettings: []NotificationSettingRecord{
			{Channel: "telegram", Enabled: false, Config: `{"token":"new"}`, Sensitive: true},
			{Channel: "email", Enabled: true, Config: RedactedValue, Sensitive: true, Redacted: true},
		},
		Subscriptions: []SubscriptionRecord{
			{
				Subscription: model.Subscription{
					Name:           "既存番剧",
					RssURL:         "https://example.com/existing.xml",
					Season:         1,
					Status:         "paused",
					Enabled:        true,
					Fansub:         "导入字幕组",
					FilterKeywords: `["1080p"]`,
				},
			},
		},
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	plan, err := NewService(db).Import(data, SourceAutoRSS, StrategyMerge)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	assertPlanItem(t, plan, "config", "download_path", "merge", true, false)
	assertPlanItem(t, plan, "notification_setting", "telegram", "merge", true, true)
	assertPlanItem(t, plan, "subscription", "https://example.com/existing.xml|season:1", "merge", true, false)
	assertPlanItem(t, plan, "config", "api_token", "skip", false, true)
	assertPlanItem(t, plan, "notification_setting", "email", "skip", false, true)
	if plan.Summary.Merge != 3 || plan.Summary.SensitiveSkipped != 2 {
		t.Fatalf("unexpected import summary: %#v", plan.Summary)
	}

	var cfg model.Config
	if err := db.Where("key = ?", "download_path").First(&cfg).Error; err != nil {
		t.Fatalf("load download_path: %v", err)
	}
	if cfg.Value != "/old" {
		t.Fatalf("expected merge not to overwrite config, got %q", cfg.Value)
	}

	var setting model.NotificationSetting
	if err := db.Where("channel = ?", "telegram").First(&setting).Error; err != nil {
		t.Fatalf("load telegram setting: %v", err)
	}
	if setting.Config != `{"token":"old"}` || !setting.Enabled {
		t.Fatalf("expected merge not to overwrite notification setting, got %#v", setting)
	}
	var count int64
	if err := db.Model(&model.NotificationSetting{}).Where("channel = ?", "email").Count(&count).Error; err != nil {
		t.Fatalf("count email setting: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected redacted email setting not imported, count=%d", count)
	}

	var sub model.Subscription
	if err := db.Where("rss_url = ?", "https://example.com/existing.xml").First(&sub).Error; err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	if sub.Fansub != "本地字幕组" {
		t.Fatalf("expected merge not to overwrite existing fansub, got %q", sub.Fansub)
	}
	if sub.FilterKeywords != `["1080p"]` {
		t.Fatalf("expected merge to fill missing filter keywords, got %q", sub.FilterKeywords)
	}
}

func TestParseAutoBangumiPackageMapsRSSLinks(t *testing.T) {
	data := json.RawMessage(`{
		"bangumi": [
			{
				"official_title": "葬送的芙莉莲",
				"rss_link": ["https://mikanani.me/RSS/Bangumi?bangumiId=3026"],
				"group_name": "喵萌",
				"season": 1,
				"filter": ["1080p", "简日"]
			}
		]
	}`)

	pkg, format, err := ParsePackage(data, SourceAutoBangumi)
	if err != nil {
		t.Fatalf("parse auto-bangumi: %v", err)
	}
	if format != SourceAutoBangumi {
		t.Fatalf("expected auto-bangumi format, got %s", format)
	}
	if len(pkg.Subscriptions) != 1 {
		t.Fatalf("expected one subscription, got %d", len(pkg.Subscriptions))
	}
	sub := pkg.Subscriptions[0]
	if sub.Name != "葬送的芙莉莲" {
		t.Fatalf("unexpected name %q", sub.Name)
	}
	if sub.RssURL != "https://mikanani.me/RSS/Bangumi?bangumiId=3026" {
		t.Fatalf("unexpected RSS URL %q", sub.RssURL)
	}
	if sub.Fansub != "喵萌" {
		t.Fatalf("unexpected fansub %q", sub.Fansub)
	}
	if sub.FilterRules == "" || sub.FilterKeywords == "" {
		t.Fatalf("expected filter fields to be mapped: %#v", sub)
	}
}

func assertPlanItem(t *testing.T, plan *ImportPlan, resource, key, action string, conflict, sensitive bool) {
	t.Helper()
	for _, item := range plan.Items {
		if item.Resource == resource && item.Key == key && item.Action == action {
			if item.Conflict != conflict || item.Sensitive != sensitive {
				t.Fatalf("expected %s %s item conflict=%t sensitive=%t, got %#v", resource, key, conflict, sensitive, item)
			}
			return
		}
	}
	t.Fatalf("expected plan item resource=%s key=%s action=%s in %#v", resource, key, action, plan.Items)
}

func countPlanItems(plan *ImportPlan, action string, conflict bool) int {
	count := 0
	for _, item := range plan.Items {
		if item.Action == action && item.Conflict == conflict {
			count++
		}
	}
	return count
}
