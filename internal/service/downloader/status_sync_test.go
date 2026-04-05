package downloader

import (
	"errors"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// MockDownloadRepository for testing
type mockDownloadRepo struct {
	getByHashFunc func(hash string) (*model.Download, error)
	updateFunc    func(download *model.Download) error
	listFunc      func(offset, limit int, status string) ([]model.Download, int64, error)
}

func (m *mockDownloadRepo) Create(download *model.Download) error {
	return nil
}

func (m *mockDownloadRepo) Update(download *model.Download) error {
	if m.updateFunc != nil {
		return m.updateFunc(download)
	}
	return nil
}

func (m *mockDownloadRepo) Delete(id uint) error {
	return nil
}

func (m *mockDownloadRepo) GetByID(id uint) (*model.Download, error) {
	return nil, nil
}

func (m *mockDownloadRepo) GetByHash(hash string) (*model.Download, error) {
	if m.getByHashFunc != nil {
		return m.getByHashFunc(hash)
	}
	return nil, nil
}

func (m *mockDownloadRepo) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	return nil, nil
}

func (m *mockDownloadRepo) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	return nil, nil
}

func (m *mockDownloadRepo) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	return nil, nil
}

func (m *mockDownloadRepo) List(offset, limit int, status string) ([]model.Download, int64, error) {
	if m.listFunc != nil {
		return m.listFunc(offset, limit, status)
	}
	return nil, 0, nil
}

func (m *mockDownloadRepo) ListBySubscriptionID(subscriptionID uint) ([]model.Download, error) {
	return nil, nil
}

func (m *mockDownloadRepo) UpdateStatus(id uint, status string) error {
	return nil
}

func (m *mockDownloadRepo) BatchDelete(ids []uint) error {
	return nil
}

func (m *mockDownloadRepo) DeleteByStatus(status string) error {
	return nil
}

func (m *mockDownloadRepo) DeleteAll() error {
	return nil
}

func (m *mockDownloadRepo) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	return nil, nil
}

func (m *mockDownloadRepo) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	return nil, nil
}

func (m *mockDownloadRepo) CreateInTx(tx *gorm.DB, download *model.Download) error {
	return nil
}

func (m *mockDownloadRepo) UpdateInTx(tx *gorm.DB, download *model.Download) error {
	return nil
}

// MockNotificationService for testing
type mockNotificationService struct {
	sentPayloads []model.NotificationPayload
}

func (m *mockNotificationService) Send(payload model.NotificationPayload) {
	if m.sentPayloads == nil {
		m.sentPayloads = []model.NotificationPayload{}
	}
	m.sentPayloads = append(m.sentPayloads, payload)
}

