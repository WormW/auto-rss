package backup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func TestBackupRoundTripPreservesEpisodeLedgerWithoutRuntimeLinks(t *testing.T) {
	source := newBackupTestDB(t)
	sub := seedBackupSubscription(t, source, "https://backup.test/rss", 1)
	downloadID := uint(99)
	downloadedAt := time.Date(2026, time.July, 12, 8, 30, 0, 0, time.UTC)
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           3,
		Status:            model.EpisodeStatusMarkedDownloaded,
		ActiveDownloadID:  &downloadID,
		ActiveTorrentHash: "hash-3",
		ActiveTorrentURL:  "https://backup.test/e03",
		ActiveTitle:       "E03",
		StatusSource:      model.EpisodeStatusSourceUser,
		DownloadedAt:      &downloadedAt,
	}
	if err := source.Create(&ledger).Error; err != nil {
		t.Fatalf("seed episode ledger: %v", err)
	}
	replacementDownloadID := uint(100)
	oldDownloadID := uint(98)
	candidate := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: ledger.ID,
		ResourceKey:           "hash:candidate-3",
		TorrentHash:           "candidate-3",
		TorrentURL:            "https://backup.test/candidate/e03",
		Title:                 "E03 candidate",
		Fansub:                "TestSub",
		Language:              "zh",
		Status:                model.CandidateStatusPending,
		FailureReason:         "retry later",
		ReplacementStage:      "downloading",
		ReplacementDownloadID: &replacementDownloadID,
		OldDownloadID:         &oldDownloadID,
		StagedPath:            "/runtime/staged",
		OldResourcePath:       "/runtime/old",
		RollbackPath:          "/runtime/rollback",
		FinalPath:             "/runtime/final",
	}
	if err := source.Create(&candidate).Error; err != nil {
		t.Fatalf("seed episode candidate: %v", err)
	}

	pkg, err := NewService(source).Export(false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if pkg.SchemaVersion != "1.1" {
		t.Fatalf("schema version = %q, want 1.1", pkg.SchemaVersion)
	}
	if len(pkg.Episodes) != 1 || len(pkg.EpisodeCandidates) != 1 {
		t.Fatalf("unexpected episode summary: episodes=%d candidates=%d", len(pkg.Episodes), len(pkg.EpisodeCandidates))
	}
	if pkg.Summary.Episodes != 1 || pkg.Summary.EpisodeCandidates != 1 {
		t.Fatalf("unexpected package summary: %#v", pkg.Summary)
	}

	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}
	serialized := string(data)
	for _, runtimeValue := range []string{
		"/runtime/staged", "/runtime/old", "/runtime/rollback", "/runtime/final",
		`"active_download_id"`, `"replacement_download_id"`, `"old_download_id"`,
		`"replacement_stage"`, `"subscription_episode_id"`, `"resource_key"`,
	} {
		if strings.Contains(serialized, runtimeValue) {
			t.Fatalf("backup leaked runtime value %q: %s", runtimeValue, serialized)
		}
	}

	target := newBackupTestDB(t)
	if _, err := NewService(target).Import(data, SourceAutoRSS, StrategyOverwrite); err != nil {
		t.Fatalf("import: %v", err)
	}

	var restored model.SubscriptionEpisode
	if err := target.First(&restored).Error; err != nil {
		t.Fatalf("load restored episode: %v", err)
	}
	if restored.Episode != 3 || restored.Status != model.EpisodeStatusMarkedDownloaded {
		t.Fatalf("unexpected restored episode: %#v", restored)
	}
	if restored.ActiveDownloadID != nil {
		t.Fatalf("active download runtime link restored: %#v", restored.ActiveDownloadID)
	}
	if restored.ActiveTorrentHash != "hash-3" || restored.ActiveTorrentURL != "https://backup.test/e03" || restored.ActiveTitle != "E03" {
		t.Fatalf("stable episode resource identity not preserved: %#v", restored)
	}
	if restored.DownloadedAt == nil || !restored.DownloadedAt.Equal(downloadedAt) {
		t.Fatalf("downloaded_at not preserved: %#v", restored.DownloadedAt)
	}

	var restoredCandidate model.EpisodeResourceCandidate
	if err := target.First(&restoredCandidate).Error; err != nil {
		t.Fatalf("load restored candidate: %v", err)
	}
	if restoredCandidate.ResourceKey != "hash:candidate-3" || restoredCandidate.TorrentHash != "candidate-3" {
		t.Fatalf("candidate stable identity not preserved: %#v", restoredCandidate)
	}
	assertCandidateRuntimeCleared(t, restoredCandidate)
}

