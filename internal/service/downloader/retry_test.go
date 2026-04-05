package downloader

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

func TestCalculateNextRetryTime(t *testing.T) {
	svc := &RetryService{}

	tests := []struct {
		retryCount int
		minMinutes int64
		maxMinutes int64
	}{
		{0, 1, 2},   // 第0次重试：1分钟后
		{1, 2, 3},   // 第1次重试：2分钟后
		{2, 4, 5},   // 第2次重试：4分钟后
		{3, 8, 9},   // 第3次重试：8分钟后
		{4, 16, 17}, // 第4次重试：16分钟后
		{5, 30, 31}, // 第5次重试：30分钟后（封顶）
		{10, 30, 31}, // 第10次重试：仍然是30分钟
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("retry_%d", tt.retryCount), func(t *testing.T) {
			nextTime := svc.CalculateNextRetryTime(tt.retryCount)
			diff := time.Until(nextTime).Minutes()

			// 放宽精度要求，允许 0.5 分钟的误差
			if diff < float64(tt.minMinutes)-0.5 || diff > float64(tt.maxMinutes)+0.5 {
				t.Errorf("CalculateNextRetryTime(%d) = %v, expected between %d and %d minutes from now",
					tt.retryCount, diff, tt.minMinutes, tt.maxMinutes)
			}
		})
	}
}

func TestShouldRetry(t *testing.T) {
	svc := &RetryService{}
	now := time.Now()
	future := now.Add(10 * time.Minute)
	past := now.Add(-10 * time.Minute)

	tests := []struct {
		name     string
		download *model.Download
		want     bool
		reason   string
	}{
		{
			name: "可以重试-未达到最大次数",
			download: &model.Download{
				Status:      "failed",
				RetryCount:  2,
				MaxRetries:  5,
				NextRetryAt: &past,
			},
			want:   true,
			reason: "",
		},
		{
			name: "不可重试-状态不是failed",
			download: &model.Download{
				Status:      "completed",
				RetryCount:  2,
				MaxRetries:  5,
				NextRetryAt: &past,
			},
			want:   false,
			reason: "not_failed_status",
		},
		{
			name: "不可重试-超过最大重试次数",
			download: &model.Download{
				Status:      "failed",
				RetryCount:  5,
				MaxRetries:  5,
				NextRetryAt: &past,
			},
			want:   false,
			reason: "max_retries_exceeded",
		},
		{
			name: "不可重试-重试时间未到",
			download: &model.Download{
				Status:      "failed",
				RetryCount:  2,
				MaxRetries:  5,
				NextRetryAt: &future,
			},
			want:   false,
			reason: "retry_time_not_reached",
		},
		{
			name: "可以重试-无重试时间设置",
			download: &model.Download{
				Status:      "failed",
				RetryCount:  0,
				MaxRetries:  5,
				NextRetryAt: nil,
			},
			want:   true,
			reason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := svc.ShouldRetry(tt.download)
			if got != tt.want {
				t.Errorf("ShouldRetry() = %v, want %v", got, tt.want)
			}
			if reason != tt.reason {
				t.Errorf("ShouldRetry() reason = %v, want %v", reason, tt.reason)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	svc := &RetryService{}

	tests := []struct {
		errMsg string
		want   bool
	}{
		{"connection timeout", true},
		{"network error", true},
		{"temporary failure", true},
		{"invalid torrent", false},
		{"torrent not found", false},
		{"banned", false},
		{"unregistered torrent", false},
		{"account suspended", false},
		{"", true}, // 空错误默认可重试
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			got := svc.isRetryableError(tt.errMsg)
			if got != tt.want {
				t.Errorf("isRetryableError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}

// MockDownloadRepositoryForRetry for testing ProcessRetries
type mockDownloadRepoForRetry struct {
	getFailedDownloadsFunc func(limit int) ([]model.Download, error)
	updateFunc             func(download *model.Download) error
}

func (m *mockDownloadRepoForRetry) Create(download *model.Download) error { return nil }
func (m *mockDownloadRepoForRetry) Update(download *model.Download) error {
	if m.updateFunc != nil {
		return m.updateFunc(download)
	}
	return nil
}
func (m *mockDownloadRepoForRetry) Delete(id uint) error                 { return nil }
func (m *mockDownloadRepoForRetry) GetByID(id uint) (*model.Download, error) { return nil, nil }
func (m *mockDownloadRepoForRetry) GetByHash(hash string) (*model.Download, error) { return nil, nil }
func (m *mockDownloadRepoForRetry) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepoForRetry) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepoForRetry) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepoForRetry) List(offset, limit int, status string) ([]model.Download, int64, error) {
	return nil, 0, nil
}
func (m *mockDownloadRepoForRetry) ListBySubscriptionID(subscriptionID uint) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepoForRetry) UpdateStatus(id uint, status string) error { return nil }
func (m *mockDownloadRepoForRetry) BatchDelete(ids []uint) error              { return nil }
func (m *mockDownloadRepoForRetry) DeleteByStatus(status string) error        { return nil }
func (m *mockDownloadRepoForRetry) DeleteAll() error                         { return nil }
func (m *mockDownloadRepoForRetry) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	if m.getFailedDownloadsFunc != nil {
		return m.getFailedDownloadsFunc(limit)
	}
	return nil, nil
}
func (m *mockDownloadRepoForRetry) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	return nil, nil
}
func (m *mockDownloadRepoForRetry) CreateInTx(tx *gorm.DB, download *model.Download) error { return nil }
func (m *mockDownloadRepoForRetry) UpdateInTx(tx *gorm.DB, download *model.Download) error { return nil }

func TestRetryService_ProcessRetries(t *testing.T) {
	past := time.Now().Add(-10 * time.Minute)

	tests := []struct {
		name           string
		retryTasks     []model.Download
		expectedCount  int
		expectedError  bool
	}{
		{
			name: "process ready retries",
			retryTasks: []model.Download{
				{
					ID:          1,
					Status:      "failed",
					RetryCount:  1,
					MaxRetries:  5,
					NextRetryAt: &past,
					LastError:   "network timeout",
				},
				{
					ID:          2,
					Status:      "failed",
					RetryCount:  2,
					MaxRetries:  5,
					NextRetryAt: &past,
					LastError:   "connection refused",
				},
			},
			expectedCount: 2,
			expectedError: false,
		},
		{
			name:           "no tasks to retry",
			retryTasks:     []model.Download{},
			expectedCount:  0,
			expectedError:  false,
		},
		{
			name: "skip max retries exceeded",
			retryTasks: []model.Download{
				{
					ID:          1,
					Status:      "failed",
					RetryCount:  5,
					MaxRetries:  5,
					NextRetryAt: &past,
					LastError:   "network timeout",
				},
			},
			expectedCount: 0,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockDownloadRepoForRetry{
				getFailedDownloadsFunc: func(limit int) ([]model.Download, error) {
					return tt.retryTasks, nil
				},
				updateFunc: func(download *model.Download) error {
					return nil
				},
			}

			svc := NewRetryService(mockRepo)
			processed, err := svc.ProcessRetries(10)

			if tt.expectedError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if processed != tt.expectedCount {
				t.Errorf("Expected %d processed, got %d", tt.expectedCount, processed)
			}
		})
	}
}

