package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// mockDownloadRepo is a mock implementation of DownloadRepository for testing
type mockDownloadRepo struct {
	createFunc                  func(download *model.Download) error
	updateFunc                  func(download *model.Download) error
	deleteFunc                  func(id uint) error
	getByIDFunc                 func(id uint) (*model.Download, error)
	getByHashFunc               func(hash string) (*model.Download, error)
	getBySubAndEpFunc           func(subscriptionID uint, episode int) (*model.Download, error)
	getBySubAndEpWithLangFunc   func(subscriptionID uint, episode int) ([]model.Download, error)
	getRecentBySubFunc          func(subscriptionID uint, limit int) ([]model.Download, error)
	listFunc                    func(offset, limit int, status string) ([]model.Download, int64, error)
	listBySubIDFunc             func(subscriptionID uint) ([]model.Download, error)
	updateStatusFunc            func(id uint, status string) error
	batchDeleteFunc             func(ids []uint) error
	deleteByStatusFunc          func(status string) error
	deleteAllFunc               func() error
	getFailedReadyFunc          func(limit int) ([]model.Download, error)
	getByRetryCountFunc         func(minRetries, maxRetries int) ([]model.Download, error)
	createInTxFunc              func(tx *gorm.DB, download *model.Download) error
	updateInTxFunc              func(tx *gorm.DB, download *model.Download) error

	// Tracking fields
	listCalls    int
	getByIDCalls int
	updateCalls  int
	deleteCalls  int
}

func (m *mockDownloadRepo) Create(download *model.Download) error {
	if m.createFunc != nil {
		return m.createFunc(download)
	}
	return nil
}

func (m *mockDownloadRepo) Update(download *model.Download) error {
	m.updateCalls++
	if m.updateFunc != nil {
		return m.updateFunc(download)
	}
	return nil
}

func (m *mockDownloadRepo) Delete(id uint) error {
	m.deleteCalls++
	if m.deleteFunc != nil {
		return m.deleteFunc(id)
	}
	return nil
}

func (m *mockDownloadRepo) GetByID(id uint) (*model.Download, error) {
	m.getByIDCalls++
	if m.getByIDFunc != nil {
		return m.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}

func (m *mockDownloadRepo) GetByHash(hash string) (*model.Download, error) {
	if m.getByHashFunc != nil {
		return m.getByHashFunc(hash)
	}
	return nil, errors.New("not found")
}

func (m *mockDownloadRepo) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	if m.getBySubAndEpFunc != nil {
		return m.getBySubAndEpFunc(subscriptionID, episode)
	}
	return nil, errors.New("not found")
}

func (m *mockDownloadRepo) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	if m.getBySubAndEpWithLangFunc != nil {
		return m.getBySubAndEpWithLangFunc(subscriptionID, episode)
	}
	return nil, nil
}

func (m *mockDownloadRepo) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	if m.getRecentBySubFunc != nil {
		return m.getRecentBySubFunc(subscriptionID, limit)
	}
	return nil, nil
}

func (m *mockDownloadRepo) List(offset, limit int, status string) ([]model.Download, int64, error) {
	m.listCalls++
	if m.listFunc != nil {
		return m.listFunc(offset, limit, status)
	}
	return []model.Download{}, 0, nil
}

func (m *mockDownloadRepo) ListBySubscriptionID(subscriptionID uint) ([]model.Download, error) {
	if m.listBySubIDFunc != nil {
		return m.listBySubIDFunc(subscriptionID)
	}
	return nil, nil
}

func (m *mockDownloadRepo) UpdateStatus(id uint, status string) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(id, status)
	}
	return nil
}

func (m *mockDownloadRepo) BatchDelete(ids []uint) error {
	if m.batchDeleteFunc != nil {
		return m.batchDeleteFunc(ids)
	}
	return nil
}

func (m *mockDownloadRepo) DeleteByStatus(status string) error {
	if m.deleteByStatusFunc != nil {
		return m.deleteByStatusFunc(status)
	}
	return nil
}

func (m *mockDownloadRepo) DeleteAll() error {
	if m.deleteAllFunc != nil {
		return m.deleteAllFunc()
	}
	return nil
}

func (m *mockDownloadRepo) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	if m.getFailedReadyFunc != nil {
		return m.getFailedReadyFunc(limit)
	}
	return nil, nil
}

func (m *mockDownloadRepo) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	if m.getByRetryCountFunc != nil {
		return m.getByRetryCountFunc(minRetries, maxRetries)
	}
	return nil, nil
}

