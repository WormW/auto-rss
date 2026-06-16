package downloader

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
)

// RenameService 文件重命名服务
type RenameService struct {
	defaultTemplate string
}

// NewRenameService 创建重命名服务
func NewRenameService(defaultTemplate string) *RenameService {
	if defaultTemplate == "" {
		// 默认使用标准媒体库格式
		defaultTemplate = "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	}
	return &RenameService{
		defaultTemplate: defaultTemplate,
	}
}

// RenameContext 重命名上下文
type RenameContext struct {
	Subscription *model.Subscription
	Download     *model.Download
	OriginalName string
	Extension    string
	Resolution   string
}

// GenerateFileName 生成新文件名
// 支持的模板变量:
//
//	${title}          - 番剧名称
//	${season}         - 季度数字 (1, 2, 3)
//	${seasonFormat}   - 格式化季度 (01, 02, 03)
//	${episode}        - 集数数字 (1, 2, 3)
//	${episodeFormat}  - 格式化集数 (01, 02, 03)
//	${fansub}         - 字幕组
//	${resolution}     - 分辨率 (1080p, 720p)
//	${language}       - 语言 (CHS, CHT)
func (r *RenameService) GenerateFileName(ctx *RenameContext) string {
	template := r.defaultTemplate

	// 如果订阅有自定义模板，使用自定义模板
	// (后续扩展: ctx.Subscription.RenameTemplate)

	// 提取分辨率 (如果未提供)
	if ctx.Resolution == "" {
		ctx.Resolution = extractResolution(ctx.OriginalName)
	}

	// 替换模板变量
	result := template
	mediaTitle := utils.MediaLibraryTitle(ctx.Subscription.Name)
	result = strings.ReplaceAll(result, "${title}", sanitizeFileName(mediaTitle))
	result = strings.ReplaceAll(result, "${season}", fmt.Sprintf("%d", ctx.Subscription.Season))
	result = strings.ReplaceAll(result, "${seasonFormat}", fmt.Sprintf("%02d", ctx.Subscription.Season))
	result = strings.ReplaceAll(result, "${episode}", fmt.Sprintf("%d", ctx.Download.Episode))
	result = strings.ReplaceAll(result, "${episodeFormat}", fmt.Sprintf("%02d", ctx.Download.Episode))
	result = strings.ReplaceAll(result, "${fansub}", sanitizeFileName(ctx.Subscription.Fansub))
	result = strings.ReplaceAll(result, "${resolution}", ctx.Resolution)
	result = strings.ReplaceAll(result, "${language}", ctx.Subscription.Language)

	// 添加文件扩展名
	if ctx.Extension != "" {
		result += ctx.Extension
	}

	return result
}

// GetPresetTemplates 获取预设模板
func GetPresetTemplates() map[string]string {
	return map[string]string{
		// Plex/Jellyfin/Emby 标准格式
		"media_library": "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}",

		// 带字幕组的标准格式
		"media_library_fansub": "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat} [${fansub}]",

		// 带分辨率的标准格式
		"media_library_full": "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat} [${resolution}] [${fansub}]",

		// 简洁格式 (不含目录结构)
		"simple": "${title} - ${episodeFormat}",

		// 字幕组格式 (类似 ani-rss)
		"fansub_style": "[${fansub}] ${title} - ${episodeFormat} [${resolution}]",

		// 完整信息格式
		"detailed": "[${fansub}] ${title} S${seasonFormat}E${episodeFormat} [${resolution}] [${language}]",
	}
}

// extractResolution 从文件名中提取分辨率
func extractResolution(filename string) string {
	patterns := []string{
		"2160p",
		"1080p",
		"720p",
		"480p",
		"4K",
		"UHD",
	}

	filenameLower := strings.ToLower(filename)

	for _, pattern := range patterns {
		if strings.Contains(filenameLower, strings.ToLower(pattern)) {
			return pattern
		}
	}

	return "Unknown"
}

// sanitizeFileName 清理文件名中的非法字符
func sanitizeFileName(name string) string {
	// 替换非法字符
	illegalChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := name

	for _, char := range illegalChars {
		result = strings.ReplaceAll(result, char, "_")
	}

	// 移除多余的空格
	result = strings.TrimSpace(result)
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")

	return result
}

// ParseTemplate 解析模板预览（用于前端展示）
func (r *RenameService) ParseTemplate(template string, sampleData *RenameContext) string {
	tempService := &RenameService{defaultTemplate: template}
	return tempService.GenerateFileName(sampleData)
}

// ValidateTemplate 验证模板是否合法
func ValidateTemplate(template string) error {
	// 检查是否包含至少一个变量
	if !strings.Contains(template, "${") {
		return fmt.Errorf("模板必须包含至少一个变量")
	}

	// 检查变量是否闭合
	openCount := strings.Count(template, "${")
	closeCount := strings.Count(template, "}")
	if openCount != closeCount {
		return fmt.Errorf("模板变量括号不匹配")
	}

	// 检查是否包含非法路径字符（如果不是目录模板）
	// 允许 / 用于目录分隔
	illegalChars := []string{"\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range illegalChars {
		if strings.Contains(template, char) {
			return fmt.Errorf("模板包含非法字符: %s", char)
		}
	}

	return nil
}

// GetTemplateVariables 获取所有支持的模板变量说明
func GetTemplateVariables() map[string]string {
	return map[string]string{
		"${title}":         "番剧名称",
		"${season}":        "季度数字 (1, 2, 3)",
		"${seasonFormat}":  "格式化季度 (01, 02, 03)",
		"${episode}":       "集数数字 (1, 2, 3)",
		"${episodeFormat}": "格式化集数 (01, 02, 03)",
		"${fansub}":        "字幕组",
		"${resolution}":    "分辨率 (1080p, 720p)",
		"${language}":      "语言 (CHS, CHT)",
	}
}

// ExtractFileInfo 从种子文件列表中提取主视频文件信息
func ExtractFileInfo(files []TorrentFile) *FileInfo {
	if len(files) == 0 {
		return nil
	}

	// 视频文件扩展名
	videoExts := []string{".mkv", ".mp4", ".avi", ".flv", ".ts", ".m2ts"}

	var largestVideo *TorrentFile

	for i := range files {
		file := &files[i]
		ext := strings.ToLower(filepath.Ext(file.Name))

		// 检查是否是视频文件
		isVideo := false
		for _, videoExt := range videoExts {
			if ext == videoExt {
				isVideo = true
				break
			}
		}

		if !isVideo {
			continue
		}

		// 找到最大的视频文件
		if largestVideo == nil || file.Size > largestVideo.Size {
			largestVideo = file
		}
	}

	if largestVideo == nil {
		return nil
	}

	return &FileInfo{
		Name:       largestVideo.Name,
		Extension:  filepath.Ext(largestVideo.Name),
		Size:       largestVideo.Size,
		Resolution: extractResolution(largestVideo.Name),
	}
}

// FileInfo 文件信息
type FileInfo struct {
	Name       string
	Extension  string
	Size       int64
	Resolution string
}

// RenameViaQBittorrent 通过 qBittorrent API 重命名单个文件
func (r *RenameService) RenameViaQBittorrent(client QBittorrentClient, hash string, oldName, newName string) error {
	if err := client.RenameTorrentFile(hash, oldName, newName); err != nil {
		return fmt.Errorf("failed to rename torrent file: %w", err)
	}
	return nil
}

// MoveViaQBittorrent 通过 qBittorrent API 移动种子位置
func (r *RenameService) MoveViaQBittorrent(client QBittorrentClient, hash string, newLocation string) error {
	if err := client.SetLocation(hash, newLocation); err != nil {
		return fmt.Errorf("failed to move torrent: %w", err)
	}
	return nil
}

// RenameCollection 批量重命名合集种子中的所有视频文件
func (r *RenameService) RenameCollection(client QBittorrentClient, hash string, subscription *model.Subscription) (int, error) {
	files, err := client.GetTorrentFiles(hash)
	if err != nil {
		return 0, fmt.Errorf("failed to get torrent files: %w", err)
	}

	renamedCount := 0
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if !isVideoFileExt(ext) {
			continue
		}

		// 这里简化处理，实际应该解析文件名中的集数
		// 合集种子的重命名逻辑较复杂，需要解析每个文件的集数
		// 暂时跳过具体实现，保持原有行为
		_ = file
	}

	return renamedCount, nil
}

