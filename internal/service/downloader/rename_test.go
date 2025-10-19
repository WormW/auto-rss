package downloader

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
)

func TestRenameServiceGenerateFileName(t *testing.T) {
	tests := []struct {
		name     string
		template string
		ctx      *RenameContext
		expected string
	}{
		{
			name:     "标准模板 - 单季番剧",
			template: "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}",
			ctx: &RenameContext{
				Download: &model.Download{
					Title:   "[ANi] Sōsō no Frieren / 葬送的芙莉莲 - 01 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]",
					Episode: 1,
					Fansub:  "ANi",
				},
				Subscription: &model.Subscription{
					Name:   "葬送的芙莉莲",
					Season: 1,
					Fansub: "ANi",
				},
				Extension: ".mp4",
			},
			expected: "葬送的芙莉莲/Season 1/葬送的芙莉莲 S01E01.mp4",
		},
		{
			name:     "标准模板 - 第二季",
			template: "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}",
			ctx: &RenameContext{
				Download: &model.Download{
					Title:   "[ANi] 番剧名称 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]",
					Episode: 12,
					Fansub:  "ANi",
				},
				Subscription: &model.Subscription{
					Name:   "番剧名称",
					Season: 2,
					Fansub: "ANi",
				},
				Extension: ".mkv",
			},
			expected: "番剧名称/Season 2/番剧名称 S02E12.mkv",
		},
		{
			name:     "简化模板",
			template: "${title}/S${seasonFormat}E${episodeFormat}",
			ctx: &RenameContext{
				Download: &model.Download{
					Episode: 5,
				},
				Subscription: &model.Subscription{
					Name:   "测试番剧",
					Season: 1,
				},
				Extension: ".mp4",
			},
			expected: "测试番剧/S01E05.mp4",
		},
		{
			name:     "包含字幕组的模板",
			template: "${title}/[${fansub}]${title} S${seasonFormat}E${episodeFormat}",
			ctx: &RenameContext{
				Download: &model.Download{
					Episode: 3,
					Fansub:  "ANi",
				},
				Subscription: &model.Subscription{
					Name:   "献祭公主与兽王",
					Season: 1,
					Fansub: "ANi",
				},
				Extension: ".mp4",
			},
			expected: "献祭公主与兽王/[ANi]献祭公主与兽王 S01E03.mp4",
		},
		{
			name:     "带分辨率模板",
			template: "${title} S${seasonFormat}E${episodeFormat} [${resolution}]",
			ctx: &RenameContext{
				Download: &model.Download{
					Episode: 8,
				},
				Subscription: &model.Subscription{
					Name:   "测试动画",
					Season: 1,
				},
				Extension:  ".mp4",
				Resolution: "1080p",
			},
			expected: "测试动画 S01E08 [1080p].mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewRenameService(tt.template)
			result := service.GenerateFileName(tt.ctx)
			if result != tt.expected {
				t.Errorf("GenerateFileName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetPresetTemplates(t *testing.T) {
	presets := GetPresetTemplates()

	// 验证所有预设模板都存在
	requiredPresets := []string{
		"media_library",
		"media_library_fansub",
		"media_library_full",
		"simple",
		"fansub_style",
		"detailed",
	}

	for _, name := range requiredPresets {
		if _, ok := presets[name]; !ok {
			t.Errorf("预设模板 %s 不存在", name)
		}
	}

	// 验证 media_library 模板内容
	expected := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	if presets["media_library"] != expected {
		t.Errorf("media_library 模板 = %v, want %v", presets["media_library"], expected)
	}
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		wantErr  bool
	}{
		{
			name:     "有效模板",
			template: "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}",
			wantErr:  false,
		},
		{
			name:     "无变量",
			template: "固定文件名.mp4",
			wantErr:  true,
		},
		{
			name:     "括号不匹配",
			template: "${title/Season ${season",
			wantErr:  true,
		},
		{
			name:     "包含非法字符 - 冒号",
			template: "${title}:${season}",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTemplate(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTemplate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractResolution(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected string
	}{
		{
			name:     "1080p",
			filename: "[ANi] 番剧 - 01 [1080P][AAC].mp4",
			expected: "1080p",
		},
		{
			name:     "720p",
			filename: "[ANi] 番剧 - 01 [720P][AAC].mp4",
			expected: "720p",
		},
		{
			name:     "4K",
			filename: "[ANi] 番剧 - 01 [4K][AAC].mkv",
			expected: "4K",
		},
		{
			name:     "无分辨率信息",
			filename: "[ANi] 番剧 - 01 [AAC].mp4",
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractResolution(tt.filename)
			if result != tt.expected {
				t.Errorf("extractResolution() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "包含非法字符",
			input:    "番剧/名称:测试",
			expected: "番剧_名称_测试",
		},
		{
			name:     "多余空格",
			input:    "番剧    名称",
			expected: "番剧 名称",
		},
		{
			name:     "正常名称",
			input:    "葬送的芙莉莲",
			expected: "葬送的芙莉莲",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeFileName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeFileName() = %v, want %v", result, tt.expected)
			}
		})
	}
}