func TestBackupImportNormalizesInterruptedReplacementState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		stage  string
	}{
		{name: "replacing without stage", status: model.CandidateStatusReplacing},
		{name: "accepted cleanup failed", status: model.CandidateStatusAcceptedCleanupFailed},
		{name: "downloading stage", status: model.CandidateStatusPending, stage: "downloading"},
		{name: "detaching stage", status: model.CandidateStatusPending, stage: "detaching"},
		{name: "terminal cleanup stage", status: model.CandidateStatusAccepted, stage: "terminal_cleanup"},
		{name: "cleanup queued stage", status: model.CandidateStatusAccepted, stage: "cleanup_queued"},
		{name: "cleanup active stage", status: model.CandidateStatusAccepted, stage: "cleanup_active"},
		{name: "unknown non-empty stage", status: model.CandidateStatusAccepted, stage: "future_runtime_stage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := backupPackageWithCandidate(tc.status)
			data, err := json.Marshal(pkg)
			if err != nil {
				t.Fatalf("marshal backup: %v", err)
			}

			var object map[string]any
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatalf("decode test package: %v", err)
			}
			candidates := object["episode_candidates"].([]any)
			candidate := candidates[0].(map[string]any)
			candidate["replacement_stage"] = tc.stage
			candidate["replacement_download_id"] = float64(23)
			candidate["old_download_id"] = float64(22)
			candidate["staged_path"] = "/old/staged"
			candidate["old_resource_path"] = "/old/resource"
			candidate["rollback_path"] = "/old/rollback"
			candidate["final_path"] = "/old/final"
			data, err = json.Marshal(object)
			if err != nil {
				t.Fatalf("marshal malicious backup: %v", err)
			}

			db := newBackupTestDB(t)
			if _, err := NewService(db).Import(data, SourceAutoRSS, StrategyOverwrite); err != nil {
				t.Fatalf("import: %v", err)
			}

			var restored model.EpisodeResourceCandidate
			if err := db.First(&restored).Error; err != nil {
				t.Fatalf("load restored candidate: %v", err)
			}
			if restored.Status != model.CandidateStatusFailed {
				t.Fatalf("candidate status = %q, want failed", restored.Status)
			}
			if !strings.Contains(restored.FailureReason, "restored_without_runtime_task") {
				t.Fatalf("failure reason missing restore marker: %q", restored.FailureReason)
			}
			assertCandidateRuntimeCleared(t, restored)
		})
	}
}

func TestParsePackageAcceptsVersion10AndRejectsFutureVersions(t *testing.T) {
	legacy := Package{
		App:           "auto-rss",
		SchemaVersion: "1.0",
		Subscriptions: []SubscriptionRecord{{Subscription: model.Subscription{
			Name: "Legacy Show", RssURL: "https://backup.test/legacy", Season: 1,
		}}},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy package: %v", err)
	}
	pkg, format, err := ParsePackage(data, SourceAutoRSS)
	if err != nil {
		t.Fatalf("parse 1.0 package: %v", err)
	}
	if format != SourceAutoRSS || pkg.SchemaVersion != "1.0" {
		t.Fatalf("unexpected legacy parse result: format=%q package=%#v", format, pkg)
	}
	if len(pkg.Episodes) != 0 || len(pkg.EpisodeCandidates) != 0 {
		t.Fatalf("legacy package should default episode state to empty: %#v", pkg)
	}

	legacy.SchemaVersion = "2.0"
	data, err = json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal future package: %v", err)
	}
	if _, _, err := ParsePackage(data, SourceAutoRSS); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("future package error = %v, want unsupported version", err)
	}
}

