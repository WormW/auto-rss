package downloader

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
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
//   ${title}          - 番剧名称
//   ${season}         - 季度数字 (1, 2, 3)
//   ${seasonFormat}   - 格式化季度 (01, 02, 03)
//   ${episode}        - 集数数字 (1, 2, 3)
//   ${episodeFormat}  - 格式化集数 (01, 02, 03)
//   ${fansub}         - 字幕组
//   ${resolution}     - 分辨率 (1080p, 720p)
//   ${language}       - 语言 (CHS, CHT)
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
	result = strings.ReplaceAll(result, "${title}", sanitizeFileName(ctx.Subscription.Name))
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

