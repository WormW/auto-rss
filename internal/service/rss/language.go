package rss

import (
	"regexp"
	"strings"
)

// LanguageType 语言类型
type LanguageType string

const (
	LangUnknown LanguageType = "unknown" // 未知
	LangCHS     LanguageType = "chs"     // 简体中文
	LangCHT     LanguageType = "cht"     // 繁体中文
	LangJP      LanguageType = "jp"      // 日语
	LangEN      LanguageType = "en"      // 英语
)

// LanguagePreference 语言偏好设置
type LanguagePreference string

const (
	LangPrefAuto LanguagePreference = "auto" // 自动学习
	LangPrefCHS  LanguagePreference = "chs"  // 简体中文优先
	LangPrefCHT  LanguagePreference = "cht"  // 繁体中文优先
	LangPrefBoth LanguagePreference = "both" // 同时保留两种语言
)

// chsKeywords 简体中文标识关键词（按优先级排序）
var chsKeywords = []string{
	"chs",      // 最标准
	"sc",       // Simplified Chinese
	"简体",     // 中文标识
	"简中",     // 简写
	"gb",       // GB编码
	"简",       // 单字简写（最后匹配，避免误伤）
}

// chtKeywords 繁体中文标识关键词（按优先级排序）
var chtKeywords = []string{
	"cht",      // 最标准
	"tc",       // Traditional Chinese
	"繁体",     // 中文标识
	"繁中",     // 简写
	"big5",     // Big5编码
	"繁",       // 单字简写
}

// jpKeywords 日语标识关键词
var jpKeywords = []string{
	"jpn", "jp", "日本語", "raw", "生肉",
}

// enKeywords 英语标识关键词
var enKeywords = []string{
	"eng", "en", "english",
}

