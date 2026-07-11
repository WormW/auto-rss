package organizer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
)

// FileMover 文件移动服务
type FileMover interface {
	// Move 移动文件（支持跨文件系统）
	Move(src, dest string) (string, error)

	// Copy 复制文件
	Copy(src, dest string) error

	// MoveWithFallback 移动文件，如果目标存在则添加时间戳后缀
	// 返回实际使用的目标路径
	MoveWithFallback(src, dest string) (string, error)

	// CleanEmptyDirs 递归清理空目录
	CleanEmptyDirs(dir string)

	// IsVideoFile 检查是否是视频文件
	IsVideoFile(filePath string) bool

	// IsFileReady 检查文件是否已完全写入
	IsFileReady(filePath string) bool

	// IsAlreadyOrganized 检查文件是否已按规范格式整理
	IsAlreadyOrganized(filePath string, subscription *model.Subscription) bool
}

// fileMover 文件移动服务实现
type fileMover struct {
	videoExts []string
	link      func(string, string) error
	remove    func(string) error
}

// NewFileMover 创建文件移动服务
func NewFileMover() FileMover {
	return &fileMover{
		videoExts: []string{".mkv", ".mp4", ".avi", ".flv", ".ts", ".m2ts", ".mov", ".wmv"},
		link:      os.Link,
		remove:    os.Remove,
	}
}

// Move 移动文件（支持跨文件系统）
func (m *fileMover) Move(src, dest string) (string, error) {
	link := m.link
	if link == nil {
		link = os.Link
	}
	remove := m.remove
	if remove == nil {
		remove = os.Remove
	}
	if err := link(src, dest); err == nil {
		if removeErr := remove(src); removeErr != nil {
			moveErr := fmt.Errorf("failed to remove source after link: %w", removeErr)
			if cleanupErr := remove(dest); cleanupErr != nil {
				return "", errors.Join(moveErr, fmt.Errorf("failed to remove linked destination: %w", cleanupErr))
			}
			return "", moveErr
		}
		return dest, nil
	} else if os.IsExist(err) {
		return "", fmt.Errorf("destination already exists: %s", dest)
	}
	if err := m.copyExclusive(src, dest); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	if err := remove(src); err != nil {
		moveErr := fmt.Errorf("failed to remove source after copy: %w", err)
		if cleanupErr := remove(dest); cleanupErr != nil {
			return "", errors.Join(moveErr, fmt.Errorf("failed to remove copied destination: %w", cleanupErr))
		}
		return "", moveErr
	}

	return dest, nil
}

func (m *fileMover) copyExclusive(src, dest string) (err error) {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	target, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
	if err != nil {
		return err
	}
	cleanup := true
	closed := false
	defer func() {
		if !closed {
			if closeErr := target.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
		if cleanup || err != nil {
			_ = os.Remove(dest)
		}
	}()
	if _, err = io.Copy(target, source); err != nil {
		return err
	}
	if err = target.Chmod(info.Mode()); err != nil {
		return err
	}
	if err = target.Sync(); err != nil {
		return err
	}
	if err = target.Close(); err != nil {
		return err
	}
	closed = true
	cleanup = false
	return nil
}

// Copy 复制文件
func (m *fileMover) Copy(src, dest string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// 创建目标目录（如果不存在）
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// 复制文件权限
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dest, sourceInfo.Mode())
}

// MoveWithFallback 移动文件，如果目标存在则添加时间戳后缀
func (m *fileMover) MoveWithFallback(src, dest string) (string, error) {
	// 验证源路径
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("source file does not exist: %w", err)
	}

	// 如果目标存在，生成带时间戳的新名称
	finalDest := dest
	if _, err := os.Stat(dest); err == nil {
		ext := filepath.Ext(dest)
		base := strings.TrimSuffix(dest, ext)
		timestamp := time.Now().Format("20060102_150405")
		finalDest = fmt.Sprintf("%s_%s%s", base, timestamp, ext)
		logger.Warn("Target file already exists, using new name", "new_path", finalDest)
	}

	return m.Move(src, finalDest)
}

// CleanEmptyDirs 递归清理空目录
func (m *fileMover) CleanEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Debug("Failed to read directory for cleanup", "dir", dir, "error", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			m.CleanEmptyDirs(subDir)
		}
	}

	// 重新读取目录内容（子目录可能已被删除）
	entries, err = os.ReadDir(dir)
	if err != nil {
		return
	}

	if len(entries) == 0 {
		if err := os.Remove(dir); err != nil {
			logger.Debug("Failed to remove empty directory", "dir", dir, "error", err)
		} else {
			logger.Debug("Removed empty directory", "dir", dir)
		}
	}
}

// IsVideoFile 检查是否是视频文件
func (m *fileMover) IsVideoFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	for _, videoExt := range m.videoExts {
		if ext == videoExt {
			return true
		}
	}

	return false
}

// IsFileReady 检查文件是否准备好（完整写入且可访问）
func (m *fileMover) IsFileReady(filePath string) bool {
	// 检查文件是否存在
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}

	// 检查文件大小是否稳定（两次检查间隔1秒，大小不变）
	size1 := info.Size()
	time.Sleep(1 * time.Second)

	info, err = os.Stat(filePath)
	if err != nil {
		return false
	}

	size2 := info.Size()

	// 如果大小发生变化，说明文件仍在写入
	if size1 != size2 {
		return false
	}

	// 尝试打开文件（检查是否被锁定）
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	file.Close()

	return true
}

// IsAlreadyOrganized 检查文件是否已经被整理过（匹配规范格式）
func (m *fileMover) IsAlreadyOrganized(filePath string, subscription *model.Subscription) bool {
	filename := filepath.Base(filePath)

	// 检查文件名是否匹配规范格式模式
	// 规范格式示例: "番剧名 S01E01.mkv"
	// 使用正则表达式检查

	// 清理番剧名用于匹配
	sanitizedTitle := sanitizeDirectoryName(subscription.Name)

	// 检查文件名是否以番剧名开头，并包含 SxxExx 格式
	if !strings.Contains(strings.ToLower(filename), strings.ToLower(sanitizedTitle)) {
		return false
	}

	// 检查是否包含季集格式 (S01E01, S1E1等)
	seasonEpisodePattern := `[Ss]\d{1,2}[Ee]\d{1,2}`
	matched, err := regexp.MatchString(seasonEpisodePattern, filename)
	if err != nil {
		logger.Error("Failed to match season/episode pattern", "error", err)
		return false
	}

	if !matched {
		return false
	}

	// 检查文件是否在 Season 目录下
	parentDir := filepath.Base(filepath.Dir(filePath))
	if strings.HasPrefix(strings.ToLower(parentDir), "season") {
		logger.Debug("File appears to be already organized",
			"path", filePath,
			"parent_dir", parentDir)
		return true
	}

	return false
}

// ValidateMovePaths 验证移动操作的源路径和目标路径
// 返回验证错误（如果有）
func ValidateMovePaths(src, dest, allowedSrcRoot, allowedDestRoot string) error {
	// 验证源路径
	if _, err := utils.ValidatePath(src, allowedSrcRoot); err != nil {
		return fmt.Errorf("source path validation failed: %w", err)
	}

	// 验证目标路径
	if _, err := utils.ValidatePath(dest, allowedDestRoot); err != nil {
		return fmt.Errorf("destination path validation failed: %w", err)
	}

	return nil
}
