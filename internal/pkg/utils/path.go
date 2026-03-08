package utils

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GenerateDownloadPath 生成下载路径（包含番剧名子目录）
// 智能检测：如果 basePath 已经包含番剧名，则不再重复添加
// basePath: 基础下载路径，如 /downloads 或 /downloads/番剧名
// animeName: 番剧名称
// 返回: /downloads/番剧名 或 /downloads/番剧名（如果已存在）
func GenerateDownloadPath(basePath, animeName string) string {
	// 统一路径基准，避免 /path/show 与 /path/show/Season 2 在不同入口产生漂移。
	basePath = strings.TrimSpace(basePath)
	cleanName := SanitizeDirectoryName(animeName)
	if basePath == "" {
		return cleanName
	}

	normalizedBase := filepath.Clean(basePath)
	baseDir := filepath.Base(normalizedBase)

	if isSimilarPath(baseDir, cleanName) {
		return normalizedBase
	}

	return filepath.Join(normalizedBase, cleanName)
}

// isSimilarPath 检查两个路径名是否相似
// 返回 true 表示路径已经包含番剧名，不需要再添加
func isSimilarPath(path1, path2 string) bool {
	// 标准化两个字符串
	normalized1 := normalizePath(path1)
	normalized2 := normalizePath(path2)

	// 完全匹配
	if normalized1 == normalized2 {
		return true
	}

	// 检查包含关系（长度差异不超过30%）
	len1 := len(normalized1)
	len2 := len(normalized2)
	maxLen := len1
	if len2 > len1 {
		maxLen = len2
	}

	if maxLen == 0 {
		return false
	}

	// 长度差异过大，不认为相似
	lenDiff := float64(abs(len1-len2)) / float64(maxLen)
	if lenDiff > 0.3 {
		return false
	}

	// 检查是否互相包含
	if strings.Contains(normalized1, normalized2) || strings.Contains(normalized2, normalized1) {
		return true
	}

	return false
}

// normalizePath 标准化路径名（用于比较）
func normalizePath(path string) string {
	// 先做 Unicode 级清洗：全角转半角，简繁常见字归一，中文数字转阿拉伯数字，移除季/期后缀噪声。
	path = foldFullwidth(path)
	path = foldHanVariants(path)
	path = replaceChineseNumerals(path)
	path = stripSeasonSuffix(path)
	path = normalizeCommonAliases(path)
	path = strings.ToLower(path)

	var result strings.Builder
	for _, r := range path {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// abs 返回绝对值
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func foldFullwidth(s string) string {
	if s == "" {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0x3000:
			b.WriteRune(' ')
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func replaceChineseNumerals(s string) string {
	if s == "" {
		return s
	}

	re := regexp.MustCompile(`[零一二三四五六七八九十两]{1,3}`)
	return re.ReplaceAllStringFunc(s, func(token string) string {
		if n, ok := chineseNumeralToInt(token); ok {
			return strconv.Itoa(n)
		}
		return token
	})
}

func foldHanVariants(s string) string {
	if s == "" {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(canonicalHanRune(r))
	}
	return b.String()
}

func canonicalHanRune(r rune) rune {
	variants := map[rune]rune{
		'點': '点',
		'宮': '宫',
		'處': '处',
		'裡': '里',
		'與': '与',
		'劍': '剑',
		'來': '来',
		'靈': '灵',
		'籠': '笼',
		'轉': '转',
		'聲': '声',
		'優': '优',
		'樂': '乐',
		'國': '国',
		'風': '风',
	}
	if c, ok := variants[r]; ok {
		return c
	}
	return r
}

func stripSeasonSuffix(s string) string {
	if s == "" {
		return s
	}

	patterns := []string{
		`(?i)[\(（\[]?season\s*[0-9ivx]+[\)）\]]?`,
		`(?i)(?:^|\s)[\(（\[]?s\s*[0-9]{1,2}[\)）\]]?(?:$|\s)`,
		`第\s*[0-9]+\s*[季期]`,
	}

	out := s
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		out = re.ReplaceAllString(out, " ")
	}
	out = regexp.MustCompile(`\s+`).ReplaceAllString(out, " ")
	return strings.TrimSpace(out)
}

func normalizeCommonAliases(s string) string {
	if s == "" {
		return s
	}

	out := s
	// 常见标题别名噪声：同一作品附带英文副标题时统一去除，避免双目录。
	aliasNoise := []string{
		`(?i)\bincarnation\b`,
		`(?i)\banimation\b`,
	}
	for _, p := range aliasNoise {
		re := regexp.MustCompile(p)
		out = re.ReplaceAllString(out, " ")
	}
	out = regexp.MustCompile(`\s+`).ReplaceAllString(out, " ")
	return strings.TrimSpace(out)
}

func chineseNumeralToInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	digits := map[rune]int{
		'零': 0,
		'一': 1,
		'二': 2,
		'三': 3,
		'四': 4,
		'五': 5,
		'六': 6,
		'七': 7,
		'八': 8,
		'九': 9,
		'两': 2,
	}

	if s == "十" {
		return 10, true
	}
	if strings.ContainsRune(s, '十') {
		parts := strings.SplitN(s, "十", 2)
		tens := 1
		if parts[0] != "" {
			r, _ := utf8.DecodeRuneInString(parts[0])
			v, ok := digits[r]
			if !ok {
				return 0, false
			}
			tens = v
		}
		ones := 0
		if len(parts) == 2 && parts[1] != "" {
			r, _ := utf8.DecodeRuneInString(parts[1])
			v, ok := digits[r]
			if !ok {
				return 0, false
			}
			ones = v
		}
		return tens*10 + ones, true
	}

	r, size := utf8.DecodeRuneInString(s)
	if size == len(s) {
		v, ok := digits[r]
		return v, ok
	}
	return 0, false
}

// SanitizeDirectoryName 清理目录名中的非法字符
func SanitizeDirectoryName(name string) string {
	// 替换非法字符为下划线
	// Windows/Unix 通用非法字符: / \ : * ? " < > |
	illegalChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := name

	for _, char := range illegalChars {
		result = strings.ReplaceAll(result, char, "_")
	}

	// 移除多余的空格
	result = strings.TrimSpace(result)
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")

	// 移除开头和结尾的点（Windows不允许）
	result = strings.Trim(result, ".")

	// 如果结果为空，使用默认名称
	if result == "" {
		result = "Unknown"
	}

	return result
}