func TestBackupLegacyVersion10AllowsSubscriptionKeyCollisionsWithoutOwnership(t *testing.T) {
	pkg := Package{
		App:           "auto-rss",
		SchemaVersion: "1.0",
		Subscriptions: []SubscriptionRecord{
			{Subscription: model.Subscription{Name: "Legacy Show A", RssURL: "https://backup.test/legacy", Season: 1}},
			{Subscription: model.Subscription{Name: "Legacy Show B", RssURL: "https://backup.test/legacy", Season: 1}},
		},
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal legacy package: %v", err)
	}

	if _, _, err := ParsePackage(data, SourceAutoRSS); err != nil {
		t.Fatalf("parse legacy package with duplicate stable keys: %v", err)
	}

	db := newBackupTestDB(t)
	if _, err := NewService(db).Import(data, SourceAutoRSS, StrategySkip); err != nil {
		t.Fatalf("import legacy package with duplicate stable keys: %v", err)
	}
	var count int64
	if err := db.Model(&model.Subscription{}).Count(&count).Error; err != nil {
		t.Fatalf("count imported subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy duplicate import count = %d, want old map semantics count 1", count)
	}
}

func TestBackupLegacyVersion10WithoutOwnershipIgnoresTargetOwnershipKeyCollisions(t *testing.T) {
	db := newBackupTestDB(t)
	first := seedBackupSubscription(t, db, "https://backup.test/duplicate", 1)
	seedBackupSubscription(t, db, "https://backup.test/duplicate", 1)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: first.ID,
		Episode:        1,
		Status:         model.EpisodeStatusDownloaded,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}
	if err := db.Create(&ledger).Error; err != nil {
		t.Fatalf("seed target episode: %v", err)
	}
	if err := db.Create(&model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: ledger.ID,
		Title:                 "legacy candidate without stable identity",
		Status:                model.CandidateStatusPending,
	}).Error; err != nil {
		t.Fatalf("seed target candidate: %v", err)
	}
	if err := db.Create(&model.NotificationSetting{
		Channel: "telegram",
		Enabled: true,
		Config:  `{"token":"old"}`,
	}).Error; err != nil {
		t.Fatalf("seed target notification setting: %v", err)
	}

	pkg := Package{
		App:           "auto-rss",
		SchemaVersion: "1.0",
		Configs:       []ConfigRecord{{Key: "download_path", Value: "/legacy/import"}},
		NotificationSettings: []NotificationSettingRecord{{
			Channel: "telegram", Enabled: false, Config: `{"token":"new"}`, Sensitive: true,
		}},
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal legacy config package: %v", err)
	}
	service := NewService(db)
	plan, err := service.Preview(data, SourceAutoRSS, StrategyOverwrite)
	if err != nil {
		t.Fatalf("preview legacy package against duplicate target keys: %v", err)
	}
	assertPlanItem(t, plan, "notification_setting", "telegram", "overwrite", true, true)
	if _, err := service.Import(data, SourceAutoRSS, StrategyOverwrite); err != nil {
		t.Fatalf("import legacy package against duplicate target keys: %v", err)
	}
	var cfg model.Config
	if err := db.Where("key = ?", "download_path").First(&cfg).Error; err != nil {
		t.Fatalf("load imported config: %v", err)
	}
	if cfg.Value != "/legacy/import" {
		t.Fatalf("imported config value = %q", cfg.Value)
	}
	var setting model.NotificationSetting
	if err := db.Where("channel = ?", "telegram").First(&setting).Error; err != nil {
		t.Fatalf("load imported notification setting: %v", err)
	}
	if setting.Enabled || setting.Config != `{"token":"new"}` {
		t.Fatalf("notification setting was not overwritten: %#v", setting)
	}
}

func TestBackupExportAllowsSubscriptionKeyCollisionWithoutOwnership(t *testing.T) {
	db := newBackupTestDB(t)
	seedBackupSubscription(t, db, "https://backup.test/duplicate", 1)
	seedBackupSubscription(t, db, "https://backup.test/duplicate", 1)

	pkg, err := NewService(db).Export(false)
	if err != nil {
		t.Fatalf("export subscriptions without ownership: %v", err)
	}
	if len(pkg.Subscriptions) != 2 || len(pkg.Episodes) != 0 || len(pkg.EpisodeCandidates) != 0 {
		t.Fatalf("unexpected export contents: subscriptions=%d episodes=%d candidates=%d", len(pkg.Subscriptions), len(pkg.Episodes), len(pkg.EpisodeCandidates))
	}
}