// ReorganizeSubscriptionFiles 重新组织订阅的所有已完成下载文件
func (r *RenameService) ReorganizeSubscriptionFiles(
	ctx context.Context,
	subscription *model.Subscription,
	downloads []model.Download,
	qbClient QBittorrentClient,
	configRepo repository.ConfigRepository,
	basePath string,
) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"moved":   0,
		"renamed": 0,
		"errors":  0,
	}

	// 获取重命名模板
	renameTemplate := r.defaultTemplate
	if configRepo != nil {
		if templateConfig, err := configRepo.Get("rename_template"); err == nil && templateConfig != nil && templateConfig.Value != "" {
			renameTemplate = templateConfig.Value
		}
	}
	tempService := NewRenameService(renameTemplate)

	for _, download := range downloads {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if download.TorrentHash == "" {
			logger.Warn("Download has no torrent hash, skipping",
				"download_id", download.ID,
				"episode", download.Episode)
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		// 获取种子信息
		torrentInfo, err := qbClient.GetTorrentInfo(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent info",
				"hash", download.TorrentHash,
				"error", err.Error())
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		// 获取种子文件列表
		files, err := qbClient.GetTorrentFiles(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent files",
				"hash", download.TorrentHash,
				"error", err.Error())
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		// 找到主视频文件
		mainVideoFile := ExtractFileInfo(files)
		if mainVideoFile == nil {
			logger.Warn("No video file found in torrent",
				"hash", download.TorrentHash)
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		ext := strings.ToLower(filepath.Ext(mainVideoFile.Name))

		// 生成新的文件名（带目录结构）
		renameCtx := &RenameContext{
			Subscription: subscription,
			Download: &model.Download{
				Episode: download.Episode,
			},
			OriginalName: mainVideoFile.Name,
			Extension:    ext,
		}
		newRelativePath := tempService.GenerateFileName(renameCtx)

		// 分离目录和文件名
		newDir := filepath.Dir(newRelativePath)
		newFileName := filepath.Base(newRelativePath)
		targetLocation := filepath.Join(basePath, newDir)

		// 当前位置
		currentLocation := torrentInfo.SavePath

		// Step 1: 移动种子到新位置（如果需要）
		if currentLocation != targetLocation {
			logger.Info("Moving torrent via qBittorrent API",
				"hash", download.TorrentHash,
				"from", currentLocation,
				"to", targetLocation)

			if err := qbClient.SetLocation(download.TorrentHash, targetLocation); err != nil {
				logger.Error("Failed to move torrent",
					"hash", download.TorrentHash,
					"target", targetLocation,
					"error", err.Error())
				result["errors"] = result["errors"].(int) + 1
				continue
			}
			result["moved"] = result["moved"].(int) + 1
			logger.Info("Torrent moved successfully",
				"hash", download.TorrentHash,
				"new_location", targetLocation)
		}

		// Step 2: 重命名文件（如果需要）
		oldFileName := mainVideoFile.Name
		oldFileBaseName := filepath.Base(oldFileName)

		if oldFileBaseName != newFileName {
			// 构建新的相对路径（在种子内部）
			newFilePath := newFileName
			if strings.Contains(oldFileName, string(filepath.Separator)) {
				// 如果原文件在子目录中，保持目录结构但改变文件名
				oldDir := filepath.Dir(oldFileName)
				newFilePath = filepath.Join(oldDir, newFileName)
			}

			logger.Info("Renaming file via qBittorrent API",
				"hash", download.TorrentHash,
				"from", oldFileName,
				"to", newFilePath)

			if err := qbClient.RenameTorrentFile(download.TorrentHash, oldFileName, newFilePath); err != nil {
				logger.Warn("Failed to rename file (may not be supported for multi-file torrents)",
					"hash", download.TorrentHash,
					"error", err.Error())
				// 重命名失败不算严重错误，继续处理
			} else {
				result["renamed"] = result["renamed"].(int) + 1
				logger.Info("File renamed successfully",
					"hash", download.TorrentHash,
					"new_name", newFilePath)
			}
		}
	}

	return result, nil
}

// RenameSubscriptionFiles 批量重命名订阅的已下载文件
func (r *RenameService) RenameSubscriptionFiles(
	ctx context.Context,
	subscription *model.Subscription,
	downloads []model.Download,
	qbClient QBittorrentClient,
	configRepo repository.ConfigRepository,
	downloadRepo repository.DownloadRepository,
	basePath string,
) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"moved":   0,
		"renamed": 0,
		"errors":  0,
	}

	// 获取重命名模板
	renameTemplate := r.defaultTemplate
	if configRepo != nil {
		if templateConfig, err := configRepo.Get("rename_template"); err == nil && templateConfig != nil && templateConfig.Value != "" {
			renameTemplate = templateConfig.Value
		}
	}
	tempService := NewRenameService(renameTemplate)

	for i := range downloads {
		download := &downloads[i]

		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if download.TorrentHash == "" {
			logger.Warn("Download has no torrent hash, skipping",
				"download_id", download.ID,
				"episode", download.Episode)
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		// 获取种子信息
		torrentInfo, err := qbClient.GetTorrentInfo(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent info, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash,
				"error", err.Error())
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		// 获取种子文件列表
		files, err := qbClient.GetTorrentFiles(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent files, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash,
				"error", err.Error())
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		if len(files) == 0 {
			logger.Warn("No files found in torrent, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash)
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		// 提取主视频文件
		mainVideoFile := ExtractFileInfo(files)
		if mainVideoFile == nil {
			logger.Warn("No video file found in torrent, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash)
			result["errors"] = result["errors"].(int) + 1
			continue
		}

		// 生成新的文件名和路径
		ext := strings.ToLower(filepath.Ext(mainVideoFile.Name))
		renameCtx := &RenameContext{
			Subscription: subscription,
			Download: &model.Download{
				Episode: download.Episode,
			},
			OriginalName: mainVideoFile.Name,
			Extension:    ext,
		}
		newRelativePath := tempService.GenerateFileName(renameCtx)

		// 分离目录和文件名
		newDir := filepath.Dir(newRelativePath)
		newFileName := filepath.Base(newRelativePath)
		targetLocation := filepath.Join(basePath, newDir)

		// 当前位置
		currentLocation := torrentInfo.SavePath

		// Step 1: 移动种子到新位置（如果需要）
		if currentLocation != targetLocation {
			logger.Info("Moving torrent",
				"hash", download.TorrentHash,
				"from", currentLocation,
				"to", targetLocation)

			if err := qbClient.SetLocation(download.TorrentHash, targetLocation); err != nil {
				logger.Error("Failed to move torrent",
					"hash", download.TorrentHash,
					"target", targetLocation,
					"error", err.Error())
				result["errors"] = result["errors"].(int) + 1
				continue
			}
			result["moved"] = result["moved"].(int) + 1
			logger.Info("Torrent moved successfully",
				"hash", download.TorrentHash,
				"new_location", targetLocation)
		}

		// Step 2: 重命名文件（如果需要）
		oldFileName := mainVideoFile.Name
		oldFileBaseName := filepath.Base(oldFileName)

		if oldFileBaseName != newFileName {
			// 构建新的相对路径（在种子内部）
			newFilePath := newFileName
			if strings.Contains(oldFileName, string(filepath.Separator)) {
				// 如果原文件在子目录中，保持目录结构但改变文件名
				oldDir := filepath.Dir(oldFileName)
				newFilePath = filepath.Join(oldDir, newFileName)
			}

			logger.Info("Renaming file",
				"hash", download.TorrentHash,
				"from", oldFileName,
				"to", newFilePath)

			if err := qbClient.RenameTorrentFile(download.TorrentHash, oldFileName, newFilePath); err != nil {
				logger.Warn("Failed to rename file",
					"hash", download.TorrentHash,
					"error", err.Error())
				result["errors"] = result["errors"].(int) + 1
				continue
			}
			result["renamed"] = result["renamed"].(int) + 1
			logger.Info("File renamed successfully",
				"hash", download.TorrentHash,
				"new_name", newFilePath)
		}

		// 更新数据库中的renamed_path
		download.RenamedPath = filepath.Join(targetLocation, newFileName)
		if err := downloadRepo.Update(download); err != nil {
			logger.Warn("Failed to update download record",
				"download_id", download.ID,
				"error", err.Error())
			// 不算作错误，因为文件操作已成功
		}
	}

	return result, nil
}

// isVideoFileExt 检查是否是视频文件扩展名
func isVideoFileExt(ext string) bool {
	videoExts := map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
		".mov": true, ".flv": true, ".webm": true, ".m4v": true,
		".ts": true, ".m2ts": true,
	}
	return videoExts[ext]
}
