package bangumi

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/ratelimit"
)

// ImageService 图片下载服务
type ImageService struct {
	httpClient   *http.Client
	savePath     string
	rateLimiter  *ratelimit.RateLimiter
	retryConfig  *RetryConfig
	cacheMutex   sync.RWMutex
	pendingCache map[string]chan struct{} // 用于防止并发下载同一图片
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries  int           // 最大重试次数
	BaseDelay   time.Duration // 基础延迟
	MaxDelay    time.Duration // 最大延迟
	RetryStatus map[int]bool  // 需要重试的 HTTP 状态码
}

// NewImageService 创建图片服务
func NewImageService(savePath string) *ImageService {
	return &ImageService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		savePath:    savePath,
		rateLimiter: ratelimit.NewRateLimiter(2, 5), // 每秒2个请求，最多5个突发
		retryConfig: &RetryConfig{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   30 * time.Second,
			RetryStatus: map[int]bool{
				429: true, // Too Many Requests
				500: true, // Internal Server Error
				502: true, // Bad Gateway
				503: true, // Service Unavailable
				504: true, // Gateway Timeout
			},
		},
		pendingCache: make(map[string]chan struct{}),
	}
}

// SetProxy 设置代理
func (s *ImageService) SetProxy(proxyURL string) error {
	if proxyURL == "" {
		s.httpClient.Transport = nil
		return nil
	}

	proxy, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}

	s.httpClient.Transport = &http.Transport{
		Proxy: http.ProxyURL(proxy),
	}
	return nil
}

// DownloadCover 下载封面图片并保存到本地
// 返回本地相对路径
func (s *ImageService) DownloadCover(coverURL string, animeID int) (string, error) {
	if coverURL == "" {
		return "", fmt.Errorf("cover URL is empty")
	}

	// 确保保存目录存在
	if err := os.MkdirAll(s.savePath, 0755); err != nil {
		return "", fmt.Errorf("create save directory failed: %w", err)
	}

	// 生成文件名: bangumi_{id}_{hash}.{ext}
	ext := s.getImageExtension(coverURL)
	hash := s.generateHash(coverURL)
	filename := fmt.Sprintf("bangumi_%d_%s%s", animeID, hash, ext)
	localPath := filepath.Join(s.savePath, filename)

	// 检查是否已在下载中（防止并发重复下载）
	if waitChan := s.getPendingDownload(filename); waitChan != nil {
		<-waitChan // 等待下载完成
		// 再次检查文件是否存在
		if _, err := os.Stat(localPath); err == nil {
			return filename, nil
		}
	}

	// 如果文件已存在，直接返回
	if _, err := os.Stat(localPath); err == nil {
		return filename, nil
	}

	// 标记为正在下载
	doneChan := s.markPendingDownload(filename)
	defer s.unmarkPendingDownload(filename, doneChan)

	// 使用限流器等待许可
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := s.rateLimiter.Wait(ctx, "bangumi_image"); err != nil {
		return "", fmt.Errorf("rate limit wait failed: %w", err)
	}

	// 带重试的下载
	data, err := s.downloadWithRetry(coverURL)
	if err != nil {
		return "", fmt.Errorf("download failed after retries: %w", err)
	}

	// 保存到本地
	if err := s.saveFile(localPath, data); err != nil {
		return "", err
	}

	return filename, nil
}

// downloadWithRetry 带指数退避的重试下载
func (s *ImageService) downloadWithRetry(coverURL string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= s.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			// 计算延迟（指数退避）
			delay := s.calculateBackoff(attempt)
			logger.Info("Retrying image download",
				"attempt", attempt,
				"delay", delay,
				"url", coverURL)
			time.Sleep(delay)
		}

		data, err := s.doDownload(coverURL)
		if err == nil {
			return data, nil
		}

		lastErr = err

		// 检查是否需要重试
		if !s.shouldRetry(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// doDownload 执行单次下载
func (s *ImageService) doDownload(coverURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", coverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://bgm.tv/")
	req.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, &retryableError{err: err}
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		if s.retryConfig.RetryStatus[resp.StatusCode] {
			return nil, &retryableError{err: fmt.Errorf("HTTP %d", resp.StatusCode), statusCode: resp.StatusCode}
		}
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &retryableError{err: fmt.Errorf("read body failed: %w", err)}
	}

	return data, nil
}

// calculateBackoff 计算指数退避延迟
func (s *ImageService) calculateBackoff(attempt int) time.Duration {
	delay := s.retryConfig.BaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > s.retryConfig.MaxDelay {
		delay = s.retryConfig.MaxDelay
	}
	// 添加随机抖动 (±25%)
	jitter := time.Duration(float64(delay) * (0.75 + 0.5*float64(time.Now().UnixNano()%100)/100))
	return jitter
}

// shouldRetry 判断错误是否应该重试
func (s *ImageService) shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	if retryable, ok := err.(*retryableError); ok {
		return retryable.statusCode == 0 || s.retryConfig.RetryStatus[retryable.statusCode]
	}

	return false
}

// saveFile 保存文件到本地
func (s *ImageService) saveFile(localPath string, data []byte) error {
	// 创建临时文件
	tempPath := localPath + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}

	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(tempPath)
		return fmt.Errorf("write file failed: %w", err)
	}
	file.Close()

	// 原子重命名
	if err := os.Rename(tempPath, localPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename file failed: %w", err)
	}

	return nil
}

// getPendingDownload 检查是否有正在进行的下载
func (s *ImageService) getPendingDownload(filename string) chan struct{} {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()
	return s.pendingCache[filename]
}

// markPendingDownload 标记开始下载
func (s *ImageService) markPendingDownload(filename string) chan struct{} {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	doneChan := make(chan struct{})
	s.pendingCache[filename] = doneChan
	return doneChan
}

// unmarkPendingDownload 取消下载标记
func (s *ImageService) unmarkPendingDownload(filename string, doneChan chan struct{}) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	close(doneChan)
	delete(s.pendingCache, filename)
}

// getImageExtension 从URL获取图片扩展名
func (s *ImageService) getImageExtension(url string) string {
	ext := filepath.Ext(url)
	// 移除可能的查询参数
	if idx := strings.Index(ext, "?"); idx != -1 {
		ext = ext[:idx]
	}

	// 验证是常见图片格式
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return ext
	default:
		return ".jpg" // 默认使用jpg
	}
}

// generateHash 生成URL的MD5哈希(前8位)
func (s *ImageService) generateHash(url string) string {
	hash := md5.Sum([]byte(url))
	return fmt.Sprintf("%x", hash)[:8]
}

// retryableError 可重试的错误类型
type retryableError struct {
	err        error
	statusCode int
}

func (e *retryableError) Error() string {
	return e.err.Error()
}

func (e *retryableError) Unwrap() error {
	return e.err
}