func TestBackupImportWithOwnershipRejectsTargetSubscriptionKeyCollision(t *testing.T) {
	db := newBackupTestDB(t)
	seedBackupSubscription(t, db, "https://backup.test/rss", 1)
	seedBackupSubscription(t, db, "https://backup.test/rss", 1)
	pkg := backupPackageWithCandidate(model.CandidateStatusPending)
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal ownership package: %v", err)
	}

	if _, err := NewService(db).Import(data, SourceAutoRSS, StrategyOverwrite); err == nil || !strings.Contains(err.Error(), "subscription backup key collision") {
		t.Fatalf("ownership import error = %v, want target subscription key collision", err)
	}
}

func TestBackupExportWithOwnershipRejectsSubscriptionKeyCollision(t *testing.T) {
	db := newBackupTestDB(t)
	first := seedBackupSubscription(t, db, "https://backup.test/duplicate", 1)
	seedBackupSubscription(t, db, "https://backup.test/duplicate", 1)
	if err := db.Create(&model.SubscriptionEpisode{
		SubscriptionID: first.ID,
		Episode:        1,
		Status:         model.EpisodeStatusDownloaded,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}).Error; err != nil {
		t.Fatalf("seed ownership episode: %v", err)
	}

	if _, err := NewService(db).Export(false); err == nil || !strings.Contains(err.Error(), "subscription backup key collision") {
		t.Fatalf("ownership export error = %v, want subscription key collision", err)
	}
}

func TestBackupImportEpisodeStrategiesAndIdempotency(t *testing.T) {
	for _, strategy := range []string{StrategySkip, StrategyMerge, StrategyOverwrite} {
		t.Run(strategy, func(t *testing.T) {
			db := newBackupTestDB(t)
			sub := seedBackupSubscription(t, db, "https://backup.test/rss", 1)
			activeDownloadID := uint(77)
			existingLedger := model.SubscriptionEpisode{
				SubscriptionID:    sub.ID,
				Episode:           3,
				Status:            model.EpisodeStatusIgnored,
				ActiveDownloadID:  &activeDownloadID,
				ActiveTorrentHash: "old-hash",
				ActiveTorrentURL:  "https://old.test/e03",
				ActiveTitle:       "old title",
				StatusSource:      model.EpisodeStatusSourceUser,
			}
			if err := db.Create(&existingLedger).Error; err != nil {
				t.Fatalf("seed existing ledger: %v", err)
			}
			replacementDownloadID := uint(88)
			existingCandidate := model.EpisodeResourceCandidate{
				SubscriptionEpisodeID: existingLedger.ID,
				ResourceKey:           "legacy-key",
				TorrentHash:           "candidate-3",
				TorrentURL:            "https://old.test/candidate/e03",
				Title:                 "old candidate",
				Status:                model.CandidateStatusAccepted,
				ReplacementStage:      "done",
				ReplacementDownloadID: &replacementDownloadID,
				FinalPath:             "/old/final",
			}
			if err := db.Create(&existingCandidate).Error; err != nil {
				t.Fatalf("seed existing candidate: %v", err)
			}

			pkg := backupPackageWithCandidate(model.CandidateStatusPending)
			pkg.Episodes[0].Status = model.EpisodeStatusDownloaded
			pkg.Episodes[0].ActiveTorrentHash = "new-hash"
			pkg.Episodes[0].ActiveTorrentURL = "https://new.test/e03"
			pkg.Episodes[0].ActiveTitle = "new title"
			pkg.EpisodeCandidates[0].TorrentURL = "https://new.test/candidate/e03"
			pkg.EpisodeCandidates[0].Title = "new candidate"
			pkg.Episodes = append(pkg.Episodes, EpisodeRecord{
				SubscriptionKey: "https://backup.test/rss|season:1",
				Episode:         4,
				Status:          model.EpisodeStatusMissing,
				StatusSource:    model.EpisodeStatusSourceAutomatic,
			})
			pkg.EpisodeCandidates = append(pkg.EpisodeCandidates, CandidateRecord{
				SubscriptionKey: "https://backup.test/rss|season:1",
				Episode:         4,
				TorrentURL:      "https://backup.test/candidate/e04",
				Title:           "E04 candidate",
				Status:          model.CandidateStatusPending,
			})
			data, err := json.Marshal(pkg)
			if err != nil {
				t.Fatalf("marshal package: %v", err)
			}
			service := NewService(db)
			if _, err := service.Import(data, SourceAutoRSS, strategy); err != nil {
				t.Fatalf("first import: %v", err)
			}
			if _, err := service.Import(data, SourceAutoRSS, strategy); err != nil {
				t.Fatalf("idempotent second import: %v", err)
			}

			var ledger model.SubscriptionEpisode
			if err := db.Where("subscription_id = ? AND episode = ?", sub.ID, 3).First(&ledger).Error; err != nil {
				t.Fatalf("load episode 3: %v", err)
			}
			var candidate model.EpisodeResourceCandidate
			if err := db.Where("subscription_episode_id = ?", ledger.ID).First(&candidate).Error; err != nil {
				t.Fatalf("load episode 3 candidate: %v", err)
			}
			if strategy == StrategyOverwrite {
				if ledger.Status != model.EpisodeStatusDownloaded || ledger.ActiveTorrentHash != "new-hash" || ledger.ActiveDownloadID != nil {
					t.Fatalf("overwrite did not replace stable ledger state and clear runtime link: %#v", ledger)
				}
				if candidate.Status != model.CandidateStatusPending || candidate.Title != "new candidate" || candidate.ResourceKey != "hash:candidate-3" {
					t.Fatalf("overwrite did not replace candidate stable state: %#v", candidate)
				}
				assertCandidateRuntimeCleared(t, candidate)
			} else {
				if ledger.Status != model.EpisodeStatusIgnored || ledger.ActiveTorrentHash != "old-hash" || ledger.ActiveDownloadID == nil {
					t.Fatalf("%s changed existing ledger: %#v", strategy, ledger)
				}
				if candidate.Status != model.CandidateStatusAccepted || candidate.Title != "old candidate" || candidate.FinalPath != "/old/final" {
					t.Fatalf("%s changed existing candidate: %#v", strategy, candidate)
				}
			}

			var episodeCount, candidateCount int64
			if err := db.Model(&model.SubscriptionEpisode{}).Count(&episodeCount).Error; err != nil {
				t.Fatalf("count episodes: %v", err)
			}
			if err := db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error; err != nil {
				t.Fatalf("count candidates: %v", err)
			}
			if episodeCount != 2 || candidateCount != 2 {
				t.Fatalf("repeated import was not idempotent: episodes=%d candidates=%d", episodeCount, candidateCount)
			}
		})
	}
}

