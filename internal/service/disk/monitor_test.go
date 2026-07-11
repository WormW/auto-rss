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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newDiskTestMonitor(t *testing.T) (*Monitor, *gorm.DB, repository.DownloadRepository, repository.ConfigRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.Config{}, &model.DiskSample{}, &model.DiskCleanupRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)
	monitor := NewMonitor(db, downloadRepo, repository.NewSubscriptionRepository(db), configRepo)
	return monitor, db, downloadRepo, configRepo
}

func TestRunCleanupDetachesDownloadedEpisodeWithoutLosingResource(t *testing.T) {
	monitor, db, downloadRepo, _ := newDiskTestMonitor(t)
	root := t.TempDir()
	path := filepath.Join(root, "ledger.mkv")
	require.NoError(t, os.WriteFile(path, []byte("video"), 0600))
	oldTime := time.Now().AddDate(0, 0, -40)
	download := model.Download{
		SubscriptionID: 1, Title: "ledger", Episode: 1,
		TorrentURL: "https://example.test/ledger", TorrentHash: "disk-ledger",
		Status: model.DownloadStatusCompleted, FilePath: path, DownloadedAt: &oldTime,
	}
	require.NoError(t, downloadRepo.Create(&download))
	ledger := model.SubscriptionEpisode{
		SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
		ActiveTorrentHash: download.TorrentHash, ActiveTorrentURL: download.TorrentURL, ActiveTitle: download.Title,
	}
	require.NoError(t, db.Create(&ledger).Error)

	result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root})
	require.NoError(t, err)
	assert.Equal(t, 1, result.DeletedCount)
	after, err := repository.NewEpisodeRepository(db).GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, after.Status)
	assert.Nil(t, after.ActiveDownloadID)
	assert.Equal(t, "disk-ledger", after.ActiveTorrentHash)
	assert.Equal(t, download.TorrentURL, after.ActiveTorrentURL)
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
	if len(result.Items) != 1 || result.Items[0].Action != "skipped" || result.Items[0].Reason != "media is currently watched or recently played" {
		t.Fatalf("expected clear protected skip reason, got %#v", result.Items)
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

func TestMediaLibraryStateMatchesProtectedPaths(t *testing.T) {
	root := t.TempDir()
	protectedFile := filepath.Join(root, "shows", "Protected", "episode-01.mkv")
	protectedDir := filepath.Join(root, "shows", "Active")
	protectedChild := filepath.Join(root, "shows", "Movie", "part-01.mkv")
	for _, path := range []string{filepath.Dir(protectedFile), protectedDir, filepath.Dir(protectedChild)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatalf("create fixture dir %s: %v", path, err)
		}
	}
	for _, path := range []string{protectedFile, filepath.Join(protectedDir, "episode-02.mkv"), protectedChild} {
		if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	protectedFileAbs := normalizedProtectedPath(t, protectedFile)
	protectedDirAbs := normalizedProtectedPath(t, protectedDir)
	protectedChildAbs := normalizedProtectedPath(t, protectedChild)

	state := mediaLibraryState{ProtectedPaths: map[string]struct{}{
		protectedFileAbs:  {},
		protectedDirAbs:   {},
		protectedChildAbs: {},
	}}

	cases := []struct {
		name     string
		download model.Download
		want     bool
	}{
		{
			name:     "file path exact match",
			download: model.Download{FilePath: protectedFile},
			want:     true,
		},
		{
			name:     "renamed path exact match",
			download: model.Download{FilePath: filepath.Join(root, "shows", "renamed-source.mkv"), RenamedPath: protectedFile},
			want:     true,
		},
		{
			name:     "download child of protected parent",
			download: model.Download{FilePath: filepath.Join(protectedDir, "episode-02.mkv")},
			want:     true,
		},
		{
			name:     "protected child of download parent",
			download: model.Download{FilePath: filepath.Dir(protectedChild)},
			want:     true,
		},
		{
			name:     "sibling prefix is not protected",
			download: model.Download{FilePath: filepath.Join(root, "shows", "Protected Extras", "episode-01.mkv")},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := state.IsProtected(tc.download); got != tc.want {
				t.Fatalf("IsProtected()=%v, want %v", got, tc.want)
			}
		})
	}

	t.Run("symlink resolved path", func(t *testing.T) {
		linkPath := filepath.Join(root, "linked-protected.mkv")
		if err := os.Symlink(protectedFile, linkPath); err != nil {
			t.Skipf("symlink not available: %v", err)
		}
		if got := state.IsProtected(model.Download{FilePath: linkPath}); !got {
			t.Fatalf("expected symlink to protected file to match")
		}
	})
}

