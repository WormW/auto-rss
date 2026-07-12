package scheduler

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	err = db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.SubscriptionEpisode{}, &model.EpisodeResourceCandidate{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

func TestSmartFetchMissingEpisodesUsesEpisodeLedgerStatuses(t *testing.T) {
	db := setupTestDB(t)
	episodeRepo := repository.NewEpisodeRepository(db)
	sub := model.Subscription{ID: 1, Name: "ledger completeness", TotalEpisodes: 5}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, episodeRepo.EnsureRange(sub.ID, sub.TotalEpisodes))
	require.NoError(t, episodeRepo.SetStatus(sub.ID, []int{1}, model.EpisodeStatusDownloaded, model.EpisodeStatusSourceAutomatic))
	require.NoError(t, episodeRepo.SetStatus(sub.ID, []int{2}, model.EpisodeStatusMarkedDownloaded, model.EpisodeStatusSourceUser))
	require.NoError(t, episodeRepo.SetStatus(sub.ID, []int{3}, model.EpisodeStatusIgnored, model.EpisodeStatusSourceUser))
	_, claimed, err := episodeRepo.ClaimForDownload(sub.ID, 4, model.EpisodeResource{Hash: "active"})
	require.NoError(t, err)
	require.True(t, claimed)

	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db), episodeRepo)

	missing, err := filter.getMissingEpisodes(&sub)
	require.NoError(t, err)
	assert.Equal(t, []int{5}, missing)
}

func TestEvaluateSubscriptionTreatsEmptyLedgerAsAllMissingWithoutWriting(t *testing.T) {
	db := setupTestDB(t)
	episodeRepo := repository.NewEpisodeRepository(db)
	sub := model.Subscription{ID: 1, Name: "empty ledger", TotalEpisodes: 3, AirDay: "1"}
	require.NoError(t, db.Create(&sub).Error)
	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db), episodeRepo)

	status, _ := filter.EvaluateSubscription(&sub, false)

	assert.Equal(t, []int{1, 2, 3}, status.MissingEpisodes)
	assert.True(t, status.ShouldFetch)
	var count int64
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Count(&count).Error)
	assert.Zero(t, count, "smart fetch status evaluation must be read-only")
}

func TestEvaluateSubscriptionFetchesConservativelyWhenLedgerUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		filter func(*testing.T, *gorm.DB) *SmartFetchFilter
	}{
		{
			name: "nil repository",
			filter: func(_ *testing.T, db *gorm.DB) *SmartFetchFilter {
				return NewSmartFetchFilter(repository.NewDownloadRepository(db), nil)
			},
		},
		{
			name: "query error",
			filter: func(t *testing.T, db *gorm.DB) *SmartFetchFilter {
				t.Helper()
				require.NoError(t, db.Migrator().DropTable(&model.SubscriptionEpisode{}))
				return NewSmartFetchFilter(repository.NewDownloadRepository(db), repository.NewEpisodeRepository(db))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			filter := tt.filter(t, db)
			completedAt := time.Now().Add(-40 * 24 * time.Hour)
			status, _ := filter.EvaluateSubscription(&model.Subscription{
				ID:             1,
				Name:           tt.name,
				TotalEpisodes:  3,
				CurrentEpisode: 3,
				CompletedAt:    &completedAt,
				AirDay:         "1",
			}, false)

			assert.True(t, status.ShouldFetch)
			assert.Equal(t, "episode_ledger_unavailable", status.FetchReason)
			assert.Equal(t, DefaultSmartFetchStrategy().NormalInterval, status.NextFetchInterval)
		})
	}
}

type smartFetchConfigRepoStub struct {
	values map[string]string
}

func (r *smartFetchConfigRepoStub) Get(key string) (*model.Config, error) {
	if value, ok := r.values[key]; ok {
		return &model.Config{Key: key, Value: value}, nil
	}
	return nil, errors.New("not found")
}

func (r *smartFetchConfigRepoStub) GetCached(key string) (string, error) {
	cfg, err := r.Get(key)
	if err != nil {
		return "", err
	}
	return cfg.Value, nil
}

func (r *smartFetchConfigRepoStub) Set(key, value string) error { return nil }
func (r *smartFetchConfigRepoStub) Delete(key string) error     { return nil }
func (r *smartFetchConfigRepoStub) GetAll() ([]model.Config, error) {
	configs := make([]model.Config, 0, len(r.values))
	for key, value := range r.values {
		configs = append(configs, model.Config{Key: key, Value: value})
	}
	return configs, nil
}

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}