func TestBackupImportRejectsInvalidEpisodeOwnershipInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Package)
	}{
		{
			name: "subscription key collision",
			mutate: func(pkg *Package) {
				pkg.Subscriptions = append(pkg.Subscriptions, pkg.Subscriptions[0])
			},
		},
		{
			name: "episode missing subscription",
			mutate: func(pkg *Package) {
				pkg.Episodes[0].SubscriptionKey = "https://other.test/rss|season:1"
			},
		},
		{
			name: "invalid episode",
			mutate: func(pkg *Package) {
				pkg.Episodes[0].Episode = 0
				pkg.EpisodeCandidates[0].Episode = 0
			},
		},
		{
			name: "invalid episode status",
			mutate: func(pkg *Package) {
				pkg.Episodes[0].Status = "owned"
			},
		},
		{
			name: "candidate missing episode",
			mutate: func(pkg *Package) {
				pkg.EpisodeCandidates[0].Episode = 999
			},
		},
		{
			name: "candidate missing resource identity",
			mutate: func(pkg *Package) {
				pkg.EpisodeCandidates[0].TorrentHash = ""
				pkg.EpisodeCandidates[0].TorrentURL = ""
			},
		},
		{
			name: "invalid candidate status",
			mutate: func(pkg *Package) {
				pkg.EpisodeCandidates[0].Status = "running"
			},
		},
		{
			name: "candidate resource collision",
			mutate: func(pkg *Package) {
				pkg.EpisodeCandidates = append(pkg.EpisodeCandidates, pkg.EpisodeCandidates[0])
				pkg.EpisodeCandidates[1].TorrentHash = strings.ToUpper(pkg.EpisodeCandidates[0].TorrentHash)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg := backupPackageWithCandidate(model.CandidateStatusPending)
			tc.mutate(&pkg)
			data, err := json.Marshal(pkg)
			if err != nil {
				t.Fatalf("marshal package: %v", err)
			}
			db := newBackupTestDB(t)
			if _, err := NewService(db).Import(data, SourceAutoRSS, StrategyOverwrite); err == nil {
				t.Fatal("invalid package import unexpectedly succeeded")
			}
			assertEpisodePackageTablesEmpty(t, db)
		})
	}
}