func TestRetryService_ProcessRetries_RepositoryError(t *testing.T) {
	mockRepo := &mockDownloadRepoForRetry{
		getFailedDownloadsFunc: func(limit int) ([]model.Download, error) {
			return nil, errors.New("database error")
		},
	}

	svc := NewRetryService(mockRepo)
	processed, err := svc.ProcessRetries(10)

	if err == nil {
		t.Error("Expected error when repository fails, got nil")
	}
	if processed != 0 {
		t.Errorf("Expected 0 processed when repository fails, got %d", processed)
	}
}

func TestRetryService_ProcessRetries_Limit(t *testing.T) {
	past := time.Now().Add(-10 * time.Minute)

	mockRepo := &mockDownloadRepoForRetry{
		getFailedDownloadsFunc: func(limit int) ([]model.Download, error) {
			// Return more tasks than the limit
			tasks := make([]model.Download, 20)
			for i := 0; i < 20; i++ {
				tasks[i] = model.Download{
					ID:          uint(i + 1),
					Status:      "failed",
					RetryCount:  1,
					MaxRetries:  5,
					NextRetryAt: &past,
					LastError:   "network timeout",
				}
			}
			// Respect the limit parameter
			if limit < len(tasks) {
				return tasks[:limit], nil
			}
			return tasks, nil
		},
		updateFunc: func(download *model.Download) error {
			return nil
		},
	}

	svc := NewRetryService(mockRepo)
	processed, err := svc.ProcessRetries(5)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if processed != 5 {
		t.Errorf("Expected 5 processed (respecting limit), got %d", processed)
	}
}
