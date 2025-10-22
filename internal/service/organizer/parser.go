package organizer

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// FileNameInfo 从文件名解析出的信息
type FileNameInfo struct {
	OriginalName string // 原始文件名
	Title        string // 番剧标题
	Episode      int    // 集数
	Season       int    // 季度
	Fansub       string // 字幕组
	Resolution   string // 分辨率
	Language     string // 语言
	VideoCodec   string // 视频编码
	AudioCodec   string // 音频编码
	Extension    string // 文件扩展名
	Quality      string // 质量标签 (WebRip, BDRip等)
}

// FileNameParser 文件名解析器
type FileNameParser struct{}

// NewFileNameParser 创建文件名解析器
func NewFileNameParser() *FileNameParser {
	return &FileNameParser{}
}

// Parse 解析文件名，提取番剧信息
// 示例: [LoliHouse] Princess-Session Orchestra - 01 [WebRip 1080p HEVC-10bit AAC].mkv
func (p *FileNameParser) Parse(filename string) *FileNameInfo {
	info := &FileNameInfo{
		OriginalName: filename,
		Extension:    filepath.Ext(filename),
	}

	// 移除扩展名
	nameWithoutExt := strings.TrimSuffix(filename, info.Extension)

	// 1. 提取字幕组 [LoliHouse]
	info.Fansub = p.extractFansub(nameWithoutExt)

	// 2. 提取集数
	info.Episode = p.extractEpisode(nameWithoutExt)

	// 3. 提取季度 (如果有)
	info.Season = p.extractSeason(nameWithoutExt)
	if info.Season == 0 {
		info.Season = 1 // 默认为第一季
	}

	// 4. 提取分辨率
	info.Resolution = p.extractResolution(nameWithoutExt)

	// 5. 提取语言
	info.Language = p.extractLanguage(nameWithoutExt)

	// 6. 提取视频编码
	info.VideoCodec = p.extractVideoCodec(nameWithoutExt)

	// 7. 提取音频编码
	info.AudioCodec = p.extractAudioCodec(nameWithoutExt)

	// 8. 提取质量标签
	info.Quality = p.extractQuality(nameWithoutExt)

	// 9. 提取番剧标题
	info.Title = p.extractTitle(nameWithoutExt, info)

	return info
}