func TestIsTorrentComplete(t *testing.T) {
	tests := []struct {
		name     string
		torrent  *TorrentInfo
		expected bool
	}{
		{
			name:     "nil torrent",
			torrent:  nil,
			expected: false,
		},
		{
			name: "complete by progress",
			torrent: &TorrentInfo{
				Progress: 0.9999,
			},
			expected: true,
		},
		{
			name: "complete by size",
			torrent: &TorrentInfo{
				Size:       1000,
				Downloaded: 1000,
				Progress:   0.5,
			},
			expected: true,
		},
		{
			name: "not complete - low progress",
			torrent: &TorrentInfo{
				Progress: 0.5,
			},
			expected: false,
		},
		{
			name: "not complete - partial download",
			torrent: &TorrentInfo{
				Size:       1000,
				Downloaded: 500,
				Progress:   0.5,
			},
			expected: false,
		},
		{
			name: "edge case - just below threshold",
			torrent: &TorrentInfo{
				Progress: 0.9998,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTorrentComplete(tt.torrent)
			if got != tt.expected {
				t.Errorf("isTorrentComplete() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStatusSync_UpdateStatus(t *testing.T) {
	tests := []struct {
		name           string
		download       *model.Download
		torrent        *TorrentInfo
		expectedChange bool
		expectedStatus string
	}{
		{
			name: "status unchanged",
			download: &model.Download{
				ID:     1,
				Status: "downloading",
			},
			torrent: &TorrentInfo{
				Hash:  "abc123",
				State: "downloading",
			},
			expectedChange: false,
			expectedStatus: "downloading",
		},
		{
			name: "status changed to downloading",
			download: &model.Download{
				ID:     1,
				Status: "pending",
			},
			torrent: &TorrentInfo{
				Hash:  "abc123",
				State: "downloading",
			},
			expectedChange: true,
			expectedStatus: "downloading",
		},
		{
			name: "status changed to completed via uploading",
			download: &model.Download{
				ID:     1,
				Status: "downloading",
			},
			torrent: &TorrentInfo{
				Hash:  "abc123",
				State: "uploading",
			},
			expectedChange: true,
			expectedStatus: "completed",
		},
		{
			name: "status changed to completed via progress",
			download: &model.Download{
				ID:     1,
				Status: "downloading",
			},
			torrent: &TorrentInfo{
				Hash:     "abc123",
				State:    "downloading",
				Progress: 0.9999,
			},
			expectedChange: true,
			expectedStatus: "completed",
		},
		{
			name: "status changed to failed",
			download: &model.Download{
				ID:     1,
				Status: "downloading",
			},
			torrent: &TorrentInfo{
				Hash:  "abc123",
				State: "error",
			},
			expectedChange: true,
			expectedStatus: "failed",
		},
		{
			name: "status changed to stalled",
			download: &model.Download{
				ID:     1,
				Status: "downloading",
			},
			torrent: &TorrentInfo{
				Hash:  "abc123",
				State: "stalledDL",
			},
			expectedChange: true,
			expectedStatus: "stalled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockDownloadRepo{}
			mockNotify := &mockNotificationService{}
			sync := NewStatusSync(mockRepo, mockNotify)

			changed, err := sync.UpdateStatus(tt.download, tt.torrent)
			if err != nil {
				t.Fatalf("UpdateStatus() error = %v", err)
			}

			if changed != tt.expectedChange {
				t.Errorf("UpdateStatus() changed = %v, want %v", changed, tt.expectedChange)
			}

			if tt.download.Status != tt.expectedStatus {
				t.Errorf("UpdateStatus() status = %v, want %v", tt.download.Status, tt.expectedStatus)
			}
		})
	}
}

func TestStatusSync_UpdateStatus_SetsErrorMessage(t *testing.T) {
	mockRepo := &mockDownloadRepo{}
	mockNotify := &mockNotificationService{}
	sync := NewStatusSync(mockRepo, mockNotify)

	download := &model.Download{
		ID:     1,
		Status: "downloading",
	}
	torrent := &TorrentInfo{
		Hash:  "abc123",
		State: "error",
	}

	_, err := sync.UpdateStatus(download, torrent)
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	if download.ErrorMessage == "" {
		t.Error("Expected ErrorMessage to be set for failed status")
	}
}

func TestStatusSync_Sync(t *testing.T) {
	now := time.Now()
	updatedDownloads := []*model.Download{}

	mockRepo := &mockDownloadRepo{
		getByHashFunc: func(hash string) (*model.Download, error) {
			if hash == "abc123" {
				return &model.Download{
					ID:        1,
					Status:    "downloading",
					UpdatedAt: now,
				}, nil
			}
			return nil, nil
		},
		updateFunc: func(download *model.Download) error {
			updatedDownloads = append(updatedDownloads, download)
			return nil
		},
	}
	mockNotify := &mockNotificationService{}
	sync := NewStatusSync(mockRepo, mockNotify)

	torrents := []*TorrentInfo{
		{Hash: "abc123", State: "uploading"},    // will change to completed
		{Hash: "xyz789", State: "downloading"}, // not in DB
	}

	err := sync.Sync(torrents)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Only one download should be updated (the one found in DB)
	if len(updatedDownloads) != 1 {
		t.Errorf("Expected 1 download to be updated, got %d", len(updatedDownloads))
	}

	if len(updatedDownloads) > 0 && updatedDownloads[0].Status != "completed" {
		t.Errorf("Expected status to be 'completed', got %s", updatedDownloads[0].Status)
	}
}

func TestStatusSync_Sync_RepositoryError(t *testing.T) {
	mockRepo := &mockDownloadRepo{
		getByHashFunc: func(hash string) (*model.Download, error) {
			return nil, errors.New("database error")
		},
	}
	mockNotify := &mockNotificationService{}
	sync := NewStatusSync(mockRepo, mockNotify)

	torrents := []*TorrentInfo{
		{Hash: "abc123", State: "downloading"},
	}

	err := sync.Sync(torrents)
	if err == nil {
		t.Error("Expected error when repository fails, got nil")
	}
}

func TestStatusSync_Reconcile(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name               string
		torrents           []*TorrentInfo
		downloadingTasks   []model.Download
		stalledTasks       []model.Download
		expectedReconciled int
		expectedSkipped    int
	}{
		{
			name: "reconcile missing download",
			torrents: []*TorrentInfo{
				{Hash: "abc123"},
			},
			downloadingTasks: []model.Download{
				{ID: 1, TorrentHash: "xyz789", UpdatedAt: now.Add(-20 * time.Minute)},
			},
			stalledTasks:       []model.Download{},
			expectedReconciled: 1,
			expectedSkipped:    0,
		},
		{
			name: "skip within grace period",
			torrents: []*TorrentInfo{
				{Hash: "abc123"},
			},
			downloadingTasks: []model.Download{
				{ID: 1, TorrentHash: "xyz789", UpdatedAt: now.Add(-5 * time.Minute)},
			},
			stalledTasks:       []model.Download{},
			expectedReconciled: 0,
			expectedSkipped:    1,
		},
		{
			name: "skip existing torrent",
			torrents: []*TorrentInfo{
				{Hash: "abc123"},
			},
			downloadingTasks: []model.Download{
				{ID: 1, TorrentHash: "abc123", UpdatedAt: now.Add(-20 * time.Minute)},
			},
			stalledTasks:       []model.Download{},
			expectedReconciled: 0,
			expectedSkipped:    0,
		},
		{
			name: "skip empty hash",
			torrents: []*TorrentInfo{},
			downloadingTasks: []model.Download{
				{ID: 1, TorrentHash: "", UpdatedAt: now.Add(-20 * time.Minute)},
			},
			stalledTasks:       []model.Download{},
			expectedReconciled: 0,
			expectedSkipped:    0,
		},
		{
			name: "reconcile from stalled tasks",
			torrents: []*TorrentInfo{
				{Hash: "abc123"},
			},
			downloadingTasks: []model.Download{},
			stalledTasks: []model.Download{
				{ID: 2, TorrentHash: "xyz789", UpdatedAt: now.Add(-20 * time.Minute)},
			},
			expectedReconciled: 1,
			expectedSkipped:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updatedDownloads := []*model.Download{}

			mockRepo := &mockDownloadRepo{
				updateFunc: func(download *model.Download) error {
					updatedDownloads = append(updatedDownloads, download)
					return nil
				},
			}
			mockNotify := &mockNotificationService{}
			sync := NewStatusSync(mockRepo, mockNotify)

			reconciled, skipped, err := sync.Reconcile(tt.torrents, tt.downloadingTasks, tt.stalledTasks)
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}

			if reconciled != tt.expectedReconciled {
				t.Errorf("Reconcile() reconciled = %v, want %v", reconciled, tt.expectedReconciled)
			}

			if skipped != tt.expectedSkipped {
				t.Errorf("Reconcile() skipped = %v, want %v", skipped, tt.expectedSkipped)
			}

			if reconciled > 0 && len(updatedDownloads) != reconciled {
				t.Errorf("Expected %d downloads to be updated, got %d", reconciled, len(updatedDownloads))
			}
		})
	}
}

func TestStatusSync_Reconcile_SendsNotification(t *testing.T) {
	now := time.Now()

	mockRepo := &mockDownloadRepo{
		updateFunc: func(download *model.Download) error {
			return nil
		},
	}
	mockNotify := &mockNotificationService{}
	sync := NewStatusSync(mockRepo, mockNotify)

	torrents := []*TorrentInfo{}
	downloadingTasks := []model.Download{
		{ID: 1, TorrentHash: "xyz789", UpdatedAt: now.Add(-20 * time.Minute)},
	}
	stalledTasks := []model.Download{}

	_, _, err := sync.Reconcile(torrents, downloadingTasks, stalledTasks)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if len(mockNotify.sentPayloads) != 1 {
		t.Errorf("Expected 1 notification to be sent, got %d", len(mockNotify.sentPayloads))
	}
}

func TestStatusSync_Reconcile_RepositoryError(t *testing.T) {
	now := time.Now()

	mockRepo := &mockDownloadRepo{
		updateFunc: func(download *model.Download) error {
			return errors.New("update failed")
		},
	}
	mockNotify := &mockNotificationService{}
	sync := NewStatusSync(mockRepo, mockNotify)

	torrents := []*TorrentInfo{}
	downloadingTasks := []model.Download{
		{ID: 1, TorrentHash: "xyz789", UpdatedAt: now.Add(-20 * time.Minute)},
	}
	stalledTasks := []model.Download{}

	reconciled, _, err := sync.Reconcile(torrents, downloadingTasks, stalledTasks)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	// Should not count as reconciled if update failed
	if reconciled != 0 {
		t.Errorf("Expected 0 reconciled when update fails, got %d", reconciled)
	}
}