func (m *mockDownloadRepo) CreateInTx(tx *gorm.DB, download *model.Download) error {
	if m.createInTxFunc != nil {
		return m.createInTxFunc(tx, download)
	}
	return nil
}

func (m *mockDownloadRepo) UpdateInTx(tx *gorm.DB, download *model.Download) error {
	if m.updateInTxFunc != nil {
		return m.updateInTxFunc(tx, download)
	}
	return nil
}

func (m *mockDownloadRepo) GetDownloadHistory(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
	return nil, 0, nil
}

func (m *mockDownloadRepo) GetDownloadStatistics(days int) (*repository.DownloadStatistics, error) {
	return nil, nil
}

func TestDownloadHandler_List(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		mockList       func(offset, limit int, status string) ([]model.Download, int64, error)
		wantStatus     int
		wantCount      int
		wantTotal      int64
		wantListCalled bool
	}{
		{
			name:  "success with default pagination",
			query: "",
			mockList: func(offset, limit int, status string) ([]model.Download, int64, error) {
				return []model.Download{
					{ID: 1, Title: "Test 1", Status: "downloading"},
					{ID: 2, Title: "Test 2", Status: "completed"},
				}, 2, nil
			},
			wantStatus:     http.StatusOK,
			wantCount:      2,
			wantTotal:      2,
			wantListCalled: true,
		},
		{
			name:  "filter by status downloading",
			query: "?status=downloading",
			mockList: func(offset, limit int, status string) ([]model.Download, int64, error) {
				assert.Equal(t, "downloading", status)
				return []model.Download{
					{ID: 1, Title: "Test 1", Status: "downloading"},
				}, 1, nil
			},
			wantStatus:     http.StatusOK,
			wantCount:      1,
			wantTotal:      1,
			wantListCalled: true,
		},
		{
			name:  "filter by status completed",
			query: "?status=completed",
			mockList: func(offset, limit int, status string) ([]model.Download, int64, error) {
				assert.Equal(t, "completed", status)
				return []model.Download{
					{ID: 2, Title: "Test 2", Status: "completed"},
				}, 1, nil
			},
			wantStatus:     http.StatusOK,
			wantCount:      1,
			wantTotal:      1,
			wantListCalled: true,
		},
		{
			name:  "with custom pagination",
			query: "?page=2&page_size=10",
			mockList: func(offset, limit int, status string) ([]model.Download, int64, error) {
				assert.Equal(t, 10, offset) // (2-1) * 10
				assert.Equal(t, 10, limit)
				return []model.Download{}, 0, nil
			},
			wantStatus:     http.StatusOK,
			wantCount:      0,
			wantTotal:      0,
			wantListCalled: true,
		},
		{
			name:  "repository error",
			query: "",
			mockList: func(offset, limit int, status string) ([]model.Download, int64, error) {
				return nil, 0, errors.New("database error")
			},
			wantStatus:     http.StatusInternalServerError,
			wantCount:      0,
			wantTotal:      0,
			wantListCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockDownloadRepo{listFunc: tt.mockList}
			handler := NewDownloadHandler(mockRepo, nil, nil)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.GET("/downloads", handler.List)

			req := httptest.NewRequest("GET", "/downloads"+tt.query, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK {
				var resp struct {
					Code int `json:"code"`
					Data struct {
						List  []model.Download `json:"list"`
						Total int64            `json:"total"`
					} `json:"data"`
				}
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, 0, resp.Code)
				assert.Len(t, resp.Data.List, tt.wantCount)
				assert.Equal(t, tt.wantTotal, resp.Data.Total)
			}

			if tt.wantListCalled {
				assert.GreaterOrEqual(t, mockRepo.listCalls, 1)
			}
		})
	}
}

