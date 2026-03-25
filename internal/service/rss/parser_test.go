package rss

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParserExtractEpisode(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name     string
		title    string
		expected int
	}{
		// 第X集格式
		{
			name:     "第12集格式",
			title:    "[字幕组] 番剧名称 第12集 [1080p]",
			expected: 12,
		},
		{
			name:     "第 12 集格式(带空格)",
			title:    "[字幕组] 番剧名称 第 12 集 [1080p]",
			expected: 12,
		},

		// 话/話格式
		{
			name:     "12话格式",
			title:    "[字幕组] 番剧名称 12话 [1080p]",
			expected: 12,
		},
		{
			name:     "12話格式(繁体)",
			title:    "[字幕组] 番剧名称 12話 [1080p]",
			expected: 12,
		},

		// E/EP格式
		{
			name:     "E12格式",
			title:    "[字幕组] 番剧名称 E12 [1080p]",
			expected: 12,
		},
		{
			name:     "EP12格式",
			title:    "[字幕组] 番剧名称 EP12 [1080p]",
			expected: 12,
		},
		{
			name:     "Ep.12格式",
			title:    "[字幕组] 番剧名称 Ep.12 [1080p]",
			expected: 12,
		},
		{
			name:     "e12小写",
			title:    "[字幕组] 番剧名称 e12 [1080p]",
			expected: 12,
		},

		// Episode格式
		{
			name:     "Episode 12格式",
			title:    "[字幕组] 番剧名称 Episode 12 [1080p]",
			expected: 12,
		},

		// 方括号格式
		{
			name:     "[12]格式",
			title:    "[字幕组] 番剧名称 [12] [1080p]",
			expected: 12,
		},
		{
			name:     "[ 12 ]格式(带空格)",
			title:    "[字幕组] 番剧名称 [ 12 ] [1080p]",
			expected: 12,
		},

		// S01E12格式
		{
			name:     "S01E12格式",
			title:    "[字幕组] 番剧名称 S01E12 [1080p]",
			expected: 12,
		},
		{
			name:     "S1E12格式(单位数季)",
			title:    "[字幕组] 番剧名称 S1E12 [1080p]",
			expected: 12,
		},

		// - 数字格式
		{
			name:     "- 12格式",
			title:    "[字幕组] 番剧名称 - 12 [1080p]",
			expected: 12,
		},
		{
			name:     "- 12 v2格式(带版本)",
			title:    "[字幕组] 番剧名称 - 12 v2 [1080p]",
			expected: 12,
		},
		{
			name:     "- 12 [1080p]格式",
			title:    "[字幕组] 番剧名称 - 12 [1080p]",
			expected: 12,
		},
		{
			name:     "- 176 (1080p)格式",
			title:    "[字幕组] 番剧名称 - 176 (1080p)",
			expected: 176,
		},

		// 边界情况
		{
			name:     "第0集应该返回0",
			title:    "[字幕组] 番剧名称 第0集 [1080p]",
			expected: 0,
		},
		{
			name:     "超过999集应该返回0",
			title:    "[字幕组] 番剧名称 第1000集 [1080p]",
			expected: 0,
		},
		{
			name:     "无集数信息",
			title:    "[字幕组] 番剧名称 [1080p]",
			expected: 0,
		},
		{
			name:     "空字符串",
			title:    "",
			expected: 0,
		},
		{
			name:     "只有数字但不是集数格式",
			title:    "2024年10月12日",
			expected: 0,
		},
		{
			name:     "集数在年份后面",
			title:    "[字幕组] 番剧名称 2024 - 05 [1080p]",
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.ExtractEpisode(tt.title)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParserExtractFansub(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name     string
		title    string
		expected string
	}{
		{
			name:     "标准字幕组格式",
			title:    "[ANi] 番剧名称 - 01 [1080p]",
			expected: "ANi",
		},
		{
			name:     "带空格的字幕组",
			title:    "[漫猫字幕组] 番剧名称 - 01 [1080p]",
			expected: "漫猫字幕组",
		},
		{
			name:     "繁体字幕组名",
			title:    "[諸神字幕組] 番剧名称 - 01 [1080p]",
			expected: "諸神字幕組",
		},
		{
			name:     "无字幕组标识",
			title:    "番剧名称 - 01 [1080p]",
			expected: "",
		},
		{
			name:     "方括号在中间",
			title:    "番剧名称 [1080p] [字幕组]",
			expected: "",
		},
		{
			name:     "空字符串",
			title:    "",
			expected: "",
		},
		{
			name:     "只有方括号",
			title:    "[] 番剧名称 - 01",
			expected: "",
		},
		{
			name:     "多层方括号取第一个",
			title:    "[字幕组][1080p] 番剧名称 - 01",
			expected: "字幕组",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.ExtractFansub(tt.title)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractInfoHashFromExtensions(t *testing.T) {
	tests := []struct {
		name       string
		extensions map[string]map[string][]struct {
			Name  string
			Value string
	}
		expected string
	}{
		{
			name:       "空extensions",
			extensions: nil,
			expected:   "",
		},
		{
			name:       "空map",
			extensions: map[string]map[string][]struct{ Name, Value string }{},
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 这个测试主要验证空输入不会panic
			result := extractInfoHashFromExtensions(tt.extensions)
			assert.Equal(t, tt.expected, result)
		})
	}
}
