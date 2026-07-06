package recovery

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/organizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
		MatchedEpisodes:   []EpisodeFile{{Path: "/a/1.mkv", Episode: 1, Season: 1}},
		DownloadsToUpdate: []uint{1, 2},
		DownloadsToCreate: []int{4, 5},
		DownloadsMissing:  []uint{6},
	}
	assert.Equal(t, uint(1), sr.SubscriptionID)
	assert.Equal(t, 12, sr.CurrentEpisodeNew)
	assert.Len(t, sr.EpisodesOnDisk, 3)
	assert.Len(t, sr.MatchedEpisodes, 1)
}

func TestScannerDryRunReportContractWithFixtures(t *testing.T) {
	db, scanner, sub, existing, missing := newRecoveryScannerFixture(t)

	result, err := scanner.Scan(&ScanRequest{DryRun: true})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Applied)
	assert.Empty(t, result.BackupPath)
	assert.Equal(t, 6, result.ScannedFiles)
	assert.Equal(t, 4, result.MatchedFiles)
	require.Len(t, result.OrphanFiles, 2)
	assert.Contains(t, basenamesFromPaths(result.OrphanFiles), "Unknown Show S01E01.mkv")
	assert.Contains(t, basenamesFromPaths(result.OrphanFiles), "Fixture Show bonus footage.mkv")

	require.Len(t, result.Subscriptions, 1)
	report := result.Subscriptions[0]
	assert.Equal(t, sub.ID, report.SubscriptionID)
	assert.Equal(t, "Fixture Show", report.Name)
	assert.Equal(t, []int{1, 2, 3}, report.EpisodesOnDisk)
	assert.NotContains(t, report.EpisodesOnDisk, 0)
	assert.Equal(t, 1, report.CurrentEpisodeOld)
	assert.Equal(t, 3, report.CurrentEpisodeNew)
	assert.Equal(t, 2, report.LatestEpisodeOld)
	assert.Equal(t, 3, report.LatestEpisodeNew)
	assert.Equal(t, []uint{existing.ID}, report.DownloadsToUpdate)
	assert.Equal(t, []int{1, 3}, report.DownloadsToCreate)
	assert.NotContains(t, report.DownloadsToCreate, 0)
	assert.Equal(t, []uint{missing.ID}, report.DownloadsMissing)

	require.Len(t, report.MatchedEpisodes, 4)
	assert.Equal(t, []int{1, 2, 3, 3}, episodeNumbers(report.MatchedEpisodes))
	assert.Contains(t, basenames(report.MatchedEpisodes), "Fixture Show S01E02.mkv")
	assert.Contains(t, basenames(report.MatchedEpisodes), "[Fansub] Fixture Show [01][1080p].mkv")
	assert.Contains(t, basenames(report.MatchedEpisodes), "Fixture Show S01E03.mkv")
	assert.Contains(t, basenames(report.MatchedEpisodes), "Fixture Show S01E03 duplicate.mkv")

	var afterSub model.Subscription
	require.NoError(t, db.First(&afterSub, sub.ID).Error)
	assert.Equal(t, 1, afterSub.CurrentEpisode)
	assert.Equal(t, 2, afterSub.LatestEpisode)

	var afterExisting model.Download
	require.NoError(t, db.First(&afterExisting, existing.ID).Error)
	assert.Equal(t, model.DownloadStatusDownloading, afterExisting.Status)
	assert.Empty(t, afterExisting.RenamedPath)
}

func TestScannerScopedScanOnlyWalksSubscriptionDirectory(t *testing.T) {
	_, scanner, sub, _, _ := newRecoveryScannerFixture(t)

	result, err := scanner.Scan(&ScanRequest{DryRun: true, SubscriptionID: &sub.ID})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 5, result.ScannedFiles)
	assert.Equal(t, 4, result.MatchedFiles)
	require.Len(t, result.OrphanFiles, 1)
	assert.Equal(t, []string{"Fixture Show bonus footage.mkv"}, basenamesFromPaths(result.OrphanFiles))
}

func TestScannerScopedScanMissingDirectoryDoesNotWalkDownloadRoot(t *testing.T) {
	db, scanner, _, _, _ := newRecoveryScannerFixture(t)
	missingDirSub := model.Subscription{
		Name:           "No Folder Show",
		Season:         1,
		CurrentEpisode: 0,
		LatestEpisode:  0,
		Enabled:        true,
		Status:         "active",
	}
	require.NoError(t, db.Create(&missingDirSub).Error)

	result, err := scanner.Scan(&ScanRequest{DryRun: true, SubscriptionID: &missingDirSub.ID})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 0, result.ScannedFiles)
	assert.Equal(t, 0, result.MatchedFiles)
	assert.Empty(t, result.OrphanFiles)
	assert.Empty(t, result.Subscriptions)
}

