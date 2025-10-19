package renamer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
)

// Renamer 文件重命名器接口
type Renamer interface {
	// Rename 重命名文件
	Rename(download *model.Download, sourcePath string) (string, error)
	// GenerateFileName 生成文件名
	GenerateFileName(subscription *model.Subscription, episode int, originalName string) string
}

type renamer struct{}

// NewRenamer 创建文件重命名器实例
func NewRenamer() Renamer {
	return &renamer{}
}

// Rename 重命名文件
func (r *renamer) Rename(download *model.Download, sourcePath string) (string, error) {
	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return "", fmt.Errorf("source file not found: %s", sourcePath)
	}

	// 获取文件扩展名
	ext := filepath.Ext(sourcePath)
	if ext == "" {
		ext = ".mkv" // 默认扩展名
	}

	// 生成新文件名（不包含路径）
	subscription := download.Subscription
	newFileName := r.GenerateFileName(&subscription, download.Episode, filepath.Base(sourcePath))

	// 构建目标路径
	destDir := subscription.DownloadPath
	if destDir == "" {
		destDir = filepath.Dir(sourcePath)
	}

	// 确保目标目录存在
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination directory: %w", err)
	}

	destPath := filepath.Join(destDir, newFileName)

	// 如果目标文件已存在，添加后缀
	if _, err := os.Stat(destPath); err == nil {
		base := strings.TrimSuffix(newFileName, ext)
		for i := 1; ; i++ {
			destPath = filepath.Join(destDir, fmt.Sprintf("%s_%d%s", base, i, ext))
			if _, err := os.Stat(destPath); os.IsNotExist(err) {
				break
			}
		}
	}

	// 移动文件
	if err := os.Rename(sourcePath, destPath); err != nil {
		// 如果跨设备移动失败，尝试复制后删除
		if err := copyFile(sourcePath, destPath); err != nil {
			return "", fmt.Errorf("failed to copy file: %w", err)
		}
		if err := os.Remove(sourcePath); err != nil {
			return "", fmt.Errorf("failed to remove source file: %w", err)
		}
	}

	return destPath, nil
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := dstFile.ReadFrom(srcFile); err != nil {
		return err
	}

	return nil
}

// GenerateFileName 生成文件名
func (r *renamer) GenerateFileName(subscription *model.Subscription, episode int, originalName string) string {
	// 获取文件扩展名
	ext := filepath.Ext(originalName)
	if ext == "" {
		ext = ".mkv"
	}

	// 清理番剧名称（移除特殊字符）
	name := cleanFileName(subscription.Name)

	// 生成 SxxExx 格式
	season := subscription.Season
	if season == 0 {
		season = 1
	}

	fileName := fmt.Sprintf("%s S%02dE%02d%s", name, season, episode, ext)
	return fileName
}

// cleanFileName 清理文件名中的非法字符
func cleanFileName(name string) string {
	// 移除或替换非法字符
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "：",
		"*", "＊",
		"?", "？",
		"\"", "'",
		"<", "《",
		">", "》",
		"|", "｜",
	)
	return strings.TrimSpace(replacer.Replace(name))
}
