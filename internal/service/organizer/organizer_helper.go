package organizer

import (
	"strings"
	"unicode"
)

// sanitizeDirectoryName 清理目录名（移除非法字符）
func sanitizeDirectoryName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "*", "_")
	name = strings.ReplaceAll(name, "?", "_")
	name = strings.ReplaceAll(name, "\"", "_")
	name = strings.ReplaceAll(name, "<", "_")
	name = strings.ReplaceAll(name, ">", "_")
	name = strings.ReplaceAll(name, "|", "_")
	return strings.TrimSpace(name)
}

// isSimilarDirectoryName 检查两个目录名是否相似（用于避免重复）
func isSimilarDirectoryName(name1, name2 string) bool {
	normalize := func(s string) string {
		s = strings.ToLower(s)
		var result strings.Builder
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				result.WriteRune(r)
			}
		}
		return result.String()
	}

	normalized1 := normalize(name1)
	normalized2 := normalize(name2)

	if normalized1 == normalized2 {
		return true
	}

	maxLen := len(normalized1)
	if len(normalized2) > maxLen {
		maxLen = len(normalized2)
	}
	if maxLen == 0 {
		return false
	}

	lenDiff := float64(abs(len(normalized1)-len(normalized2))) / float64(maxLen)
	if lenDiff > 0.3 {
		return false
	}

	return strings.Contains(normalized1, normalized2) || strings.Contains(normalized2, normalized1)
}

// abs 返回整数的绝对值
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
