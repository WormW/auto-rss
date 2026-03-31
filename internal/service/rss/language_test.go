package rss

import (
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name         string
		title        string
		wantLang     LanguageType
		wantKeyword  string
	}{
		// 简体中文测试
		{
			name:        "标准CHS标签",
			title:       "[字幕组] 葬送的芙莉莲 - 12 [1080P][CHS][MP4]",
			wantLang:    LangCHS,
			wantKeyword: "chs",
		},
		{
			name:        "SC标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][SC]",
			wantLang:    LangCHS,
			wantKeyword: "sc",
		},
		{
			name:        "简体标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][简体]",
			wantLang:    LangCHS,
			wantKeyword: "简体",
		},
		{
			name:        "简中标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][简中]",
			wantLang:    LangCHS,
			wantKeyword: "简中",
		},
		{
			name:        "GB标签",
			title:       "[字幕组] 芙莉莲 - 01 [GB][1080P]",
			wantLang:    LangCHS,
			wantKeyword: "gb",
		},
		{
			name:        "单字简标签",
			title:       "[字幕组] 芙莉莲 - 01 [简][1080P]",
			wantLang:    LangCHS,
			wantKeyword: "简",
		},
		// 繁体中文测试
		{
			name:        "标准CHT标签",
			title:       "[字幕组] 葬送的芙莉莲 - 12 [1080P][CHT][MP4]",
			wantLang:    LangCHT,
			wantKeyword: "cht",
		},
		{
			name:        "TC标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][TC]",
			wantLang:    LangCHT,
			wantKeyword: "tc",
		},
		{
			name:        "繁体标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][繁体]",
			wantLang:    LangCHT,
			wantKeyword: "繁体",
		},
		{
			name:        "繁中标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][繁中]",
			wantLang:    LangCHT,
			wantKeyword: "繁中",
		},
		{
			name:        "Big5标签",
			title:       "[字幕组] 芙莉莲 - 01 [Big5][1080P]",
			wantLang:    LangCHT,
			wantKeyword: "big5",
		},
		{
			name:        "单字繁标签",
			title:       "[字幕组] 芙莉莲 - 01 [繁][1080P]",
			wantLang:    LangCHT,
			wantKeyword: "繁",
		},
		// 日语/生肉测试
		{
			name:        "JPN标签",
			title:       "[Raw] 芙莉莲 - 01 [1080P][JPN]",
			wantLang:    LangJP,
			wantKeyword: "jpn",
		},
		{
			name:        "Raw标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][RAW]",
			wantLang:    LangJP,
			wantKeyword: "raw",
		},
		{
			name:        "生肉标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][生肉]",
			wantLang:    LangJP,
			wantKeyword: "生肉",
		},
		// 未知语言
		{
			name:        "无语言标签",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][MP4]",
			wantLang:    LangUnknown,
			wantKeyword: "",
		},
		{
			name:        "空标题",
			title:       "",
			wantLang:    LangUnknown,
			wantKeyword: "",
		},
		// 混合标签（应该优先识别为中文）
		{
			name:        "简日混合",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][简日]",
			wantLang:    LangCHS,
			wantKeyword: "mixed",
		},
		{
			name:        "繁日混合",
			title:       "[字幕组] 芙莉莲 - 01 [1080P][繁日]",
			wantLang:    LangCHT,
			wantKeyword: "mixed",
		},
		// 大小写测试
		{
			name:        "大写CHS",
			title:       "[字幕组] 芙莉莲 - 01 [CHS]",
			wantLang:    LangCHS,
			wantKeyword: "chs",
		},
		{
			name:        "大小写混合ChS",
			title:       "[字幕组] 芙莉莲 - 01 [ChS]",
			wantLang:    LangCHS,
			wantKeyword: "chs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLang, gotKeyword := DetectLanguage(tt.title)
			if gotLang != tt.wantLang {
				t.Errorf("DetectLanguage() gotLang = %v, want %v", gotLang, tt.wantLang)
			}
			// 对于非mixed结果，检查关键词
			if tt.wantKeyword != "" && tt.wantKeyword != "mixed" {
				// 关键词可能是大小写变化的，转小写比较
				if gotKeyword != tt.wantKeyword {
					t.Errorf("DetectLanguage() gotKeyword = %v, want %v", gotKeyword, tt.wantKeyword)
				}
			}
		})
	}
}

