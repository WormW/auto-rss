package bangumi

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ImageService 图片下载服务
type ImageService struct {
	httpClient *http.Client
	savePath   string
}

// NewImageService 创建图片服务
func NewImageService(savePath string) *ImageService {
	return &ImageService{
		httpClient: &http.Client{
			Timeout: 30 * 1000 * 1000 * 1000, // 30秒
		},
		savePath: savePath,
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

	// 如果文件已存在,直接返回
	if _, err := os.Stat(localPath); err == nil {
		return filename, nil
	}

	// 下载图片
	req, err := http.NewRequest("GET", coverURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Auto-RSS/1.0")
	req.Header.Set("Referer", "https://bgm.tv/")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 保存到本地
	file, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("create file failed: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		os.Remove(localPath) // 下载失败删除文件
		return "", fmt.Errorf("save file failed: %w", err)
	}

	return filename, nil
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
