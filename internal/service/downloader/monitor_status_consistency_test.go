package downloader

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestDownloadStatusConsistencyBetweenSchedulerAndMonitor verifies that
// the scheduler sets downloading status correctly and monitor can update it.
func TestDownloadStatusConsistencyBetweenSchedulerAndMonitor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	repo := repository.NewDownloadRepository(db)

	// Simulate scheduler creating a download task (status = downloading)
	download := model.Download{
		Title:       "Test Anime S01E01",
		Episode:     1,
		Status:      "downloading",
		TorrentURL:  "magnet:?xt=urn:btih:abc123",
		TorrentHash: "abc123",
	}
	if err := repo.Create(&download); err != nil {
		t.Fatalf("failed to create download: %v", err)
	}

	// Verify the download was created with downloading status
	if download.Status != "downloading" {
		t.Fatalf("expected status 'downloading', got %q", download.Status)
	}

	// Verify we can query by "downloading" status (scheduler uses this)
	results, total, err := repo.List(0, 10, "downloading")
	if err != nil {
		t.Fatalf("failed to list downloading: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected 1 downloading task, got total=%d, len=%d", total, len(results))
	}

	// Simulate monitor updating status to "completed"
	download.Status = "completed"
	if err := repo.Update(&download); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	// Verify status change is reflected
	completed, _, err := repo.List(0, 10, "completed")
	if err != nil {
		t.Fatalf("failed to list completed: %v", err)
	}
	if len(completed) != 1 || completed[0].Status != "completed" {
		t.Fatalf("expected 1 completed task, got %d", len(completed))
	}

	// Verify it's no longer in downloading
	dlResults, _, _ := repo.List(0, 10, "downloading")
	if len(dlResults) != 0 {
		t.Fatalf("expected 0 downloading after completion, got %d", len(dlResults))
	}
}

// TestMapQBStateToStatusAlignsWithRepositoryFilter verifies that
// mapQBStateToStatus returns statuses that the repository can filter.
func TestMapQBStateToStatusAlignsWithRepositoryFilter(t *testing.T) {
	// All statuses that mapQBStateToStatus can return
	possibleStatuses := map[string]bool{
		"downloading": true,
		"stalled":     true,
		"completed":   true,
		"failed":      true,
		"pending":     true,
	}

	testCases := []struct {
		qbState string
		want    string
	}{
		// Should be downloading
		{qbState: "downloading", want: "downloading"},
		{qbState: "forcedDL", want: "downloading"},

		// Should be stalled
		{qbState: "stalledDL", want: "stalled"},
		{qbState: "pausedDL", want: "stalled"},
		{qbState: "queuedDL", want: "stalled"},
		{qbState: "metaDL", want: "stalled"},
		{qbState: "checkingDL", want: "stalled"},

		// Should be completed
		{qbState: "uploading", want: "completed"},
		{qbState: "stalledUP", want: "completed"},
		{qbState: "pausedUP", want: "completed"},

		// Should be failed
		{qbState: "error", want: "failed"},
		{qbState: "missingFiles", want: "failed"},
	}

	for _, tt := range testCases {
		t.Run(tt.qbState, func(t *testing.T) {
			got := mapQBStateToStatus(tt.qbState)
			if got != tt.want {
				t.Fatalf("mapQBStateToStatus(%q) = %q, want %q", tt.qbState, got, tt.want)
			}
			// Verify the returned status is a valid repository filter status
			if !possibleStatuses[got] {
				t.Fatalf("mapQBStateToStatus(%q) returned invalid status %q", tt.qbState, got)
			}
		})
	}
}