// extractFansub 提取字幕组
// 格式: [字幕组名称]
func (p *FileNameParser) extractFansub(filename string) string {
	re := regexp.MustCompile(`^\[([^\]]+)\]`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractEpisode 提取集数
func (p *FileNameParser) extractEpisode(filename string) int {
	// 常见集数格式:
	// - [xx] 第12集
	// - E12, EP12, Episode 12
	// - 12话, 12話
	// - S01E12
	// - - 01, - 12 (常见于番剧标题后)
	patterns := []string{
		`第?\s*(\d+)\s*[集话話]`,           // 第12集, 12话
		`[Ee][Pp]?\.?\s*(\d+)`,          // E12, EP12, Ep.12
		`Episode\s*(\d+)`,               // Episode 12
		`S\d{1,2}[Ee](\d+)`,             // S01E12, S1E12
		`-\s*(\d{1,3})\s*[\[\-]`,        // - 12 [, - 01 -
		`\s+(\d{1,3})\s+\[`,             // 空格+数字+空格+[
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(filename)
		if len(matches) > 1 {
			episode, err := strconv.Atoi(matches[1])
			if err == nil && episode > 0 && episode < 1000 {
				return episode
			}
		}
	}

	return 0
}

// extractSeason 提取季度
func (p *FileNameParser) extractSeason(filename string) int {
	// 格式: S01, S1, Season 1, 第一季
	patterns := []string{
		`[Ss]eason\s*(\d+)`,  // Season 1
		`[Ss](\d{1,2})`,      // S01, S1
		`第(\d+)季`,           // 第1季
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(filename)
		if len(matches) > 1 {
			season, err := strconv.Atoi(matches[1])
			if err == nil && season > 0 && season < 100 {
				return season
			}
		}
	}

	return 0
}

// extractResolution 提取分辨率
func (p *FileNameParser) extractResolution(filename string) string {
	patterns := []string{
		"4K",
		"UHD",
		"2160p",
		"1440p",
		"1080p",
		"720p",
		"480p",
		"360p",
	}

	filenameLower := strings.ToLower(filename)

	for _, pattern := range patterns {
		if strings.Contains(filenameLower, strings.ToLower(pattern)) {
			return pattern
		}
	}

	return ""
}

// extractLanguage 提取语言
func (p *FileNameParser) extractLanguage(filename string) string {
	// 常见语言标记
	languages := map[string]string{
		"CHS":      "CHS",
		"CHT":      "CHT",
		"简中":       "CHS",
		"繁中":       "CHT",
		"简体":       "CHS",
		"繁体":       "CHT",
		"GB":       "CHS",
		"BIG5":     "CHT",
		"简日":       "CHS",
		"繁日":       "CHT",
		"简繁":       "CHS_CHT",
		"内嵌":       "CHS",
	}

	filenameUpper := strings.ToUpper(filename)

	for key, value := range languages {
		if strings.Contains(filenameUpper, strings.ToUpper(key)) {
			return value
		}
	}

	return ""
}

// extractVideoCodec 提取视频编码
func (p *FileNameParser) extractVideoCodec(filename string) string {
	codecs := []string{
		"HEVC",
		"H.265",
		"H265",
		"x265",
		"AVC",
		"H.264",
		"H264",
		"x264",
		"VP9",
		"AV1",
	}

	filenameLower := strings.ToLower(filename)

	for _, codec := range codecs {
		if strings.Contains(filenameLower, strings.ToLower(codec)) {
			// 标准化编码名称
			codecLower := strings.ToLower(codec)
			if strings.Contains(codecLower, "265") || codecLower == "hevc" {
				return "HEVC"
			}
			if strings.Contains(codecLower, "264") || codecLower == "avc" {
				return "AVC"
			}
			return strings.ToUpper(codec)
		}
	}

	return ""
}

// extractAudioCodec 提取音频编码
func (p *FileNameParser) extractAudioCodec(filename string) string {
	codecs := []string{
		"FLAC",
		"AAC",
		"AC3",
		"DTS",
		"OPUS",
		"MP3",
		"EAC3",
		"TRUEHD",
	}

	filenameLower := strings.ToLower(filename)

	for _, codec := range codecs {
		if strings.Contains(filenameLower, strings.ToLower(codec)) {
			return strings.ToUpper(codec)
		}
	}

	return ""
}

// extractQuality 提取质量标签
func (p *FileNameParser) extractQuality(filename string) string {
	qualities := []string{
		"BDRip",
		"WEBRip",
		"WebRip",
		"WEB-DL",
		"DVDRip",
		"HDTV",
		"BluRay",
		"Blu-ray",
	}

	filenameLower := strings.ToLower(filename)

	for _, quality := range qualities {
		if strings.Contains(filenameLower, strings.ToLower(quality)) {
			// 标准化质量名称
			qualityLower := strings.ToLower(quality)
			if strings.Contains(qualityLower, "webrip") {
				return "WebRip"
			}
			if strings.Contains(qualityLower, "web-dl") {
				return "WEB-DL"
			}
			if strings.Contains(qualityLower, "blu") {
				return "BluRay"
			}
			return quality
		}
	}

	return ""
}

// extractTitle 提取番剧标题
// 从文件名中移除所有已识别的标签后，剩余部分即为标题
func (p *FileNameParser) extractTitle(filename string, info *FileNameInfo) string {
	title := filename

	// 移除字幕组标签
	if info.Fansub != "" {
		re := regexp.MustCompile(`^\[` + regexp.QuoteMeta(info.Fansub) + `\]\s*`)
		title = re.ReplaceAllString(title, "")
	}

	// 移除末尾的方括号内容 (通常包含技术信息)
	re := regexp.MustCompile(`\s*\[.*?\]\s*$`)
	title = re.ReplaceAllString(title, "")

	// 移除集数信息 (- 01, - 12 等)
	if info.Episode > 0 {
		episodePatterns := []string{
			`\s*-\s*\d{1,3}\s*$`,           // - 01
			`\s+\d{1,3}\s*$`,               // 空格+数字结尾
			`\s*[Ee][Pp]?\d{1,3}\s*$`,     // EP01
			`\s*[Ss]\d{1,2}[Ee]\d{1,3}\s*$`, // S01E01
		}
		for _, pattern := range episodePatterns {
			re := regexp.MustCompile(pattern)
			title = re.ReplaceAllString(title, "")
		}
	}

	// 清理多余空格和特殊字符
	title = strings.TrimSpace(title)
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")

	// 移除末尾的 - 或其他分隔符
	title = strings.TrimRight(title, " -_")

	return title
}

// MatchTitle 模糊匹配标题
// 计算两个标题的相似度，用于匹配订阅
func (p *FileNameParser) MatchTitle(title1, title2 string) float64 {
	// 标准化标题
	t1 := p.normalizeTitle(title1)
	t2 := p.normalizeTitle(title2)

	// 完全匹配
	if t1 == t2 {
		return 1.0
	}

	// 包含关系
	if strings.Contains(t1, t2) || strings.Contains(t2, t1) {
		return 0.8
	}

	// 计算 Levenshtein 距离
	distance := p.levenshteinDistance(t1, t2)
	maxLen := float64(max(len(t1), len(t2)))
	if maxLen == 0 {
		return 0
	}

	similarity := 1.0 - float64(distance)/maxLen
	return similarity
}

// normalizeTitle 标准化标题（用于比较）
func (p *FileNameParser) normalizeTitle(title string) string {
	// 转小写
	title = strings.ToLower(title)

	// 移除特殊字符（保留字母、数字、中文）
	var result strings.Builder
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// levenshteinDistance 计算 Levenshtein 距离
func (p *FileNameParser) levenshteinDistance(s1, s2 string) int {
	len1 := len(s1)
	len2 := len(s2)

	// 创建距离矩阵
	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
	}

	// 初始化第一行和第一列
	for i := 0; i <= len1; i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}

	// 填充矩阵
	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // 删除
				matrix[i][j-1]+1,      // 插入
				matrix[i-1][j-1]+cost, // 替换
			)
		}
	}

	return matrix[len1][len2]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
