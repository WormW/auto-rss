package downloader

import (
	"fmt"
	"math"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
)

const (
	// RetryIntervalBase 基础重试间隔（1分钟）
	RetryIntervalBase = 1 * time.Minute
	// RetryIntervalMax 最大重试间隔（60分钟）
	RetryIntervalMax = 60 * time.Minute
	// RetryExponent 指数退避基数
	RetryExponent = 2.0
)

// RetryService 下载重试服务
type RetryService struct {
	downloadRepo retryDownloadRepository
}

type retryDownloadRepository interface {
	Update(download *model.Download) error
	GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error)
}

// NewRetryService 创建重试服务
func NewRetryService(downloadRepo retryDownloadRepository) *RetryService {
	return &RetryService{
		downloadRepo: downloadRepo,
	}
}

// CalculateNextRetryTime 计算下次重试时间
// 使用指数退避策略：1分钟 -> 5分钟 -> 15分钟 -> 30分钟 -> 60分钟
func (s *RetryService) CalculateNextRetryTime(retryCount int) time.Time {
	// 计算间隔：base * (exponent ^ retryCount)
	// retryCount: 0 -> 1分钟
	// retryCount: 1 -> 2分钟
	// retryCount: 2 -> 4分钟
	// retryCount: 3 -> 8分钟
	// retryCount: 4 -> 16分钟
	// retryCount: 5+ -> 30分钟（封顶）

	interval := RetryIntervalBase
	if retryCount > 0 {
		minutes := math.Pow(RetryExponent, float64(retryCount))
		if minutes > 30 {
			minutes = 30
		}
		interval = time.Duration(minutes) * time.Minute
	}

	return time.Now().Add(interval)
}

// ShouldRetry 检查是否应该重试
func (s *RetryService) ShouldRetry(download *model.Download) (bool, string) {
	// 检查状态
	if download.Status != "failed" {
		return false, "not_failed_status"
	}

	// 检查重试次数
	if download.RetryCount >= download.MaxRetries && download.MaxRetries > 0 {
		return false, "max_retries_exceeded"
	}

	// 检查是否到了重试时间
	if download.NextRetryAt != nil && time.Now().Before(*download.NextRetryAt) {
		return false, "retry_time_not_reached"
	}

	// 检查错误类型是否可重试
	if !s.isRetryableError(download.LastError) {
		return false, "error_not_retryable"
	}

	return true, ""
}

// isRetryableError 检查错误是否可重试
func (s *RetryService) isRetryableError(errMsg string) bool {
	if errMsg == "" {
		return true
	}

	// 不可重试的错误类型（永久性错误）
	nonRetryableErrors := []string{
		"invalid torrent",
		"torrent not found",
		"banned",
		"unregistered",
		"account suspended",
		"ratio limit",
		"disk full",
		"permission denied",
	}

	errMsgLower := fmt.Sprintf("%s", errMsg)
	for _, err := range nonRetryableErrors {
		if containsIgnoreCase(errMsgLower, err) {
			return false
		}
	}

	return true
}

// PrepareRetry 准备重试
// 更新下载记录的重试状态，返回更新后的下载对象
func (s *RetryService) PrepareRetry(download *model.Download, reason string) error {
	download.RetryCount++
	nextRetryAt := s.CalculateNextRetryTime(download.RetryCount)
	download.NextRetryAt = &nextRetryAt
	download.RetryReason = reason
	download.Status = "pending" // 重置为pending状态以便重新处理
	download.ErrorMessage = ""  // 清空错误信息

	if err := s.downloadRepo.Update(download); err != nil {
		return fmt.Errorf("failed to update download for retry: %w", err)
	}

	logger.Info("Download prepared for retry",
		"download_id", download.ID,
		"title", download.Title,
		"retry_count", download.RetryCount,
		"max_retries", download.MaxRetries,
		"next_retry_at", nextRetryAt.Format("2006-01-02 15:04:05"),
		"reason", reason)

	return nil
}

// MarkFailed 标记下载失败并设置重试信息
func (s *RetryService) MarkFailed(download *model.Download, err error, reason string) error {
	download.Status = "failed"
	download.LastError = err.Error()
	download.ErrorMessage = err.Error()

	// 计算下次重试时间（如果需要）
	if download.RetryCount < download.MaxRetries || download.MaxRetries == 0 {
		nextRetryAt := s.CalculateNextRetryTime(download.RetryCount)
		download.NextRetryAt = &nextRetryAt
	}

	if err := s.downloadRepo.Update(download); err != nil {
		return fmt.Errorf("failed to mark download as failed: %w", err)
	}

	logger.Warn("Download marked as failed",
		"download_id", download.ID,
		"title", download.Title,
		"retry_count", download.RetryCount,
		"max_retries", download.MaxRetries,
		"error", err.Error(),
		"reason", reason)

	return nil
}

// ProcessRetries 处理所有待重试的失败任务
func (s *RetryService) ProcessRetries(limit int) (processed int, err error) {
	// 获取准备好重试的失败任务
	retryTasks, err := s.downloadRepo.GetFailedDownloadsReadyForRetry(limit)
	if err != nil {
		logger.Error("Failed to get failed downloads for retry", "error", err.Error())
		return 0, err
	}

	if len(retryTasks) == 0 {
		return 0, nil
	}

	logger.Info("Processing failed downloads for retry", "count", len(retryTasks))

	processed = 0
	for i := range retryTasks {
		download := &retryTasks[i]

		// 再次检查是否应该重试
		shouldRetry, reason := s.ShouldRetry(download)
		if !shouldRetry {
			logger.Debug("Skipping retry for download",
				"download_id", download.ID,
				"reason", reason,
				"retry_count", download.RetryCount)
			continue
		}

		// 准备重试
		if err := s.PrepareRetry(download, "auto_retry"); err != nil {
			logger.Error("Failed to prepare download for retry",
				"download_id", download.ID,
				"error", err.Error())
			continue
		}

		processed++
		logger.Info("Download queued for retry",
			"download_id", download.ID,
			"title", download.Title,
			"retry_count", download.RetryCount,
			"next_retry_at", download.NextRetryAt.Format("2006-01-02 15:04:05"))
	}

	return processed, nil
}

// GetRetryStats 获取重试统计
func (s *RetryService) GetRetryStats() map[string]int {
	// 返回重试统计信息
	// 实际实现需要 repository 支持
	return map[string]int{
		"total_retries_today":  0,
		"pending_retries":      0,
		"max_retries_exceeded": 0,
	}
}

// containsIgnoreCase 检查字符串是否包含子串（忽略大小写）
func containsIgnoreCase(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	// 简单实现，实际可以使用 strings.Contains(strings.ToLower(s), strings.ToLower(substr))
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower 将字节转为小写
func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
