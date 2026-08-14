package downloader

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMapQBStateToStatus(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  string
	}{
		{name: "real downloading", state: "downloading", want: "downloading"},
		{name: "forced downloading", state: "forcedDL", want: "downloading"},
		{name: "stalled downloading", state: "stalledDL", want: "stalled"},
		{name: "paused downloading", state: "pausedDL", want: "stalled"},
		{name: "queued downloading", state: "queuedDL", want: "stalled"},
		{name: "meta downloading", state: "metaDL", want: "stalled"},
		{name: "checking downloading", state: "checkingDL", want: "stalled"},
		{name: "uploading completed", state: "uploading", want: "completed"},
		{name: "stalled uploading completed", state: "stalledUP", want: "completed"},
		{name: "error failed", state: "error", want: "failed"},
		{name: "missing files failed", state: "missingFiles", want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapQBStateToStatus(tt.state)
			if got != tt.want {
				t.Fatalf("mapQBStateToStatus(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestDownloadMonitorReconcileIgnoresReplacementDownloads(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/replacement-monitor.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Download{}, &model.Subscription{}, &model.Config{}))
	candidateID := uint(77)
	replacement := model.Download{
		SubscriptionID: 1, Title: "replacement", Episode: 1,
		TorrentURL: "magnet:replacement", TorrentHash: "replacement-hash",
		Status: model.DownloadStatusDownloading, Purpose: model.DownloadPurposeReplacement,
		ReplacementCandidateID: &candidateID, CreatedAt: time.Now().Add(-2 * ReconcileGracePeriod),
	}
	require.NoError(t, db.Create(&replacement).Error)
	oldCheckpoint := time.Now().Add(-2 * ReconcileGracePeriod)
	require.NoError(t, db.Model(&replacement).UpdateColumns(map[string]any{"created_at": oldCheckpoint, "updated_at": oldCheckpoint}).Error)
	downloadRepo := repository.NewDownloadRepository(db)
	lifecycle := &replacementMonitorLifecycle{downloads: downloadRepo}
	notifications := &replacementMonitorNotifications{}
	monitor := NewDownloadMonitor(
		db, &retryLedgerQBClient{}, downloadRepo,
		repository.NewSubscriptionRepository(db), repository.NewConfigRepository(db), "", lifecycle,
	)
	monitor.SetNotificationService(notifications)
	monitor.checkDownloads()

	persisted, err := downloadRepo.GetByID(replacement.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	assert.Zero(t, lifecycle.failureCalls)
	assert.Zero(t, notifications.calls)
}

type replacementMonitorLifecycle struct {
	downloads    repository.DownloadRepository
	failureCalls int
}

func (*replacementMonitorLifecycle) MarkDownloadCompleted(*model.Download, *model.Subscription, time.Time) error {
	return nil
}
func (*replacementMonitorLifecycle) MarkDownloadCompletedInTx(*gorm.DB, *model.Download, *model.Subscription, time.Time) error {
	return nil
}
func (*replacementMonitorLifecycle) MarkDownloadFailed(uint) error { return nil }
func (l *replacementMonitorLifecycle) PersistDownloadFailure(download *model.Download, _ bool) error {
	l.failureCalls++
	return l.downloads.Update(download)
}
func (*replacementMonitorLifecycle) DetachDownload(uint) error { return nil }

type replacementMonitorNotifications struct{ calls int }

func (n *replacementMonitorNotifications) Send(model.NotificationPayload) { n.calls++ }

func TestReplacementMonitorIsolationSkipsNormalCompletionHandler(t *testing.T) {
	if shouldRunCompletionHandler(&model.Download{Purpose: model.DownloadPurposeReplacement}) {
		t.Fatal("replacement download must not enter the normal completion handler")
	}
	if !shouldRunCompletionHandler(&model.Download{Purpose: model.DownloadPurposeNormal}) {
		t.Fatal("normal download should enter the normal completion handler")
	}
}
