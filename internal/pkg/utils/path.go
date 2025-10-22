package utils

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// GenerateDownloadPath 生成下载路径（包含番剧名子目录）
// 智能检测：如果 basePath 已经包含番剧名，则不再重复添加
// basePath: 基础下载路径，如 /downloads 或 /downloads/番剧名
// animeName: 番剧名称
// 返回: /downloads/番剧名 或 /downloads/番剧名（如果已存在）
func GenerateDownloadPath(basePath, animeName string) string {
	// 清理番剧名中的非法字符
	cleanName := SanitizeDirectoryName(animeName)

	// 获取 basePath 的最后一个目录名
	baseDir := filepath.Base(basePath)

	// 检查最后一个目录名是否和番剧名匹配
	if isSimilarPath(baseDir, cleanName) {
		// 已经包含番剧名，直接返回
		return basePath
	}

	// 不包含番剧名，添加子目录
	return filepath.Join(basePath, cleanName)
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
	// 转小写
	path = strings.ToLower(path)

	// 只保留字母、数字和中文字符
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
