package downloader

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestRenameServiceGenerateFileName(t *testing.T) {
	tests := []struct {
		name     string
		template string
		ctx      *RenameContext
		expected string
	}{
		{
			name:     "标准媒体库模板",
			template: "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:     "葬送的芙莉莲",
					Season:   1,
					Fansub:   "ANi",
					Language: "CHS",
				},
				Download: &model.Download{
					Episode: 1,
				},
				Extension: ".mp4",
			},
			expected: "葬送的芙莉莲/Season 1/葬送的芙莉莲 S01E01.mp4",
		},
		{
			name:     "带字幕组模板",
			template: "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat} [${fansub}]",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:   "测试番剧",
					Season: 2,
					Fansub: "漫猫字幕组",
				},
				Download: &model.Download{
					Episode: 12,
				},
				Extension: ".mkv",
			},
			expected: "测试番剧/Season 2/测试番剧 S02E12 [漫猫字幕组].mkv",
		},
		{
			name:     "带分辨率模板",
			template: "${title} S${seasonFormat}E${episodeFormat} [${resolution}]",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:   "测试动画",
					Season: 1,
				},
				Download: &model.Download{
					Episode: 5,
				},
				Extension:  ".mp4",
				Resolution: "1080p",
			},
			expected: "测试动画 S01E05 [1080p].mp4",
		},
		{
			name:     "从文件名提取分辨率",
			template: "${title} S${seasonFormat}E${episodeFormat} [${resolution}]",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:   "测试动画",
					Season: 1,
				},
				Download: &model.Download{
					Episode: 5,
				},
				OriginalName: "[字幕组] 测试动画 - 05 [1080p][Baha].mp4",
				Extension:    ".mp4",
				Resolution:   "",
			},
			expected: "测试动画 S01E05 [1080p].mp4",
		},
		{
			name:     "字幕组风格模板",
			template: "[${fansub}] ${title} - ${episodeFormat} [${resolution}]",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:   "番剧名称",
					Season: 1,
					Fansub: "ANi",
				},
				Download: &model.Download{
					Episode: 3,
				},
				Extension:  ".mp4",
				Resolution: "1080p",
			},
			expected: "[ANi] 番剧名称 - 03 [1080p].mp4",
		},
		{
			name:     "带语言模板",
			template: "[${fansub}] ${title} S${seasonFormat}E${episodeFormat} [${language}]",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:     "番剧名称",
					Season:   1,
					Fansub:   "字幕组",
					Language: "CHT",
				},
				Download: &model.Download{
					Episode: 7,
				},
				Extension: ".mkv",
			},
			expected: "[字幕组] 番剧名称 S01E07 [CHT].mkv",
		},
		{
			name:     "简化模板",
			template: "${title} - ${episodeFormat}",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:   "简单番剧",
					Season: 1,
				},
				Download: &model.Download{
					Episode: 9,
				},
				Extension: ".mp4",
			},
			expected: "简单番剧 - 09.mp4",
		},
		{
			name:     "数字格式的season和episode",
			template: "${title}/${season}/${episode}",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:   "测试番剧",
					Season: 3,
				},
				Download: &model.Download{
					Episode: 15,
				},
				Extension: ".mp4",
			},
			expected: "测试番剧/3/15.mp4",
		},
		{
			name:     "无扩展名",
			template: "${title} S${seasonFormat}E${episodeFormat}",
			ctx: &RenameContext{
				Subscription: &model.Subscription{
					Name:   "无扩展名测试",
					Season: 1,
				},
				Download: &model.Download{
					Episode: 1,
				},
				Extension: "",
			},
			expected: "无扩展名测试 S01E01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewRenameService(tt.template)
			result := service.GenerateFileName(tt.ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewRenameServiceDefaultTemplate(t *testing.T) {
	// 测试空模板时使用默认模板
	service := NewRenameService("")
	assert.NotNil(t, service)
	assert.Equal(t, "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}", service.defaultTemplate)

	// 验证默认模板能正常工作
	ctx := &RenameContext{
		Subscription: &model.Subscription{
			Name:   "测试番剧",
			Season: 1,
		},
		Download: &model.Download{
			Episode: 1,
		},
		Extension: ".mp4",
	}
	result := service.GenerateFileName(ctx)
	assert.Equal(t, "测试番剧/Season 1/测试番剧 S01E01.mp4", result)
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
		assert.Contains(t, presets, name, "预设模板 %s 应该存在", name)
	}

	// 验证 media_library 模板内容
	expected := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	assert.Equal(t, expected, presets["media_library"])
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
			name:     "括号不匹配-少闭合",
			template: "${title/Season ${season}",
			wantErr:  true,
		},
		{
			name:     "括号不匹配-多闭合",
			template: "${title}/Season ${season}}",
			wantErr:  true,
		},
		{
			name:     "包含非法字符-冒号",
			template: "${title}:${season}",
			wantErr:  true,
		},
		{
			name:     "包含非法字符-星号",
			template: "${title}*${season}",
			wantErr:  true,
		},
		{
			name:     "包含非法字符-问号",
			template: "${title}?${season}",
			wantErr:  true,
		},
		{
			name:     "包含非法字符-尖括号",
			template: "${title}<${season}",
			wantErr:  true,
		},
		{
			name:     "斜杠是允许的",
			template: "${title}/${season}",
			wantErr:  false,
		},
		{
			name:     "空字符串",
			template: "",
			wantErr:  true,
		},
		{
			name:     "只有变量开头",
			template: "${",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTemplate(tt.template)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetTemplateVariables(t *testing.T) {
	vars := GetTemplateVariables()

	// 验证所有变量都存在
	expectedVars := []string{
		"${title}",
		"${season}",
		"${seasonFormat}",
		"${episode}",
		"${episodeFormat}",
		"${fansub}",
		"${resolution}",
		"${language}",
	}

	for _, v := range expectedVars {
		assert.Contains(t, vars, v, "模板变量 %s 应该存在", v)
		assert.NotEmpty(t, vars[v], "模板变量 %s 的说明不应为空", v)
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
			name:     "480p",
			filename: "[ANi] 番剧 - 01 [480p][AAC].mp4",
			expected: "480p",
		},
		{
			name:     "2160p",
			filename: "[ANi] 番剧 - 01 [2160p][AAC].mp4",
			expected: "2160p",
		},
		{
			name:     "4K",
			filename: "[ANi] 番剧 - 01 [4K][AAC].mkv",
			expected: "4K",
		},
		{
			name:     "UHD",
			filename: "[ANi] 番剧 - 01 [UHD][AAC].mkv",
			expected: "UHD",
		},
		{
			name:     "无分辨率信息",
			filename: "[ANi] 番剧 - 01 [AAC].mp4",
			expected: "Unknown",
		},
		{
			name:     "大小写混合",
			filename: "[ANi] 番剧 - 01 [1080p][Baha].mp4",
			expected: "1080p",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractResolution(tt.filename)
			assert.Equal(t, tt.expected, result)
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
			name:     "包含斜杠",
			input:    "番剧/名称",
			expected: "番剧_名称",
		},
		{
			name:     "包含冒号",
			input:    "Re:从零开始",
			expected: "Re_从零开始",
		},
		{
			name:     "包含星号",
			input:    "番剧*名称",
			expected: "番剧_名称",
		},
		{
			name:     "包含问号",
			input:    "番剧?名称",
			expected: "番剧_名称",
		},
		{
			name:     "包含引号",
			input:    "番剧\"名称\"",
			expected: "番剧_名称_",
		},
		{
			name:     "包含尖括号",
			input:    "番剧<名称>",
			expected: "番剧_名称_",
		},
		{
			name:     "包含管道符",
			input:    "番剧|名称",
			expected: "番剧_名称",
		},
		{
			name:     "多个非法字符",
			input:    "番剧/名称:测试*字符?",
			expected: "番剧_名称_测试_字符_",
		},
		{
			name:     "多余空格",
			input:    "番剧    名称",
			expected: "番剧 名称",
		},
		{
			name:     "开头结尾空格",
			input:    "  番剧名称  ",
			expected: "番剧名称",
		},
		{
			name:     "正常名称",
			input:    "葬送的芙莉莲",
			expected: "葬送的芙莉莲",
		},
		{
			name:     "空字符串",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeFileName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseTemplate(t *testing.T) {
	service := NewRenameService("")

	ctx := &RenameContext{
		Subscription: &model.Subscription{
			Name:     "测试番剧",
			Season:   2,
			Fansub:   "字幕组",
			Language: "CHS",
		},
		Download: &model.Download{
			Episode: 5,
		},
		OriginalName: "[字幕组] 测试番剧 - 05 [1080p].mp4",
		Extension:    ".mp4",
		Resolution:   "1080p",
	}

	// 测试自定义模板解析
	template := "[${fansub}] ${title} S${seasonFormat}E${episodeFormat}"
	result := service.ParseTemplate(template, ctx)
	expected := "[字幕组] 测试番剧 S02E05"
	assert.Equal(t, expected, result)
}

func TestExtractFileInfo(t *testing.T) {
	tests := []struct {
		name     string
		files    []TorrentFile
		expected *FileInfo
	}{
		{
			name:     "空文件列表",
			files:    []TorrentFile{},
			expected: nil,
		},
		{
			name: "只有一个视频文件",
			files: []TorrentFile{
				{Name: "episode01.mkv", Size: 1024 * 1024 * 500},
			},
			expected: &FileInfo{
				Name:       "episode01.mkv",
				Extension:  ".mkv",
				Size:       1024 * 1024 * 500,
				Resolution: "Unknown",
			},
		},
		{
			name: "多个视频文件取最大",
			files: []TorrentFile{
				{Name: "episode01_small.mp4", Size: 1024 * 1024 * 100},
				{Name: "episode01_large.mp4", Size: 1024 * 1024 * 500},
				{Name: "episode01_medium.mp4", Size: 1024 * 1024 * 300},
			},
			expected: &FileInfo{
				Name:       "episode01_large.mp4",
				Extension:  ".mp4",
				Size:       1024 * 1024 * 500,
				Resolution: "Unknown",
			},
		},
		{
			name: "混合文件类型",
			files: []TorrentFile{
				{Name: "readme.txt", Size: 1024},
				{Name: "cover.jpg", Size: 1024 * 100},
				{Name: "episode01.mkv", Size: 1024 * 1024 * 500},
			},
			expected: &FileInfo{
				Name:       "episode01.mkv",
				Extension:  ".mkv",
				Size:       1024 * 1024 * 500,
				Resolution: "Unknown",
			},
		},
		{
			name: "带分辨率信息",
			files: []TorrentFile{
				{Name: "[字幕组] 番剧 - 01 [1080p].mkv", Size: 1024 * 1024 * 1000},
			},
			expected: &FileInfo{
				Name:       "[字幕组] 番剧 - 01 [1080p].mkv",
				Extension:  ".mkv",
				Size:       1024 * 1024 * 1000,
				Resolution: "1080p",
			},
		},
		{
			name: "只有非视频文件",
			files: []TorrentFile{
				{Name: "readme.txt", Size: 1024},
				{Name: "cover.jpg", Size: 1024 * 100},
			},
			expected: nil,
		},
		{
			name: "不同视频格式",
			files: []TorrentFile{
				{Name: "video.avi", Size: 1024 * 1024 * 400},
				{Name: "video.flv", Size: 1024 * 1024 * 300},
				{Name: "video.ts", Size: 1024 * 1024 * 500},
			},
			expected: &FileInfo{
				Name:       "video.ts",
				Extension:  ".ts",
				Size:       1024 * 1024 * 500,
				Resolution: "Unknown",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractFileInfo(tt.files)
			assert.Equal(t, tt.expected, result)
		})
	}
}
