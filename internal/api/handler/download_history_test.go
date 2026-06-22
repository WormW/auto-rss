package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDownloadHistoryRouter(repo *mockDownloadRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewDownloadHistoryHandler(repo)
	router.GET("/downloads/history", handler.GetHistory)
	router.GET("/downloads/statistics", handler.GetStatistics)
	return router
}

func TestDownloadHistoryHandler_GetHistory(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		mockHistory   func(t *testing.T) func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error)
		wantStatus    int
		wantCode      int
		wantPage      int
		wantPageSize  int
		wantTotal     int64
		wantListCount int
		wantCalled    bool
	}{
		{
			name:  "default pagination",
			query: "",
			mockHistory: func(t *testing.T) func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
				return func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
					assert.NotNil(t, filter)
					assert.Nil(t, filter.SubscriptionID)
					assert.Empty(t, filter.Status)
					assert.Nil(t, filter.StartDate)
					assert.Nil(t, filter.EndDate)
					assert.Equal(t, 0, offset)
					assert.Equal(t, 20, limit)
					return []model.Download{{ID: 1, Title: "Episode 1", Status: model.DownloadStatusCompleted}}, 1, nil
				}
			},
			wantStatus:    http.StatusOK,
			wantCode:      0,
			wantPage:      1,
			wantPageSize:  20,
			wantTotal:     1,
			wantListCount: 1,
			wantCalled:    true,
		},
		{
			name:  "clamped pagination",
			query: "?page=-3&page_size=500",
			mockHistory: func(t *testing.T) func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
				return func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
					assert.NotNil(t, filter)
					assert.Equal(t, 0, offset)
					assert.Equal(t, 100, limit)
					return []model.Download{}, 0, nil
				}
			},
			wantStatus:   http.StatusOK,
			wantCode:     0,
			wantPage:     1,
			wantPageSize: 100,
			wantTotal:    0,
			wantCalled:   true,
		},
		{
			name:       "invalid start_date",
			query:      "?start_date=2026/06/01",
			wantStatus: http.StatusBadRequest,
			wantCode:   400,
		},
		{
			name:       "invalid end_date",
			query:      "?end_date=06-01-2026",
			wantStatus: http.StatusBadRequest,
			wantCode:   400,
		},
		{
			name:  "valid filters propagate",
			query: "?page=3&page_size=5&status=failed&subscription_id=42&start_date=2026-06-01&end_date=2026-06-02",
			mockHistory: func(t *testing.T) func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
				return func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
					require.NotNil(t, filter)
					require.NotNil(t, filter.SubscriptionID)
					assert.Equal(t, uint(42), *filter.SubscriptionID)
					assert.Equal(t, model.DownloadStatusFailed, filter.Status)
					require.NotNil(t, filter.StartDate)
					require.NotNil(t, filter.EndDate)
					assert.Equal(t, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC), *filter.StartDate)
					assert.Equal(t, time.Date(2026, time.June, 2, 23, 59, 59, 0, time.UTC), *filter.EndDate)
					assert.Equal(t, 10, offset)
					assert.Equal(t, 5, limit)
					return []model.Download{{ID: 42, Title: "Filtered", Status: model.DownloadStatusFailed}}, 7, nil
				}
			},
			wantStatus:    http.StatusOK,
			wantCode:      0,
			wantPage:      3,
			wantPageSize:  5,
			wantTotal:     7,
			wantListCount: 1,
			wantCalled:    true,
		},
		{
			name:  "repository error",
			query: "",
			mockHistory: func(t *testing.T) func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
				return func(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
					return nil, 0, errors.New("database unavailable")
				}
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   500,
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockDownloadRepo{}
			if tt.mockHistory != nil {
				repo.getDownloadHistoryFunc = tt.mockHistory(t)
			}
			router := setupDownloadHistoryRouter(repo)

			req := httptest.NewRequest(http.MethodGet, "/downloads/history"+tt.query, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tt.wantStatus, w.Code, w.Body.String())

			var resp struct {
				Code int `json:"code"`
				Data struct {
					List     []model.Download `json:"list"`
					Total    int64            `json:"total"`
					Page     int              `json:"page"`
					PageSize int              `json:"page_size"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tt.wantCode, resp.Code)

			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, tt.wantPage, resp.Data.Page)
				assert.Equal(t, tt.wantPageSize, resp.Data.PageSize)
				assert.Equal(t, tt.wantTotal, resp.Data.Total)
				assert.Len(t, resp.Data.List, tt.wantListCount)
			}

			if tt.wantCalled {
				assert.Equal(t, 1, repo.getDownloadHistoryCalls)
			} else {
				assert.Zero(t, repo.getDownloadHistoryCalls)
			}
		})
	}
}

func TestDownloadHistoryHandler_GetStatistics(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		mockStatistics func(t *testing.T) func(days int) (*repository.DownloadStatistics, error)
		wantStatus     int
		wantCode       int
		wantTotalCount int64
		wantDailyCount int
	}{
		{
			name:  "default days",
			query: "",
			mockStatistics: func(t *testing.T) func(days int) (*repository.DownloadStatistics, error) {
				return func(days int) (*repository.DownloadStatistics, error) {
					assert.Equal(t, 7, days)
					return &repository.DownloadStatistics{
						TotalCount:     3,
						CompletedCount: 2,
						FailedCount:    1,
						DailyStats: []repository.DailyStat{
							{Date: "2026-06-22", Count: 3, Completed: 2, Failed: 1},
						},
					}, nil
				}
			},
			wantStatus:     http.StatusOK,
			wantCode:       0,
			wantTotalCount: 3,
			wantDailyCount: 1,
		},
		{
			name:  "clamped days",
			query: "?days=999",
			mockStatistics: func(t *testing.T) func(days int) (*repository.DownloadStatistics, error) {
				return func(days int) (*repository.DownloadStatistics, error) {
					assert.Equal(t, 365, days)
					return &repository.DownloadStatistics{TotalCount: 0}, nil
				}
			},
			wantStatus: http.StatusOK,
			wantCode:   0,
		},
		{
			name:  "low days resets to default",
			query: "?days=0",
			mockStatistics: func(t *testing.T) func(days int) (*repository.DownloadStatistics, error) {
				return func(days int) (*repository.DownloadStatistics, error) {
					assert.Equal(t, 7, days)
					return &repository.DownloadStatistics{TotalCount: 0}, nil
				}
			},
			wantStatus: http.StatusOK,
			wantCode:   0,
		},
		{
			name:  "repository error",
			query: "",
			mockStatistics: func(t *testing.T) func(days int) (*repository.DownloadStatistics, error) {
				return func(days int) (*repository.DownloadStatistics, error) {
					assert.Equal(t, 7, days)
					return nil, errors.New("database unavailable")
				}
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockDownloadRepo{getDownloadStatisticsFunc: tt.mockStatistics(t)}
			router := setupDownloadHistoryRouter(repo)

			req := httptest.NewRequest(http.MethodGet, "/downloads/statistics"+tt.query, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tt.wantStatus, w.Code, w.Body.String())

			var resp struct {
				Code int `json:"code"`
				Data struct {
					TotalCount int64                  `json:"total_count"`
					DailyStats []repository.DailyStat `json:"daily_stats"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tt.wantCode, resp.Code)

			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, tt.wantTotalCount, resp.Data.TotalCount)
				assert.Len(t, resp.Data.DailyStats, tt.wantDailyCount)
			}
			assert.Equal(t, 1, repo.getDownloadStatisticsCalls)
		})
	}
}