func TestScannerRejectsApplyModeByDefault(t *testing.T) {
	db, scanner, sub, existing, _ := newRecoveryScannerFixture(t)
	t.Setenv(recoveryApplyEnabledEnv, "")

	result, err := scanner.Scan(&ScanRequest{DryRun: false})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrApplyDisabled))
	assert.Nil(t, result)

	var afterSub model.Subscription
	require.NoError(t, db.First(&afterSub, sub.ID).Error)
	assert.Equal(t, 1, afterSub.CurrentEpisode)
	assert.Equal(t, 2, afterSub.LatestEpisode)

	var afterExisting model.Download
	require.NoError(t, db.First(&afterExisting, existing.ID).Error)
	assert.Equal(t, model.DownloadStatusDownloading, afterExisting.Status)
	assert.Empty(t, afterExisting.RenamedPath)
}

func TestScannerApplyStopsWhenBackupFails(t *testing.T) {
	db, scanner, sub, existing, _ := newRecoveryScannerFixture(t)
	t.Setenv(recoveryApplyEnabledEnv, "true")

	origWd, err := os.Getwd()
	require.NoError(t, err)
	tempWd := t.TempDir()
	require.NoError(t, os.Chdir(tempWd))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(origWd))
	})

	result, err := scanner.Scan(&ScanRequest{DryRun: false})

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to backup database")
	assert.ErrorContains(t, err, "failed to open db for backup")
	assert.Nil(t, result)

	var afterSub model.Subscription
	require.NoError(t, db.First(&afterSub, sub.ID).Error)
	assert.Equal(t, 1, afterSub.CurrentEpisode)
	assert.Equal(t, 2, afterSub.LatestEpisode)

	var afterExisting model.Download
	require.NoError(t, db.First(&afterExisting, existing.ID).Error)
	assert.Equal(t, model.DownloadStatusDownloading, afterExisting.Status)
	assert.Empty(t, afterExisting.RenamedPath)

	_, statErr := os.Stat(filepath.Join(tempWd, "data"))
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestRecoveryApplyEnabled(t *testing.T) {
	t.Setenv(recoveryApplyEnabledEnv, "")
	assert.False(t, recoveryApplyEnabled())

	t.Setenv(recoveryApplyEnabledEnv, "false")
	assert.False(t, recoveryApplyEnabled())

	t.Setenv(recoveryApplyEnabledEnv, "true")
	assert.True(t, recoveryApplyEnabled())

	t.Setenv(recoveryApplyEnabledEnv, "TRUE")
	assert.True(t, recoveryApplyEnabled())
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

func newRecoveryScannerFixture(t *testing.T) (*gorm.DB, *Scanner, model.Subscription, model.Download, model.Download) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))

	root := t.TempDir()
	requireFile(t, filepath.Join(root, "Fixture Show", "Season 1", "Fixture Show S01E02.mkv"))
	requireFile(t, filepath.Join(root, "Fixture Show", "Raw", "[Fansub] Fixture Show [01][1080p].mkv"))
	requireFile(t, filepath.Join(root, "Fixture Show", "Raw", "Fixture Show bonus footage.mkv"))
	requireFile(t, filepath.Join(root, "Fixture Show", "Nested", "Season 1", "Fixture Show S01E03.mkv"))
	requireFile(t, filepath.Join(root, "Fixture Show", "Nested", "Season 1", "Copy", "Fixture Show S01E03 duplicate.mkv"))
	requireFile(t, filepath.Join(root, "Unknown Show", "Unknown Show S01E01.mkv"))

	sub := model.Subscription{
		Name:           "Fixture Show",
		Season:         1,
		CurrentEpisode: 1,
		LatestEpisode:  2,
		Enabled:        true,
		Status:         "active",
	}
	require.NoError(t, db.Create(&sub).Error)

	existing := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Fixture Show 02",
		Episode:        2,
		TorrentURL:     "memory://existing",
		TorrentHash:    "existing-02",
		Status:         model.DownloadStatusDownloading,
	}
	require.NoError(t, db.Create(&existing).Error)

	missing := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Fixture Show 04",
		Episode:        4,
		TorrentURL:     "memory://missing",
		TorrentHash:    "missing-04",
		RenamedPath:    filepath.Join(root, "Fixture Show", "Season 1", "Fixture Show S01E04.mkv"),
		Status:         model.DownloadStatusCompleted,
	}
	require.NoError(t, db.Create(&missing).Error)

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)
	require.NoError(t, configRepo.Set("download_path", root))

	scanner := NewScanner(db, subRepo, downloadRepo, configRepo, nil)
	return db, scanner, sub, existing, missing
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0644))
}

func episodeNumbers(files []EpisodeFile) []int {
	episodes := make([]int, 0, len(files))
	for _, file := range files {
		episodes = append(episodes, file.Episode)
	}
	return episodes
}

func basenames(files []EpisodeFile) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, filepath.Base(file.Path))
	}
	sort.Strings(names)
	return names
}

func basenamesFromPaths(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)
	return names
}