func TestDownloadHandler_GetByID(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		mockGet    func(id uint) (*model.Download, error)
		wantStatus int
		wantCode   int // response code field
	}{
		{
			name: "success",
			id:   "1",
			mockGet: func(id uint) (*model.Download, error) {
				assert.Equal(t, uint(1), id)
				return &model.Download{
					ID:     1,
					Title:  "Test Download",
					Status: "downloading",
				}, nil
			},
			wantStatus: http.StatusOK,
			wantCode:   0,
		},
		{
			name:       "invalid id format",
			id:         "abc",
			mockGet:    nil,
			wantStatus: http.StatusBadRequest,
			wantCode:   400,
		},
		{
			name: "not found",
			id:   "999",
			mockGet: func(id uint) (*model.Download, error) {
				return nil, errors.New("not found")
			},
			wantStatus: http.StatusNotFound,
			wantCode:   404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockDownloadRepo{getByIDFunc: tt.mockGet}
			handler := NewDownloadHandler(mockRepo, nil, nil)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.GET("/downloads/:id", handler.GetByID)

			req := httptest.NewRequest("GET", "/downloads/"+tt.id, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			var resp struct {
				Code int `json:"code"`
			}
			json.Unmarshal(w.Body.Bytes(), &resp)
			assert.Equal(t, tt.wantCode, resp.Code)
		})
	}
}

func TestDownloadHandler_Delete(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		mockGet      func(id uint) (*model.Download, error)
		mockDelete   func(id uint) error
		wantStatus   int
		wantDeleted  bool
	}{
		{
			name: "success",
			id:   "1",
			mockGet: func(id uint) (*model.Download, error) {
				return &model.Download{ID: 1, Title: "Test"}, nil
			},
			mockDelete:  func(id uint) error { return nil },
			wantStatus:  http.StatusOK,
			wantDeleted: true,
		},
		{
			name:         "invalid id format",
			id:           "abc",
			mockGet:      nil,
			mockDelete:   nil,
			wantStatus:   http.StatusBadRequest,
			wantDeleted:  false,
		},
		{
			name: "not found",
			id:   "999",
			mockGet: func(id uint) (*model.Download, error) {
				return nil, errors.New("not found")
			},
			mockDelete:  nil,
			wantStatus:  http.StatusNotFound,
			wantDeleted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockDownloadRepo{
				getByIDFunc: tt.mockGet,
				deleteFunc:  tt.mockDelete,
			}
			handler := NewDownloadHandler(mockRepo, nil, nil)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.DELETE("/downloads/:id", handler.Delete)

			req := httptest.NewRequest("DELETE", "/downloads/"+tt.id, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantDeleted {
				assert.GreaterOrEqual(t, mockRepo.deleteCalls, 1)
			}
		})
	}
}

func TestDownloadHandler_Retry(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		mockGet      func(id uint) (*model.Download, error)
		mockUpdate   func(download *model.Download) error
		wantStatus   int
		wantUpdated  bool
		verifyUpdate func(t *testing.T, download *model.Download)
	}{
		{
			name: "success - resets download for retry",
			id:   "1",
			mockGet: func(id uint) (*model.Download, error) {
				return &model.Download{
					ID:          1,
					Title:       "Test Download",
					Status:      "failed",
					RetryCount:  3,
					TorrentHash: "old-hash",
					TorrentURL:  "http://example.com/torrent",
				}, nil
			},
			mockUpdate: func(download *model.Download) error {
				return nil
			},
			wantStatus:  http.StatusOK,
			wantUpdated: true,
			verifyUpdate: func(t *testing.T, download *model.Download) {
				assert.Equal(t, 0, download.RetryCount)
				assert.Equal(t, "pending", download.Status)
				assert.Equal(t, "", download.TorrentHash)
				assert.Equal(t, "user_retry", download.RetryReason)
			},
		},
		{
			name:       "invalid id format",
			id:         "abc",
			mockGet:    nil,
			mockUpdate: nil,
			wantStatus: http.StatusBadRequest,
			wantUpdated: false,
		},
		{
			name: "not found",
			id:   "999",
			mockGet: func(id uint) (*model.Download, error) {
				return nil, errors.New("not found")
			},
			mockUpdate:  nil,
			wantStatus:  http.StatusNotFound,
			wantUpdated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var updatedDownload *model.Download
			mockRepo := &mockDownloadRepo{
				getByIDFunc: tt.mockGet,
				updateFunc: func(download *model.Download) error {
					updatedDownload = download
					if tt.mockUpdate != nil {
						return tt.mockUpdate(download)
					}
					return nil
				},
			}
			handler := NewDownloadHandler(mockRepo, nil, nil)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/downloads/:id/retry", handler.Retry)

			req := httptest.NewRequest("POST", "/downloads/"+tt.id+"/retry", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantUpdated {
				assert.GreaterOrEqual(t, mockRepo.updateCalls, 1)
				if tt.verifyUpdate != nil && updatedDownload != nil {
					tt.verifyUpdate(t, updatedDownload)
				}
			}
		})
	}
}