func TestCompletedAtLogValue(t *testing.T) {
	assert.Nil(t, completedAtLogValue(nil))

	completedAt := time.Date(2026, time.July, 11, 16, 30, 0, 0, time.Local)
	assert.Equal(t, "2026-07-11 16:30:00", completedAtLogValue(&completedAt))
}

func TestEvaluateSubscription_CompletedStopDays(t *testing.T) {
	db := setupTestDB(t)

	// 创建 download repository
	downloadRepo := repository.NewDownloadRepository(db)

	// 创建过滤器
	filter := NewSmartFetchFilter(downloadRepo, repository.NewEpisodeRepository(db))
	strategy := DefaultSmartFetchStrategy()
	strategy.CompletedStopDays = 30 // 30天后停止检查
	filter.SetStrategy(strategy)

	now := time.Now()
	completed35DaysAgo := now.Add(-35 * 24 * time.Hour)
	completed10DaysAgo := now.Add(-10 * 24 * time.Hour)

	tests := []struct {
		name         string
		sub          *model.Subscription
		expectFetch  bool
		expectReason string
	}{
		{
			name: "完结超过30天 - 应停止检查",
			sub: &model.Subscription{
				ID:             1,
				Name:           "完结动画-超30天",
				TotalEpisodes:  12,
				CurrentEpisode: 12,
				CompletedAt:    &completed35DaysAgo,
			},
			expectFetch:  false,
			expectReason: "completed_35_days_ago_stop_checking",
		},
		{
			name: "完结10天 - 应继续检查（补全v2等）",
			sub: &model.Subscription{
				ID:             2,
				Name:           "完结动画-10天",
				TotalEpisodes:  12,
				CurrentEpisode: 12,
				CompletedAt:    &completed10DaysAgo,
				AirDay:         "1",
			},
			expectFetch: true, // 完结但未满30天，可能还有v2版本
		},
		{
			name: "刚完结（CompletedAt为nil）- 应设置CompletedAt",
			sub: &model.Subscription{
				ID:             3,
				Name:           "刚完结动画",
				TotalEpisodes:  12,
				CurrentEpisode: 12,
				CompletedAt:    nil,
				AirDay:         "1",
			},
			expectFetch: true, // 刚完结，需要设置CompletedAt并继续检查
		},
		{
			name: "未完结动画 - 正常检查",
			sub: &model.Subscription{
				ID:             4,
				Name:           "连载中动画",
				TotalEpisodes:  12,
				CurrentEpisode: 5,
				CompletedAt:    nil,
				AirDay:         "1",
			},
			expectFetch: true,
		},
		{
			name: "完结30天整（临界值）- 应停止检查",
			sub: &model.Subscription{
				ID:             5,
				Name:           "完结动画-刚好30天",
				TotalEpisodes:  12,
				CurrentEpisode: 12,
				CompletedAt:    &completed10DaysAgo, // 实际10天前，但为了测试临界值我们需要一个刚好30天的
			},
			expectFetch: true, // 10天未满30天，继续检查
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 为临界值测试特殊处理
			if tt.name == "完结30天整（临界值）- 应停止检查" {
				completedExactly30DaysAgo := now.Add(-30 * 24 * time.Hour)
				tt.sub.CompletedAt = &completedExactly30DaysAgo
				tt.expectFetch = false
				tt.expectReason = "completed_30_days_ago_stop_checking"
			}

			status, _ := filter.EvaluateSubscription(tt.sub, false)
			assert.Equal(t, tt.expectFetch, status.ShouldFetch, "ShouldFetch mismatch")
			if tt.expectReason != "" {
				assert.Contains(t, status.FetchReason, tt.expectReason[:20], "FetchReason should contain expected text")
			}

			// 验证刚完结的动画是否被设置了 CompletedAt
			if tt.name == "刚完结（CompletedAt为nil）- 应设置CompletedAt" {
				assert.NotNil(t, tt.sub.CompletedAt, "CompletedAt should be set for newly completed subscription")
			}
		})
	}
}

func TestEvaluateSubscription_CompletedStopDaysZero(t *testing.T) {
	db := setupTestDB(t)

	// 测试 CompletedStopDays = 0（不停止检查）
	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db), repository.NewEpisodeRepository(db))
	strategy := DefaultSmartFetchStrategy()
	strategy.CompletedStopDays = 0 // 0表示不停止检查
	filter.SetStrategy(strategy)

	completed100DaysAgo := time.Now().Add(-100 * 24 * time.Hour)

	sub := &model.Subscription{
		ID:             1,
		Name:           "完结100天动画",
		TotalEpisodes:  12,
		CurrentEpisode: 12,
		CompletedAt:    &completed100DaysAgo,
		AirDay:         "1",
	}

	status, _ := filter.EvaluateSubscription(sub, false)
	// 即使完结100天，因为 CompletedStopDays=0，所以不会停止检查
	// 但是会根据其他逻辑（如在活跃窗口等）决定是否拉取
	// 这里主要看 Reason 不包含 "stop_checking"
	assert.NotContains(t, status.FetchReason, "stop_checking", "Should not stop checking when CompletedStopDays=0")
}