// languagePatterns 正则表达式模式（用于提取方括号/圆括号内的语言标识）
var languagePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[([^\]]*(?:chs|cht|sc|tc|简体|繁体|简中|繁中|gb|big5|简|繁)[^\]]*)\]`),
	regexp.MustCompile(`\(([^)]*(?:chs|cht|sc|tc|简体|繁体|简中|繁中|gb|big5|简|繁)[^)]*)\)`),
}

// DetectLanguage 从标题中检测语言类型
// 返回检测到的语言和匹配到的关键词
func DetectLanguage(title string) (LanguageType, string) {
	if title == "" {
		return LangUnknown, ""
	}

	titleLower := strings.ToLower(title)

	// 第一步：从方括号/圆括号中提取可能的语言标识区域
	// 这可以减少误匹配（如 "简单" 被匹配到 "简"）
	langSections := extractLanguageSections(titleLower)

	// 第二步：在语言标识区域内检测
	for _, section := range langSections {
		// 检测简体中文
		for _, kw := range chsKeywords {
			if strings.Contains(section, kw) {
				return LangCHS, kw
			}
		}
		// 检测繁体中文
		for _, kw := range chtKeywords {
			if strings.Contains(section, kw) {
				return LangCHT, kw
			}
		}
	}

	// 第三步：全标题检测（更宽松的匹配）
	// 检测日语
	for _, kw := range jpKeywords {
		if strings.Contains(titleLower, kw) {
			return LangJP, kw
		}
	}
	// 检测英语
	for _, kw := range enKeywords {
		if strings.Contains(titleLower, kw) {
			return LangEN, kw
		}
	}

	// 第四步：特定模式匹配（如 "[简日]" 这种组合标签）
	if isMixedChinese(titleLower) {
		// 组合标签优先认为是简体（通常简体包含繁体字幕）
		if strings.Contains(titleLower, "简") {
			return LangCHS, "mixed"
		}
		if strings.Contains(titleLower, "繁") {
			return LangCHT, "mixed"
		}
	}

	return LangUnknown, ""
}

// extractLanguageSections 从标题中提取可能包含语言标识的段落
func extractLanguageSections(title string) []string {
	var sections []string
	seen := make(map[string]bool)

	// 使用正则提取方括号和圆括号内的内容
	for _, pattern := range languagePatterns {
		matches := pattern.FindAllStringSubmatch(title, -1)
		for _, match := range matches {
			if len(match) > 1 && !seen[match[1]] {
				sections = append(sections, match[1])
				seen[match[1]] = true
			}
		}
	}

	// 如果没有找到特定区域，返回整个标题
	if len(sections) == 0 {
		sections = append(sections, title)
	}

	return sections
}

// isMixedChinese 检查是否为混合语言标签（如 "[简日]", "[繁日]"）
func isMixedChinese(title string) bool {
	mixedPatterns := []string{
		"简日", "繁日", "简繁", "chs+jpn", "cht+jpn",
	}
	for _, pattern := range mixedPatterns {
		if strings.Contains(title, pattern) {
			return true
		}
	}
	return false
}

// ShouldDownload 根据语言偏好决定是否应该下载
// 参数：
//   - preference: 用户设置的语言偏好
//   - itemLang: RSS条目的语言
//   - existingLangs: 该订阅该集数已下载的语言列表
//   - historyStats: 历史下载统计 (chs_count, cht_count)
//
// 返回值：
//   - shouldDownload: 是否应该下载
//   - reason: 决策原因（用于日志）
func ShouldDownload(
	preference LanguagePreference,
	itemLang LanguageType,
	existingLangs []LanguageType,
	historyStats map[LanguageType]int,
) (bool, string) {
	// 如果条目语言未知，总是下载（避免误判）
	if itemLang == LangUnknown {
		return true, "language_unknown"
	}

	// 检查该语言版本是否已存在
	for _, existing := range existingLangs {
		if existing == itemLang {
			return false, "language_already_exists"
		}
	}

	switch preference {
	case LangPrefBoth:
		// 同时保留：只要该语言不存在就下载
		return true, "both_languages_allowed"

	case LangPrefCHS:
		// 简体优先
		if itemLang == LangCHS {
			return true, "chs_preferred"
		}
		// 繁体：检查是否已有简体
		hasCHS := false
		for _, lang := range existingLangs {
			if lang == LangCHS {
				hasCHS = true
				break
			}
		}
		if hasCHS {
			return false, "chs_exists_skip_cht"
		}
		return true, "chs_not_found_fallback_cht"

	case LangPrefCHT:
		// 繁体优先
		if itemLang == LangCHT {
			return true, "cht_preferred"
		}
		// 简体：检查是否已有繁体
		hasCHT := false
		for _, lang := range existingLangs {
			if lang == LangCHT {
				hasCHT = true
				break
			}
		}
		if hasCHT {
			return false, "cht_exists_skip_chs"
		}
		return true, "cht_not_found_fallback_chs"

	case LangPrefAuto:
		// 自动学习：根据历史统计推断偏好
		inferredPref := inferPreferenceFromHistory(historyStats)
		return ShouldDownload(inferredPref, itemLang, existingLangs, historyStats)

	default:
		// 未知偏好，默认下载
		return true, "unknown_preference_default"
	}
}

// inferPreferenceFromHistory 根据历史下载统计推断语言偏好
func inferPreferenceFromHistory(stats map[LanguageType]int) LanguagePreference {
	chsCount := stats[LangCHS]
	chtCount := stats[LangCHT]
	total := chsCount + chtCount

	// 数据不足时，默认简体优先（大多数用户习惯）
	if total < 3 {
		return LangPrefCHS
	}

	// 如果某语言占比 > 80%，优先该语言
	if chsCount > 0 && float64(chsCount)/float64(total) > 0.8 {
		return LangPrefCHS
	}
	if chtCount > 0 && float64(chtCount)/float64(total) > 0.8 {
		return LangPrefCHT
	}

	// 混合偏好，使用 both 模式
	return LangPrefBoth
}

// NormalizeLanguagePreference 规范化语言偏好值
func NormalizeLanguagePreference(pref string) LanguagePreference {
	switch strings.ToLower(pref) {
	case "chs", "sc", "simp", "simplified":
		return LangPrefCHS
	case "cht", "tc", "trad", "traditional":
		return LangPrefCHT
	case "both", "all":
		return LangPrefBoth
	case "auto", "":
		return LangPrefAuto
	default:
		return LangPrefAuto
	}
}

// LanguageTypeToPreference 将语言类型转换为对应的优先设置
func LanguageTypeToPreference(lang LanguageType) LanguagePreference {
	switch lang {
	case LangCHS:
		return LangPrefCHS
	case LangCHT:
		return LangPrefCHT
	default:
		return LangPrefAuto
	}
}