func TestRunCleanupDeletesOnlyConnectedUnprotectedMedia(t *testing.T) {
	monitor, db, downloadRepo, configRepo := newDiskTestMonitor(t)
	root := t.TempDir()
	oldTime := time.Now().AddDate(0, 0, -40)
	protectedDir := filepath.Join(root, "active-show")
	protectedPath := filepath.Join(protectedDir, "episode-01.mkv")
	unprotectedPath := filepath.Join(root, "stale-show", "episode-01.mkv")
	if err := os.MkdirAll(filepath.Dir(protectedPath), 0700); err != nil {
		t.Fatalf("create protected dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(unprotectedPath), 0700); err != nil {
		t.Fatalf("create unprotected dir: %v", err)
	}
	if err := os.WriteFile(protectedPath, []byte("keep"), 0600); err != nil {
		t.Fatalf("write protected fixture: %v", err)
	}
	if err := os.WriteFile(unprotectedPath, []byte(strings.Repeat("x", 1024)), 0600); err != nil {
		t.Fatalf("write unprotected fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Sessions":
			fmt.Fprintf(w, `[{"NowPlayingItem":{"Path":%q}}]`, protectedDir)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	setConfigs(t, configRepo, map[string]string{
		"media_library.type":  "jellyfin",
		"media_library.url":   server.URL,
		"media_library.token": "token",
	})

	for _, download := range []model.Download{
		{Title: "active", TorrentURL: "https://example.test/active.torrent", TorrentHash: "hash-active", Status: model.DownloadStatusCompleted, FilePath: protectedPath, DownloadedAt: &oldTime},
		{Title: "stale", TorrentURL: "https://example.test/stale.torrent", TorrentHash: "hash-stale", Status: model.DownloadStatusCompleted, FilePath: unprotectedPath, DownloadedAt: &oldTime},
	} {
		if err := downloadRepo.Create(&download); err != nil {
			t.Fatalf("create download %s: %v", download.Title, err)
		}
	}

	result, err := monitor.RunCleanup(CleanupOptions{Trigger: CleanupTriggerManual, Strategy: CleanupByAge, KeepDays: 30, DownloadPath: root, ProtectWatching: true})
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if result.MediaLibraryStatus != MediaLibraryStatusConnected || result.DeletedCount != 1 || result.SkippedCount != 1 || result.FreedBytes != 1024 {
		t.Fatalf("expected one protected skip and one connected unprotected delete, got %#v", result)
	}

	itemsByPath := map[string]CleanupItem{}
	for _, item := range result.Items {
		itemsByPath[item.Path] = item
	}
	if item := itemsByPath[protectedPath]; item.Action != "skipped" || item.Reason != "media is currently watched or recently played" {
		t.Fatalf("expected protected item to skip with reason, got %#v", item)
	}
	if item := itemsByPath[unprotectedPath]; item.Action != "deleted" || item.FreedBytes != 1024 {
		t.Fatalf("expected unprotected item to delete, got %#v", item)
	}
	if _, err := os.Stat(protectedPath); err != nil {
		t.Fatalf("expected protected file retained: %v", err)
	}
	if _, err := os.Stat(unprotectedPath); !os.IsNotExist(err) {
		t.Fatalf("expected unprotected file deleted, stat err=%v", err)
	}
	var remaining []model.Download
	if err := db.Find(&remaining).Error; err != nil {
		t.Fatalf("list remaining downloads: %v", err)
	}
	if len(remaining) != 1 || remaining[0].TorrentHash != "hash-active" {
		t.Fatalf("expected only protected download retained, got %#v", remaining)
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

func normalizedProtectedPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs protected path %s: %v", path, err)
	}
	if realPath, err := filepath.EvalSymlinks(abs); err == nil {
		abs = realPath
	}
	return abs
}
