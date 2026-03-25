package downloader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractEpisodeFromFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected int
	}{
		// 第X集格式
		{
			name:     "第12集格式",
			filename: "番剧名称 第12集.mkv",
			expected: 12,
		},
		{
			name:     "第 12 集格式(带空格)",
			filename: "番剧名称 第 12 集.mkv",
			expected: 12,
		},

		// 话/話格式
		{
			name:     "12话格式",
			filename: "番剧名称 12话.mkv",
			expected: 12,
		},
		{
			name:     "12話格式(繁体)",
			filename: "番剧名称 12話.mkv",
			expected: 12,
		},

		// E/EP格式
		{
			name:     "E12格式",
			filename: "[字幕组] 番剧名称 E12 [1080p].mkv",
			expected: 12,
		},
		{
			name:     "EP12格式",
			filename: "[字幕组] 番剧名称 EP12 [1080p].mkv",
			expected: 12,
		},
		{
			name:     "Ep.12格式",
			filename: "[字幕组] 番剧名称 Ep.12 [1080p].mkv",
			expected: 12,
		},
		{
			name:     "e12小写",
			filename: "[字幕组] 番剧名称 e12 [1080p].mkv",
			expected: 12,
		},

		// Episode格式
		{
			name:     "Episode 12格式",
			filename: "[字幕组] 番剧名称 Episode 12 [1080p].mkv",
			expected: 12,
		},

		// S01E12格式
		{
			name:     "S01E12格式",
			filename: "番剧名称 S01E12.mkv",
			expected: 12,
		},
		{
			name:     "S1E12格式(单位数季)",
			filename: "番剧名称 S1E12.mkv",
			expected: 12,
		},
		{
			name:     "s01e12小写",
			filename: "番剧名称 s01e12.mkv",
			expected: 12,
		},

		// 方括号格式
		{
			name:     "[12]格式",
			filename: "[字幕组] 番剧名称 [12] [1080p].mkv",
			expected: 12,
		},
		{
			name:     "(12)格式",
			filename: "番剧名称 (12).mkv",
			expected: 12,
		},
		{
			name:     "[012]格式(前导零)",
			filename: "番剧名称 [012].mkv",
			expected: 12,
		},

		// 空格/连字符/下划线分隔
		{
			name:     "- 01 -格式",
			filename: "番剧名称 - 01 -.mkv",
			expected: 1,
		},
		{
			name:     "_01_格式",
			filename: "番剧名称_01_.mkv",
			expected: 1,
		},
		{
			name:     "空格01空格格式",
			filename: "番剧名称 01 版本.mkv",
			expected: 1,
		},
		{
			name:     "-01[格式",
			filename: "番剧名称 -01[1080p].mkv",
			expected: 1,
		},
		{
			name:     "- 01.格式",
			filename: "番剧名称 - 01.mkv",
			expected: 1,
		},

		// 以数字结尾
		{
			name:     "以-01结尾",
			filename: "番剧名称 -01",
			expected: 1,
		},
		{
			name:     "以 01结尾",
			filename: "番剧名称 01",
			expected: 1,
		},

		// 合集文件名测试
		{
			name:     "合集多文件-第01集",
			filename: "Season 1/[字幕组] 番剧名称 第01集 [1080p].mp4",
			expected: 1,
		},
		{
			name:     "合集多文件-E12",
			filename: "番剧名称/Season 1/番剧名称 E12 [1080p].mkv",
			expected: 12,
		},
		{
			name:     "合集多文件-S01E05",
			filename: "番剧名称 S01E05 [1080p].mkv",
			expected: 5,
		},
		{
			name:     "合集多文件-[05]",
			filename: "[字幕组] 番剧名称 [05] [1080p].mkv",
			expected: 5,
		},

		// 边界情况
		{
			name:     "第0集应该返回0",
			filename: "番剧名称 第0集.mkv",
			expected: 0,
		},
		{
			name:     "超过999集应该返回0",
			filename: "番剧名称 第1000集.mkv",
			expected: 0,
		},
		{
			name:     "三位数集数正常返回",
			filename: "番剧名称 第999集.mkv",
			expected: 999,
		},
		{
			name:     "无集数信息",
			filename: "[字幕组] 番剧名称 [1080p].mkv",
			expected: 0,
		},
		{
			name:     "空字符串",
			filename: "",
			expected: 0,
		},
		{
			name:     "单个数字不是集数",
			filename: "2024年作品.mkv",
			expected: 0,
		},
		{
			name:     "年份后的集数",
			filename: "[字幕组] 番剧 2024 第05集 [1080p].mkv",
			expected: 5,
		},
		{
			name:     "文件扩展名前的数字",
			filename: "番剧名称.12.mkv",
			expected: 0,
		},
		{
			name:     "多位数字但不是集数格式",
			filename: "番剧名称 20241012.mkv",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractEpisodeFromFilename(tt.filename)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractEpisodeFromFilenameCollection(t *testing.T) {
	// 专门针对合集文件名解析的测试
	tests := []struct {
		name     string
		filename string
		expected int
	}{
		{
			name:     "合集-S01E01到S01E12",
			filename: "[字幕组] 番剧合集 S01E01-E12 [1080p]/[字幕组] 番剧 S01E01 [1080p].mkv",
			expected: 1,
		},
		{
			name:     "合集-多季度S02E05",
			filename: "番剧 Season 2/[字幕组] 番剧 S02E05 [1080p].mkv",
			expected: 5,
		},
		{
			name:     "合集-嵌套目录结构",
			filename: "Anime/番剧名称/Season 1/[字幕组] 第03集 [1080p].mkv",
			expected: 3,
		},
		{
			name:     "合集-简洁命名",
			filename: "E01.mkv",
			expected: 1,
		},
		{
			name:     "合集-集数在目录名",
			filename: "第12集/video.mkv",
			expected: 12,
		},
		{
			name:     "合集-EP前缀",
			filename: "番剧 EP01 [1080p].mp4",
			expected: 1,
		},
		{
			name:     "合集-BD命名",
			filename: "[BD][1080p] 番剧 - 01 [字幕组].mkv",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractEpisodeFromFilename(tt.filename)
			assert.Equal(t, tt.expected, result)
		})
	}
}
