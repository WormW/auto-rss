package disk

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newDiskTestMonitor(t *testing.T) (*Monitor, *gorm.DB, repository.DownloadRepository, repository.ConfigRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}, &model.DiskSample{}, &model.DiskCleanupRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)
	monitor := NewMonitor(db, downloadRepo, repository.NewSubscriptionRepository(db), configRepo)
	return monitor, db, downloadRepo, configRepo
}

func TestDiskSamplesAndCleanupRecordsPersistForHistory(t *testing.T) {
	monitor, db, _, _ := newDiskTestMonitor(t)

	info := &DiskInfo{
		Path:         "/tmp/downloads",
		TotalBytes:   1000,
		UsedBytes:    400,
		FreeBytes:    600,
		UsagePercent: 40,
		Status:       StatusHealthy,
	}
	if err := monitor.RecordDiskSample("/tmp/downloads", info); err != nil {
		t.Fatalf("record sample: %v", err)
	}
	samples, err := monitor.ListDiskSamples(10)
	if err != nil {
		t.Fatalf("list samples: %v", err)
	}
	if len(samples) != 1 || samples[0].FreeBytes != 600 {
		t.Fatalf("expected persisted disk sample, got %#v", samples)
	}

	if err := db.Create(&model.DiskCleanupRecord{Trigger: CleanupTriggerManual, Strategy: string(CleanupByAge), DownloadPath: "/tmp/downloads", DeletedCount: 2, CreatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("create cleanup record: %v", err)
	}
	records, total, err := monitor.ListCleanupRecords(0, 10)
	if err != nil {
		t.Fatalf("list cleanup records: %v", err)
	}
	if total != 1 || len(records) != 1 || records[0].DeletedCount != 2 {
		t.Fatalf("expected persisted cleanup record, total=%d records=%#v", total, records)
	}
}

func TestRunCleanupDeletesFilesAndReportsRealCounts(t *testing.T) {
	monitor, db, downloadRepo, _ := newDiskTestMonitor(t)
	root := t.TempDir()
	oldTime := time.Now().AddDate(0, 0, -40)
	deletePath := filepath.Join(root, "old.mkv")
	if err := os.WriteFile(deletePath, []byte(strings.Repeat("x", 4096)), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	download := model.Download{Title: "old", TorrentURL: "https://example.test/old.torrent", TorrentHash: "hash-real-counts", Status: model.DownloadStatusCompleted, FilePath: deletePath, DownloadedAt: &oldTime}
	if err := downloadRepo.Create(&download); err != nil {
		t.Fatalf("create download: %v", err)
	}

	result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root})
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if !result.Cleaned || result.DeletedCount != 1 || result.SkippedCount != 0 || result.FreedBytes != 4096 {
		t.Fatalf("unexpected cleanup result: %#v", result)
	}
	if _, err := os.Stat(deletePath); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted, stat err=%v", err)
	}
	var count int64
	db.Model(&model.Download{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected DB record deleted, count=%d", count)
	}
	var records []model.DiskCleanupRecord
	if err := db.Find(&records).Error; err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(records) != 1 || records[0].DeletedCount != 1 || records[0].FreedBytes != 4096 {
		t.Fatalf("expected cleanup history record, got %#v", records)
	}
}

func TestRunCleanupPersistsFailureSummary(t *testing.T) {
	monitor, db, downloadRepo, _ := newDiskTestMonitor(t)
	root := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.mkv")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0600); err != nil {
		t.Fatalf("write outside fixture: %v", err)
	}
	oldTime := time.Now().AddDate(0, 0, -40)
	download := model.Download{Title: "outside", TorrentURL: "https://example.test/outside.torrent", TorrentHash: "hash-failure-summary", Status: model.DownloadStatusCompleted, FilePath: outsidePath, DownloadedAt: &oldTime}
	if err := downloadRepo.Create(&download); err != nil {
		t.Fatalf("create download: %v", err)
	}

	result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root})
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if result.FailedCount != 1 || len(result.FailedPaths) != 1 || result.FailedPaths[0] != outsidePath {
		t.Fatalf("expected failed path summary in result, got %#v", result)
	}

	var record model.DiskCleanupRecord
	if err := db.First(&record).Error; err != nil {
		t.Fatalf("load cleanup record: %v", err)
	}
	if record.FailedCount != 1 || !strings.Contains(record.FailedPaths, outsidePath) {
		t.Fatalf("expected persisted failure summary, got %#v", record)
	}
}

