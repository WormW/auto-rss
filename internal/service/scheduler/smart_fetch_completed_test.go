package scheduler

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
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

	err = db.AutoMigrate(&model.Subscription{}, &model.Download{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
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

func TestEvaluateSubscription_CompletedStopDays(t *testing.T) {
	db := setupTestDB(t)

	// 创建 download repository
	downloadRepo := repository.NewDownloadRepository(db)

	// 创建过滤器
	filter := NewSmartFetchFilter(downloadRepo)
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

			status, _ := filter.EvaluateSubscription(tt.sub)
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
	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db))
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

	status, _ := filter.EvaluateSubscription(sub)
	// 即使完结100天，因为 CompletedStopDays=0，所以不会停止检查
	// 但是会根据其他逻辑（如在活跃窗口等）决定是否拉取
	// 这里主要看 Reason 不包含 "stop_checking"
	assert.NotContains(t, status.FetchReason, "stop_checking", "Should not stop checking when CompletedStopDays=0")
}

func TestEvaluateSubscription_SmartFetchGlobalDisabled(t *testing.T) {
	db := setupTestDB(t)
	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db))
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
	})

	assert.True(t, status.ShouldFetch)
	assert.False(t, status.SmartFetchEnabled)
	assert.Equal(t, "smart_fetch_disabled", status.FetchReason)
	assert.NotEmpty(t, status.Explanation)
}

func TestEvaluateSubscription_SubscriptionOverrideNever(t *testing.T) {
	db := setupTestDB(t)
	filter := NewSmartFetchFilter(repository.NewDownloadRepository(db))

	status, _ := filter.EvaluateSubscription(&model.Subscription{
		ID:                 1,
		Name:               "单订阅禁用智能拉取",
		TotalEpisodes:      12,
		CurrentEpisode:     12,
		AirDay:             "1",
		SmartFetchOverride: "never",
	})

	assert.True(t, status.ShouldFetch)
	assert.False(t, status.SmartFetchEnabled)
	assert.Equal(t, "smart_fetch_disabled", status.FetchReason)
}

func TestIsCompleted(t *testing.T) {
	filter := NewSmartFetchFilter(nil)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.isCompleted(tt.sub)
			assert.Equal(t, tt.expect, result)
		})
	}
}