func TestEvaluateSubscription_ClearsStaleCompletedAtForOffsetSubscription(t *testing.T) {
	filter := NewSmartFetchFilter(nil, nil)
	completedAt := time.Now().Add(-40 * 24 * time.Hour)
	sub := &model.Subscription{
		Name:           "偏移订阅",
		EpisodeOffset:  170,
		TotalEpisodes:  52,
		CurrentEpisode: 171,
		CompletedAt:    &completedAt,
		AirDay:         "1",
	}

	status, needsUpdate := filter.EvaluateSubscription(sub, false)

	assert.False(t, status.IsCompleted)
	assert.True(t, needsUpdate)
	assert.Nil(t, sub.CompletedAt)
}

func TestEvaluateSubscription_SmartFetchGlobalDisabled(t *testing.T) {
	db := setupTestDB(t)
	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db), repository.NewEpisodeRepository(db))
	filter.LoadConfigFromDB(&smartFetchConfigRepoStub{
		values: map[string]string{
			"smart_fetch.enabled": "false",
		},
	})

	status, _ := filter.EvaluateSubscription(&model.Subscription{
		ID:             1,
		Name:           "全局禁用智能拉取",
		TotalEpisodes:  12,
		CurrentEpisode: 12,
		AirDay:         "1",
	}, false)

	assert.True(t, status.ShouldFetch)
	assert.False(t, status.SmartFetchEnabled)
	assert.Equal(t, "smart_fetch_disabled", status.FetchReason)
	assert.NotEmpty(t, status.Explanation)
}

func TestEvaluateSubscription_SubscriptionOverrideNever(t *testing.T) {
	db := setupTestDB(t)
	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db), repository.NewEpisodeRepository(db))

	status, _ := filter.EvaluateSubscription(&model.Subscription{
		ID:                 1,
		Name:               "单订阅禁用智能拉取",
		TotalEpisodes:      12,
		CurrentEpisode:     12,
		AirDay:             "1",
		SmartFetchOverride: "never",
	}, false)

	assert.True(t, status.ShouldFetch)
	assert.False(t, status.SmartFetchEnabled)
	assert.Equal(t, "smart_fetch_disabled", status.FetchReason)
}

func TestEvaluateSubscription_CalendarOnlySkipsRSSFetch(t *testing.T) {
	filter := NewSmartFetchFilter(nil, nil)

	status, needsUpdate := filter.EvaluateSubscription(&model.Subscription{
		ID:         1,
		Name:       "追番日历条目",
		SourceType: "calendar",
		AirDay:     "3",
		AirTime:    "23:30",
		Enabled:    true,
	}, false)

	assert.False(t, needsUpdate)
	assert.False(t, status.ShouldFetch)
	assert.False(t, status.SmartFetchEnabled)
	assert.Equal(t, "calendar_only", status.FetchReason)
	assert.NotEmpty(t, status.Explanation)
}

func TestIsCompleted(t *testing.T) {
	filter := NewSmartFetchFilter(nil, nil)

	tests := []struct {
		name   string
		sub    *model.Subscription
		expect bool
	}{
		{
			name: "已完结",
			sub: &model.Subscription{
				TotalEpisodes:  12,
				CurrentEpisode: 12,
			},
			expect: true,
		},
		{
			name: "未完结",
			sub: &model.Subscription{
				TotalEpisodes:  12,
				CurrentEpisode: 5,
			},
			expect: false,
		},
		{
			name: "未知总集数",
			sub: &model.Subscription{
				TotalEpisodes:  0,
				CurrentEpisode: 12,
			},
			expect: false,
		},
		{
			name: "超过总集数",
			sub: &model.Subscription{
				TotalEpisodes:  12,
				CurrentEpisode: 13, // 可能是特别篇或数据错误
			},
			expect: true,
		},
		{
			name: "偏移订阅尚未完结",
			sub: &model.Subscription{
				EpisodeOffset:  170,
				TotalEpisodes:  52,
				CurrentEpisode: 221,
			},
			expect: false,
		},
		{
			name: "偏移订阅已经完结",
			sub: &model.Subscription{
				EpisodeOffset:  170,
				TotalEpisodes:  52,
				CurrentEpisode: 222,
			},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.isCompleted(tt.sub)
			assert.Equal(t, tt.expect, result)
		})
	}
}
