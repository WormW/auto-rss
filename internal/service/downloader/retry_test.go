package downloader

import (
	"fmt"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
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
