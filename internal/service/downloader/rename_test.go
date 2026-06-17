package downloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
)

type stubConfigRepo struct {
	values map[string]string
}

func (s *stubConfigRepo) Get(key string) (*model.Config, error) {
	if value, ok := s.values[key]; ok {
		return &model.Config{Key: key, Value: value}, nil
	}
	return nil, nil
}

func (s *stubConfigRepo) GetCached(key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", nil
}

func (s *stubConfigRepo) Set(key, value string) error {
	s.values[key] = value
	return nil
}

func (s *stubConfigRepo) Delete(key string) error {
	delete(s.values, key)
	return nil
}

func (s *stubConfigRepo) GetAll() ([]model.Config, error) {
	configs := make([]model.Config, 0, len(s.values))
	for key, value := range s.values {
		configs = append(configs, model.Config{Key: key, Value: value})
	}
	return configs, nil
}

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
		{
			name:     "默认媒体库模板清理中文季号标题",
			template: "",
			ctx: &RenameContext{
				Download: &model.Download{
					Episode: 1,
				},
				Subscription: &model.Subscription{
					Name:   "入间同学入魔了 第四季",
					Season: 4,
				},
				Extension: ".mp4",
			},
			expected: "入间同学入魔了/Season 4/入间同学入魔了 S04E01.mp4",
		},
		{
			name:     "默认媒体库模板清理日文系列号标题",
			template: "",
			ctx: &RenameContext{
				Download: &model.Download{
					Episode: 1,
				},
				Subscription: &model.Subscription{
					Name:   "魔入りました！入間くん 第4シリーズ",
					Season: 4,
				},
				Extension: ".mkv",
			},
			expected: "魔入りました！入間くん/Season 4/魔入りました！入間くん S04E01.mkv",
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

func TestReorganizeSubscriptionFilesGeneratesNFOInShowRoot(t *testing.T) {
	tests := []struct {
		name             string
		template         string
		wantMovedTo      string
		wantRenamedTo    string
		wantRenamedCount int
	}{
		{
			name:             "default nested template",
			wantMovedTo:      filepath.Join("Test Anime", "Season 1"),
			wantRenamedTo:    "Test Anime S01E01.mkv",
			wantRenamedCount: 1,
		},
		{
			name:             "flat simple preset",
			template:         GetPresetTemplates()["simple"],
			wantMovedTo:      ".",
			wantRenamedTo:    "Test Anime - 01.mkv",
			wantRenamedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			basePath := t.TempDir()
			configRepo := &stubConfigRepo{values: map[string]string{}}
			if tt.template != "" {
				configRepo.values["rename_template"] = tt.template
			}

			var movedTo string
			var renamedTo string
			qbClient := &mockQBClient{
				getTorrentInfoFunc: func(hash string) (*TorrentInfo, error) {
					return &TorrentInfo{Hash: hash, SavePath: filepath.Join(basePath, "incoming")}, nil
				},
				getTorrentFilesFunc: func(hash string) ([]TorrentFile, error) {
					return []TorrentFile{{Name: "episode01.mkv", Size: 1000000}}, nil
				},
				setLocationFunc: func(hash, location string) error {
					movedTo = location
					return nil
				},
				renameFileFunc: func(hash, oldName, newName string) error {
					renamedTo = newName
					return nil
				},
			}

			service := NewRenameService("")
			result, err := service.ReorganizeSubscriptionFiles(
				context.Background(),
				testRenameSubscription(),
				[]model.Download{{ID: 1, TorrentHash: "hash-1", Episode: 1}},
				qbClient,
				configRepo,
				basePath,
			)
			if err != nil {
				t.Fatalf("ReorganizeSubscriptionFiles() error = %v", err)
			}

			wantMovedPath := filepath.Join(basePath, tt.wantMovedTo)
			if movedTo != wantMovedPath {
				t.Fatalf("SetLocation() path = %q, want %q", movedTo, wantMovedPath)
			}
			if renamedTo != tt.wantRenamedTo {
				t.Fatalf("RenameTorrentFile() path = %q, want %q", renamedTo, tt.wantRenamedTo)
			}
			if result["renamed"] != tt.wantRenamedCount {
				t.Fatalf("renamed count = %v, want %d", result["renamed"], tt.wantRenamedCount)
			}

			assertGeneratedShowRootNFO(t, basePath)
		})
	}
}

