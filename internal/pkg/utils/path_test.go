package utils

import (
	"testing"
)

func TestGenerateDownloadPath(t *testing.T) {
	tests := []struct {
		name      string
		basePath  string
		animeName string
		expected  string
	}{
		{
			name:      "基础路径不包含番剧名",
			basePath:  "/downloads",
			animeName: "葬送的芙莉莲",
			expected:  "/downloads/葬送的芙莉莲",
		},
		{
			name:      "基础路径已包含番剧名",
			basePath:  "/downloads/葬送的芙莉莲",
			animeName: "葬送的芙莉莲",
			expected:  "/downloads/葬送的芙莉莲",
		},
		{
			name:      "基础路径包含清理后的番剧名",
			basePath:  "/downloads/Re_从零开始的异世界生活",
			animeName: "Re:从零开始的异世界生活",
			expected:  "/downloads/Re_从零开始的异世界生活",
		},
		{
			name:      "不同番剧名",
			basePath:  "/downloads/间谍过家家",
			animeName: "咒术回战",
			expected:  "/downloads/间谍过家家/咒术回战",
		},
		{
			name:      "英文番剧名",
			basePath:  "/downloads",
			animeName: "Princess Session Orchestra",
			expected:  "/downloads/Princess Session Orchestra",
		},
		{
			name:      "英文番剧名已存在",
			basePath:  "/downloads/Princess Session Orchestra",
			animeName: "Princess Session Orchestra",
			expected:  "/downloads/Princess Session Orchestra",
		},
		{
			name:      "带特殊字符的番剧名",
			basePath:  "/downloads",
			animeName: "某科学的<超电磁炮>",
			expected:  "/downloads/某科学的_超电磁炮_",
		},
		{
			name:      "混合大小写匹配",
			basePath:  "/downloads/princesssessionorchestra",
			animeName: "Princess Session Orchestra",
			expected:  "/downloads/princesssessionorchestra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateDownloadPath(tt.basePath, tt.animeName)
			if result != tt.expected {
				t.Errorf("GenerateDownloadPath() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeDirectoryName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "正常番剧名",
			input:    "葬送的芙莉莲",
			expected: "葬送的芙莉莲",
		},
		{
			name:     "带冒号",
			input:    "Re:从零开始的异世界生活",
			expected: "Re_从零开始的异世界生活",
		},
		{
			name:     "带尖括号",
			input:    "某科学的<超电磁炮>",
			expected: "某科学的_超电磁炮_",
		},
		{
			name:     "带多个非法字符",
			input:    "番剧名/子目录\\特殊:字符*?",
			expected: "番剧名_子目录_特殊_字符__",
		},
		{
			name:     "多余空格",
			input:    "番剧名   多个空格",
			expected: "番剧名 多个空格",
		},
		{
			name:     "开头结尾的点",
			input:    ".番剧名.",
			expected: "番剧名",
		},
		{
			name:     "空字符串",
			input:    "",
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeDirectoryName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeDirectoryName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsSimilarPath(t *testing.T) {
	tests := []struct {
		name     string
		path1    string
		path2    string
		expected bool
	}{
		{
			name:     "完全相同",
			path1:    "葬送的芙莉莲",
			path2:    "葬送的芙莉莲",
			expected: true,
		},
		{
			name:     "大小写不同",
			path1:    "Princess Session Orchestra",
			path2:    "princess session orchestra",
			expected: true,
		},
		{
			name:     "包含特殊字符",
			path1:    "Re_从零开始的异世界生活",
			path2:    "Re:从零开始的异世界生活",
			expected: true,
		},
		{
			name:     "完全不同",
			path1:    "葬送的芙莉莲",
			path2:    "咒术回战",
			expected: false,
		},
		{
			name:     "部分相似但长度差异大",
			path1:    "番剧",
			path2:    "番剧名称很长的完整标题",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSimilarPath(tt.path1, tt.path2)
			if result != tt.expected {
				t.Errorf("isSimilarPath() = %v, want %v", result, tt.expected)
			}
		})
	}
}
