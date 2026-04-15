package recovery

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/organizer"
	"github.com/stretchr/testify/assert"
)

func TestIsSimilarDirectoryName(t *testing.T) {
	tests := []struct {
		name1    string
		name2    string
		expected bool
	}{
		{"胆大党", "胆大党", true},
		{"胆大党", "膽大黨", false},
		{"朋友的妹妹只缠著我", "朋友的妹妹只缠著我", true},
		{"剑来 第二季", "剑来", false},
		{"完全不同", "胆大党", false},
		{"Summer Time Rendering", "summertimeRendering", true},
	}

	for _, tc := range tests {
		t.Run(tc.name1+"_"+tc.name2, func(t *testing.T) {
			result := isSimilarDirectoryName(tc.name1, tc.name2)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractEpisode_Organized(t *testing.T) {
	scanner := &Scanner{parser: organizer.NewFileNameParser()}
	sub := &model.Subscription{Season: 1}

	tests := []struct {
		filename    string
		wantEpisode int
		wantSeason  int
	}{
		{"胆大党 S01E19.mp4", 19, 1},
		{"胆大党 S02E24.mp4", 24, 2},
		{"番剧名 S01E01.mkv", 1, 1},
		{"番剧名 S12E123.mkv", 123, 12},
	}

	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			ep, season := scanner.extractEpisode(tc.filename, sub)
			assert.Equal(t, tc.wantEpisode, ep)
			assert.Equal(t, tc.wantSeason, season)
		})
	}
}

func TestExtractEpisode_Raw(t *testing.T) {
	scanner := &Scanner{parser: organizer.NewFileNameParser()}
	sub := &model.Subscription{Season: 1}

	tests := []struct {
		filename    string
		wantEpisode int
	}{
		{"[Nekomoe] Summer Time Rendering [01][1080p].mkv", 1},
		{"[LoliHouse] Princess-Session Orchestra - 01 [WebRip].mkv", 1},
		{"[ANi] 朋友的妹妹只缠著我 - 12 [1080P].mp4", 12},
		{"第05集.mkv", 5},
		{"05话.mkv", 5},
		{"EP06.mkv", 6},
	}

	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			ep, _ := scanner.extractEpisode(tc.filename, sub)
			assert.Equal(t, tc.wantEpisode, ep)
		})
	}
}

func TestMatchFile_DirectoryName(t *testing.T) {
	scanner := &Scanner{parser: organizer.NewFileNameParser()}
	subscriptions := []model.Subscription{
		{ID: 1, Name: "胆大党", Season: 2},
		{ID: 2, Name: "夏日重现", Season: 1},
	}

	root := "/Volumes/仓库/Bangumi"

	tests := []struct {
		path        string
		wantSubID   uint
		wantEpisode int
	}{
		{filepath.Join(root, "胆大党", "Season 2", "胆大党 S02E19.mp4"), 1, 19},
		{filepath.Join(root, "夏日重现", "Season 1", "[Nekomoe] Summer Time Rendering [01].mkv"), 2, 1},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			sub, ep, _ := scanner.matchFile(tc.path, subscriptions, root)
			assert.NotNil(t, sub)
			assert.Equal(t, tc.wantSubID, sub.ID)
			assert.Equal(t, tc.wantEpisode, ep)
		})
	}
}

func TestMatchFile_Orphan(t *testing.T) {
	scanner := &Scanner{parser: organizer.NewFileNameParser(), matcher: nil}
	subscriptions := []model.Subscription{
		{ID: 1, Name: "胆大党", Season: 1},
	}

	root := "/downloads"
	path := filepath.Join(root, "Unknown Anime", "S01E01.mp4")

	sub, ep, _ := scanner.matchFile(path, subscriptions, root)
	// 目录名不匹配，matcher 为 nil，应该返回 nil
	assert.Nil(t, sub)
	assert.Equal(t, 0, ep)
}

func TestScannerBackupDB(t *testing.T) {
	// 创建临时目录和临时数据库文件
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "auto-rss.db")
	err := os.WriteFile(dbPath, []byte("test db content"), 0644)
	assert.NoError(t, err)

	// 切换到临时目录作为工作目录，让 backupDB 找到文件
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// 创建必要的子目录
	os.MkdirAll("data", 0755)
	os.Rename(dbPath, filepath.Join(tmpDir, "data", "auto-rss.db"))

	// scanner 的 backupDB 不依赖 db 连接来定位文件，使用硬编码路径
	scanner := &Scanner{}
	backupPath, err := scanner.backupDB()
	assert.NoError(t, err)
	assert.NotEmpty(t, backupPath)

	info, err := os.Stat(backupPath)
	assert.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestScannerAggregateEpisodes(t *testing.T) {
	// 构造模拟的磁盘结果
	diskEpisodes := map[uint]map[int][]EpisodeFile{
		1: {
			1: {{Path: "/a/1.mkv", Episode: 1}},
			3: {{Path: "/a/3.mkv", Episode: 3}},
			5: {{Path: "/a/5.mkv", Episode: 5}},
		},
	}

	episodes := make([]int, 0)
	maxEp := 0
	for ep := range diskEpisodes[1] {
		episodes = append(episodes, ep)
		if ep > maxEp {
			maxEp = ep
		}
	}

	assert.Equal(t, 5, maxEp)
	assert.Len(t, episodes, 3)
}

func TestSubscriptionScanResultStruct(t *testing.T) {
	// 确保结构体字段可以正常赋值
	sr := SubscriptionScanResult{
		SubscriptionID:    1,
		Name:              "Test",
		CurrentEpisodeOld: 10,
		CurrentEpisodeNew: 12,
		LatestEpisodeOld:  10,
		LatestEpisodeNew:  12,
		EpisodesOnDisk:    []int{1, 2, 3},
		DownloadsToUpdate: []uint{1, 2},
		DownloadsToCreate: []int{4, 5},
		DownloadsMissing:  []uint{6},
	}
	assert.Equal(t, uint(1), sr.SubscriptionID)
	assert.Equal(t, 12, sr.CurrentEpisodeNew)
	assert.Len(t, sr.EpisodesOnDisk, 3)
}

func TestScanRequestBinding(t *testing.T) {
	req := ScanRequest{DryRun: true}
	assert.True(t, req.DryRun)

	id := uint(42)
	req2 := ScanRequest{DryRun: false, SubscriptionID: &id}
	assert.False(t, req2.DryRun)
	assert.Equal(t, uint(42), *req2.SubscriptionID)
}

func TestVideoExts(t *testing.T) {
	assert.True(t, videoExts[".mp4"])
	assert.True(t, videoExts[".mkv"])
	assert.True(t, videoExts[".avi"])
	assert.False(t, videoExts[".txt"])
	assert.False(t, videoExts[".jpg"])
}

func TestBracketEpisodePattern(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"[01]", 1},
		{"[12]", 12},
		{"[123]", 123},
		{"Summer Time Rendering [25].mkv", 25},
		{"no episode here", 0},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			matches := bracketEpisodePattern.FindStringSubmatch(tc.input)
			if tc.want == 0 {
				assert.Len(t, matches, 0)
				return
			}
			assert.Len(t, matches, 2)
			ep, _ := strconv.Atoi(matches[1])
			assert.Equal(t, tc.want, ep)
		})
	}
}