func TestBackupImportRollsBackWholePackageWhenCandidateWriteFails(t *testing.T) {
	db := newBackupTestDB(t)
	if err := db.Exec(`CREATE TRIGGER fail_candidate_insert
		BEFORE INSERT ON episode_resource_candidates
		BEGIN
			SELECT RAISE(ABORT, 'candidate insert failed');
		END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	pkg := backupPackageWithCandidate(model.CandidateStatusPending)
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	if _, err := NewService(db).Import(data, SourceAutoRSS, StrategyOverwrite); err == nil {
		t.Fatal("import unexpectedly succeeded")
	}
	assertEpisodePackageTablesEmpty(t, db)
}

func TestBackupExportRejectsOwnershipStateWithoutStableResourceIdentity(t *testing.T) {
	db := newBackupTestDB(t)
	sub := seedBackupSubscription(t, db, "https://backup.test/rss", 1)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID,
		Episode:        3,
		Status:         model.EpisodeStatusDownloaded,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}
	if err := db.Create(&ledger).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if err := db.Create(&model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: ledger.ID,
		Title:                 "candidate without hash or URL",
		Status:                model.CandidateStatusPending,
	}).Error; err != nil {
		t.Fatalf("seed invalid candidate: %v", err)
	}

	if _, err := NewService(db).Export(false); err == nil || !strings.Contains(err.Error(), "resource identity") {
		t.Fatalf("export error = %v, want missing resource identity", err)
	}
}

func seedBackupSubscription(t *testing.T, db *gorm.DB, rssURL string, season int) model.Subscription {
	t.Helper()
	sub := model.Subscription{Name: "Backup Show", RssURL: rssURL, Season: season, Enabled: true, Status: "active"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	return sub
}

func backupPackageWithCandidate(status string) Package {
	const subscriptionKey = "https://backup.test/rss|season:1"
	return Package{
		App:           "auto-rss",
		SchemaVersion: "1.1",
		Subscriptions: []SubscriptionRecord{{Subscription: model.Subscription{
			Name: "Backup Show", RssURL: "https://backup.test/rss", Season: 1, Enabled: true, Status: "active",
		}}},
		Episodes: []EpisodeRecord{{
			SubscriptionKey: subscriptionKey,
			Episode:         3,
			Status:          model.EpisodeStatusDownloaded,
			StatusSource:    model.EpisodeStatusSourceAutomatic,
		}},
		EpisodeCandidates: []CandidateRecord{{
			SubscriptionKey: subscriptionKey,
			Episode:         3,
			TorrentHash:     "candidate-3",
			TorrentURL:      "https://backup.test/candidate/e03",
			Title:           "E03 candidate",
			Status:          status,
			FailureReason:   "interrupted",
		}},
	}
}

func assertCandidateRuntimeCleared(t *testing.T, candidate model.EpisodeResourceCandidate) {
	t.Helper()
	if candidate.ReplacementStage != "" || candidate.StagedPath != "" || candidate.OldResourcePath != "" ||
		candidate.RollbackPath != "" || candidate.FinalPath != "" || candidate.OldTorrentHash != "" ||
		candidate.ReplacementDownloadID != nil || candidate.OldDownloadID != nil {
		t.Fatalf("candidate runtime fields were restored: %#v", candidate)
	}
}

func assertEpisodePackageTablesEmpty(t *testing.T, db *gorm.DB) {
	t.Helper()
	for name, target := range map[string]any{
		"subscriptions": &model.Subscription{},
		"episodes":      &model.SubscriptionEpisode{},
		"candidates":    &model.EpisodeResourceCandidate{},
	} {
		var count int64
		if err := db.Model(target).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("invalid/failed package left %d %s rows", count, name)
		}
	}
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