func TestRunCleanupProtectsWatchedJellyfinMedia(t *testing.T) {
	monitor, db, downloadRepo, configRepo := newDiskTestMonitor(t)
	root := t.TempDir()
	oldTime := time.Now().AddDate(0, 0, -40)
	watchedPath := filepath.Join(root, "watched.mkv")
	if err := os.WriteFile(watchedPath, []byte("watched"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Sessions":
			fmt.Fprintf(w, `[{"NowPlayingItem":{"Path":%q}}]`, watchedPath)
		case "/Users/user-1/Items":
			fmt.Fprint(w, `{"Items":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	setConfigs(t, configRepo, map[string]string{
		"media_library.type":              "jellyfin",
		"media_library.url":               server.URL,
		"media_library.token":             "token",
		"media_library.user_id":           "user-1",
		"media_library.recent_play_hours": "24",
	})
	download := model.Download{Title: "watched", TorrentURL: "https://example.test/watched.torrent", TorrentHash: "hash-watched", Status: model.DownloadStatusCompleted, FilePath: watchedPath, DownloadedAt: &oldTime}
	if err := downloadRepo.Create(&download); err != nil {
		t.Fatalf("create download: %v", err)
	}

	result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root, ProtectWatching: true})
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if result.DeletedCount != 0 || result.SkippedCount != 1 || result.MediaLibraryStatus != MediaLibraryStatusConnected {
		t.Fatalf("expected watched media skipped, got %#v", result)
	}
	if _, err := os.Stat(watchedPath); err != nil {
		t.Fatalf("expected watched file to remain: %v", err)
	}
	var count int64
	db.Model(&model.Download{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected DB record retained, count=%d", count)
	}
}

func TestRunCleanupSkipsConservativelyWhenMediaLibraryFails(t *testing.T) {
	monitor, db, downloadRepo, configRepo := newDiskTestMonitor(t)
	root := t.TempDir()
	oldTime := time.Now().AddDate(0, 0, -40)
	path := filepath.Join(root, "protected-on-failure.mkv")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()
	setConfigs(t, configRepo, map[string]string{
		"media_library.type":  "jellyfin",
		"media_library.url":   server.URL,
		"media_library.token": "token",
	})
	download := model.Download{Title: "failure", TorrentURL: "https://example.test/failure.torrent", TorrentHash: "hash-failure", Status: model.DownloadStatusCompleted, FilePath: path, DownloadedAt: &oldTime}
	if err := downloadRepo.Create(&download); err != nil {
		t.Fatalf("create download: %v", err)
	}

	result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root, ProtectWatching: true})
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if result.DeletedCount != 0 || result.SkippedCount != 1 || result.MediaLibraryStatus != MediaLibraryStatusFailed {
		t.Fatalf("expected conservative media failure skip, got %#v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file retained: %v", err)
	}
	var count int64
	db.Model(&model.Download{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected DB record retained, count=%d", count)
	}
}

func TestRunCleanupSkipsConservativelyWhenMediaLibraryUnconfigured(t *testing.T) {
	monitor, db, downloadRepo, _ := newDiskTestMonitor(t)
	root := t.TempDir()
	oldTime := time.Now().AddDate(0, 0, -40)
	path := filepath.Join(root, "protected-when-unconfigured.mkv")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	download := model.Download{Title: "unconfigured", TorrentURL: "https://example.test/unconfigured.torrent", TorrentHash: "hash-unconfigured", Status: model.DownloadStatusCompleted, FilePath: path, DownloadedAt: &oldTime}
	if err := downloadRepo.Create(&download); err != nil {
		t.Fatalf("create download: %v", err)
	}

	result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root, ProtectWatching: true})
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if result.DeletedCount != 0 || result.SkippedCount != 1 || result.MediaLibraryStatus != MediaLibraryStatusUnconfigured {
		t.Fatalf("expected conservative unconfigured media skip, got %#v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file retained: %v", err)
	}
	var count int64
	db.Model(&model.Download{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected DB record retained, count=%d", count)
	}
}

func TestRunCleanupRejectsPathBoundaryDeletion(t *testing.T) {
	cases := []struct {
		name      string
		makePath  func(root, outside string) string
		wantError string
	}{
		{name: "empty", makePath: func(_, _ string) string { return "" }, wantError: "empty path"},
		{name: "root", makePath: func(root, _ string) string { return root }, wantError: "refusing to delete download root"},
		{name: "outside", makePath: func(_, outside string) string { return outside }, wantError: "outside download root"},
		{name: "symlink escape", makePath: func(root, outside string) string {
			linkPath := filepath.Join(root, "escape")
			if err := os.Symlink(outside, linkPath); err != nil {
				return outside
			}
			return linkPath
		}, wantError: "outside download root"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			monitor, db, downloadRepo, _ := newDiskTestMonitor(t)
			root := t.TempDir()
			outsideDir := t.TempDir()
			outsidePath := filepath.Join(outsideDir, "outside.mkv")
			if err := os.WriteFile(outsidePath, []byte("outside"), 0600); err != nil {
				t.Fatalf("write outside fixture: %v", err)
			}
			oldTime := time.Now().AddDate(0, 0, -40)
			download := model.Download{Title: tc.name, TorrentURL: "https://example.test/item.torrent", TorrentHash: "hash-" + tc.name, Status: model.DownloadStatusCompleted, FilePath: tc.makePath(root, outsidePath), DownloadedAt: &oldTime}
			if err := downloadRepo.Create(&download); err != nil {
				t.Fatalf("create download: %v", err)
			}

			result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root})
			if err != nil {
				t.Fatalf("run cleanup: %v", err)
			}
			if result.DeletedCount != 0 || result.SkippedCount != 1 || !strings.Contains(result.Items[0].Reason, tc.wantError) {
				t.Fatalf("expected boundary skip %q, got %#v", tc.wantError, result)
			}
			if _, err := os.Stat(outsidePath); err != nil {
				t.Fatalf("outside file should remain: %v", err)
			}
			var count int64
			db.Model(&model.Download{}).Count(&count)
			if count != 1 {
				t.Fatalf("expected DB record retained, count=%d", count)
			}
		})
	}
}

func setConfigs(t *testing.T, repo repository.ConfigRepository, values map[string]string) {
	t.Helper()
	for key, value := range values {
		if err := repo.Set(key, value); err != nil {
			t.Fatalf("set config %s: %v", key, err)
		}
	}
}