func TestShouldDownload(t *testing.T) {
	tests := []struct {
		name         string
		preference   LanguagePreference
		itemLang     LanguageType
		existingLang []LanguageType
		historyStats map[LanguageType]int
		want         bool
		wantReason   string
	}{
		// CHS 优先策略
		{
			name:         "CHS优先_简体条目_无现有",
			preference:   LangPrefCHS,
			itemLang:     LangCHS,
			existingLang: []LanguageType{},
			want:         true,
			wantReason:   "chs_preferred",
		},
		{
			name:         "CHS优先_繁体条目_无现有简体",
			preference:   LangPrefCHS,
			itemLang:     LangCHT,
			existingLang: []LanguageType{},
			want:         true,
			wantReason:   "chs_not_found_fallback_cht",
		},
		{
			name:         "CHS优先_繁体条目_已有简体",
			preference:   LangPrefCHS,
			itemLang:     LangCHT,
			existingLang: []LanguageType{LangCHS},
			want:         false,
			wantReason:   "chs_exists_skip_cht",
		},
		{
			name:         "CHS优先_简体条目_已有繁体",
			preference:   LangPrefCHS,
			itemLang:     LangCHS,
			existingLang: []LanguageType{LangCHT},
			want:         true,
			wantReason:   "chs_preferred",
		},
		// CHT 优先策略
		{
			name:         "CHT优先_繁体条目_无现有",
			preference:   LangPrefCHT,
			itemLang:     LangCHT,
			existingLang: []LanguageType{},
			want:         true,
			wantReason:   "cht_preferred",
		},
		{
			name:         "CHT优先_简体条目_已有繁体",
			preference:   LangPrefCHT,
			itemLang:     LangCHS,
			existingLang: []LanguageType{LangCHT},
			want:         false,
			wantReason:   "cht_exists_skip_chs",
		},
		// Both 策略
		{
			name:         "Both策略_简体_无现有",
			preference:   LangPrefBoth,
			itemLang:     LangCHS,
			existingLang: []LanguageType{},
			want:         true,
			wantReason:   "both_languages_allowed",
		},
		{
			name:         "Both策略_繁体_已有简体",
			preference:   LangPrefBoth,
			itemLang:     LangCHT,
			existingLang: []LanguageType{LangCHS},
			want:         true,
			wantReason:   "both_languages_allowed",
		},
		{
			name:         "Both策略_简体_已有简体",
			preference:   LangPrefBoth,
			itemLang:     LangCHS,
			existingLang: []LanguageType{LangCHS},
			want:         false,
			wantReason:   "language_already_exists",
		},
		// 未知语言
		{
			name:         "未知语言_总是下载",
			preference:   LangPrefCHS,
			itemLang:     LangUnknown,
			existingLang: []LanguageType{},
			want:         true,
			wantReason:   "language_unknown",
		},
		// Auto 策略（基于历史统计）
		{
			name:         "Auto策略_历史偏好简体",
			preference:   LangPrefAuto,
			itemLang:     LangCHS,
			existingLang: []LanguageType{},
			historyStats: map[LanguageType]int{
				LangCHS: 10,
				LangCHT: 1,
			},
			want:       true,
			wantReason: "chs_preferred", // 推断为CHS优先
		},
		{
			name:         "Auto策略_历史偏好繁体",
			preference:   LangPrefAuto,
			itemLang:     LangCHT,
			existingLang: []LanguageType{},
			historyStats: map[LanguageType]int{
				LangCHS: 1,
				LangCHT: 10,
			},
			want:       true,
			wantReason: "cht_preferred", // 推断为CHT优先
		},
		{
			name:         "Auto策略_数据不足",
			preference:   LangPrefAuto,
			itemLang:     LangCHS,
			existingLang: []LanguageType{},
			historyStats: map[LanguageType]int{
				LangCHS: 1,
			},
			want:       true,
			wantReason: "chs_preferred", // 数据不足，默认CHS
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotReason := ShouldDownload(tt.preference, tt.itemLang, tt.existingLang, tt.historyStats)
			if got != tt.want {
				t.Errorf("ShouldDownload() = %v, want %v", got, tt.want)
			}
			if gotReason != tt.wantReason {
				t.Errorf("ShouldDownload() reason = %v, want %v", gotReason, tt.wantReason)
			}
		})
	}
}

func TestNormalizeLanguagePreference(t *testing.T) {
	tests := []struct {
		input string
		want  LanguagePreference
	}{
		{"auto", LangPrefAuto},
		{"AUTO", LangPrefAuto},
		{"", LangPrefAuto},
		{"chs", LangPrefCHS},
		{"CHS", LangPrefCHS},
		{"sc", LangPrefCHS},
		{"simplified", LangPrefCHS},
		{"cht", LangPrefCHT},
		{"CHT", LangPrefCHT},
		{"tc", LangPrefCHT},
		{"traditional", LangPrefCHT},
		{"both", LangPrefBoth},
		{"all", LangPrefBoth},
		{"unknown", LangPrefAuto}, // 未知值默认auto
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeLanguagePreference(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeLanguagePreference(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestInferPreferenceFromHistory(t *testing.T) {
	tests := []struct {
		name  string
		stats map[LanguageType]int
		want  LanguagePreference
	}{
		{
			name:  "无数据",
			stats: map[LanguageType]int{},
			want:  LangPrefCHS, // 默认简体
		},
		{
			name: "数据不足",
			stats: map[LanguageType]int{
				LangCHS: 2,
			},
			want: LangPrefCHS,
		},
		{
			name: "明显简体偏好",
			stats: map[LanguageType]int{
				LangCHS: 15,
				LangCHT: 2,
			},
			want: LangPrefCHS,
		},
		{
			name: "明显繁体偏好",
			stats: map[LanguageType]int{
				LangCHS: 2,
				LangCHT: 15,
			},
			want: LangPrefCHT,
		},
		{
			name: "混合偏好",
			stats: map[LanguageType]int{
				LangCHS: 10,
				LangCHT: 10,
			},
			want: LangPrefBoth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferPreferenceFromHistory(tt.stats)
			if got != tt.want {
				t.Errorf("inferPreferenceFromHistory() = %v, want %v", got, tt.want)
			}
		})
	}
}
