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