func TestRenameSubscriptionFilesGeneratesNFOInShowRoot(t *testing.T) {
	tests := []struct {
		name            string
		template        string
		wantMovedTo     string
		wantRenamedPath string
	}{
		{
			name:            "default nested template",
			wantMovedTo:     filepath.Join("Test Anime", "Season 1"),
			wantRenamedPath: filepath.Join("Test Anime", "Season 1", "Test Anime S01E01.mkv"),
		},
		{
			name:            "flat fansub preset",
			template:        GetPresetTemplates()["fansub_style"],
			wantMovedTo:     ".",
			wantRenamedPath: "[ANi] Test Anime - 01 [1080p].mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			basePath := t.TempDir()
			configRepo := &stubConfigRepo{values: map[string]string{}}
			if tt.template != "" {
				configRepo.values["rename_template"] = tt.template
			}

			var movedTo string
			var updatedRenamedPath string
			qbClient := &mockQBClient{
				getTorrentInfoFunc: func(hash string) (*TorrentInfo, error) {
					return &TorrentInfo{Hash: hash, SavePath: filepath.Join(basePath, "incoming")}, nil
				},
				getTorrentFilesFunc: func(hash string) ([]TorrentFile, error) {
					return []TorrentFile{{Name: "episode01.1080p.mkv", Size: 1000000}}, nil
				},
				setLocationFunc: func(hash, location string) error {
					movedTo = location
					return nil
				},
			}
			downloadRepo := &mockDownloadRepo{
				updateFunc: func(download *model.Download) error {
					updatedRenamedPath = download.RenamedPath
					return nil
				},
			}

			service := NewRenameService("")
			_, err := service.RenameSubscriptionFiles(
				context.Background(),
				testRenameSubscription(),
				[]model.Download{{ID: 1, TorrentHash: "hash-1", Episode: 1}},
				qbClient,
				configRepo,
				downloadRepo,
				basePath,
			)
			if err != nil {
				t.Fatalf("RenameSubscriptionFiles() error = %v", err)
			}

			wantMovedPath := filepath.Join(basePath, tt.wantMovedTo)
			if movedTo != wantMovedPath {
				t.Fatalf("SetLocation() path = %q, want %q", movedTo, wantMovedPath)
			}

			wantRenamedPath := filepath.Join(basePath, tt.wantRenamedPath)
			if updatedRenamedPath != wantRenamedPath {
				t.Fatalf("download.RenamedPath = %q, want %q", updatedRenamedPath, wantRenamedPath)
			}

			assertGeneratedShowRootNFO(t, basePath)
		})
	}
}

func testRenameSubscription() *model.Subscription {
	return &model.Subscription{
		ID:        1,
		Name:      "Test Anime",
		Season:    1,
		Fansub:    "ANi",
		BangumiID: 12345,
	}
}

func assertGeneratedShowRootNFO(t *testing.T, basePath string) {
	t.Helper()

	baseNFOPath := filepath.Join(basePath, tvShowNFOFileName)
	if _, err := os.Stat(baseNFOPath); !os.IsNotExist(err) {
		t.Fatalf("tvshow.nfo should not be created at media-library base path, stat err = %v", err)
	}

	showNFOPath := filepath.Join(basePath, "Test Anime", tvShowNFOFileName)
	content := readFileString(t, showNFOPath)
	if !strings.Contains(content, "<bangumiid>12345</bangumiid>") {
		t.Fatalf("tvshow.nfo missing bangumi id: %s", content)
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
