package handler

import (
	"bytes"
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

// mockSubscriptionRepo is a mock implementation of SubscriptionRepository for testing
type mockSubscriptionRepo struct {
	createFunc       func(subscription *model.Subscription) error
	updateFunc       func(subscription *model.Subscription) error
	deleteFunc       func(id uint) error
	getByIDFunc      func(id uint) (*model.Subscription, error)
	getByRSSURLFunc  func(rssURL string) (*model.Subscription, error)
	listFunc         func(offset, limit int) ([]model.Subscription, int64, error)
	getActiveFunc    func() ([]model.Subscription, error)
	updateInTxFunc   func(tx *gorm.DB, subscription *model.Subscription) error
	getWithCountFunc func() ([]repository.SubscriptionWithStats, error)

	listCalls    int
	getByIDCalls int
	createCalls  int
}

func (m *mockSubscriptionRepo) Create(subscription *model.Subscription) error {
	m.createCalls++
	if m.createFunc != nil {
		return m.createFunc(subscription)
	}
	return nil
}

func (m *mockSubscriptionRepo) Update(subscription *model.Subscription) error {
	if m.updateFunc != nil {
		return m.updateFunc(subscription)
	}
	return nil
}

func (m *mockSubscriptionRepo) Delete(id uint) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(id)
	}
	return nil
}

func (m *mockSubscriptionRepo) GetByID(id uint) (*model.Subscription, error) {
	m.getByIDCalls++
	if m.getByIDFunc != nil {
		return m.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}

func (m *mockSubscriptionRepo) GetByRSSURL(rssURL string) (*model.Subscription, error) {
	if m.getByRSSURLFunc != nil {
		return m.getByRSSURLFunc(rssURL)
	}
	return nil, errors.New("not found")
}

func (m *mockSubscriptionRepo) List(offset, limit int) ([]model.Subscription, int64, error) {
	m.listCalls++
	if m.listFunc != nil {
		return m.listFunc(offset, limit)
	}
	return []model.Subscription{}, 0, nil
}

func (m *mockSubscriptionRepo) GetActiveSubscriptions() ([]model.Subscription, error) {
	if m.getActiveFunc != nil {
		return m.getActiveFunc()
	}
	return nil, nil
}

func (m *mockSubscriptionRepo) UpdateInTx(tx *gorm.DB, subscription *model.Subscription) error {
	if m.updateInTxFunc != nil {
		return m.updateInTxFunc(tx, subscription)
	}
	return nil
}

func (m *mockSubscriptionRepo) GetSubscriptionsWithDownloadCount() ([]repository.SubscriptionWithStats, error) {
	if m.getWithCountFunc != nil {
		return m.getWithCountFunc()
	}
	return nil, errors.New("not implemented")
}

func TestSubscriptionHandler_List(t *testing.T) {
	tests := []struct {
		name       string
		mockGet    func() ([]repository.SubscriptionWithStats, error)
		wantStatus int
		wantCount  int
	}{
		{
			name: "success with subscriptions",
			mockGet: func() ([]repository.SubscriptionWithStats, error) {
				return []repository.SubscriptionWithStats{
					{Subscription: model.Subscription{ID: 1, Name: "Anime 1"}, DownloadingCount: 2},
					{Subscription: model.Subscription{ID: 2, Name: "Anime 2"}, DownloadingCount: 0},
				}, nil
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name: "empty list",
			mockGet: func() ([]repository.SubscriptionWithStats, error) {
				return []repository.SubscriptionWithStats{}, nil
			},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name: "repository error",
			mockGet: func() ([]repository.SubscriptionWithStats, error) {
				return nil, errors.New("database error")
			},
			wantStatus: http.StatusInternalServerError,
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockSubscriptionRepo{getWithCountFunc: tt.mockGet}
			handler := NewSubscriptionHandler(mockRepo, nil, nil, nil, "")

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.GET("/subscriptions", handler.List)

			req := httptest.NewRequest("GET", "/subscriptions", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK && tt.wantCount > 0 {
				var resp struct {
					Code int `json:"code"`
					Data struct {
						List []repository.SubscriptionWithStats `json:"list"`
					} `json:"data"`
				}
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, 0, resp.Code)
				assert.Len(t, resp.Data.List, tt.wantCount)
			}
		})
	}
}

func TestSubscriptionHandler_GetByID(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		mockGet    func(id uint) (*model.Subscription, error)
		wantStatus int
	}{
		{
			name: "success",
			id:   "1",
			mockGet: func(id uint) (*model.Subscription, error) {
				assert.Equal(t, uint(1), id)
				return &model.Subscription{
					ID:   1,
					Name: "Test Subscription",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			mockGet:    nil,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "not found",
			id:   "999",
			mockGet: func(id uint) (*model.Subscription, error) {
				return nil, errors.New("not found")
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockSubscriptionRepo{getByIDFunc: tt.mockGet}
			handler := NewSubscriptionHandler(mockRepo, nil, nil, nil, "")

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.GET("/subscriptions/:id", handler.GetByID)

			req := httptest.NewRequest("GET", "/subscriptions/"+tt.id, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestSubscriptionHandler_Create(t *testing.T) {
	tests := []struct {
		name         string
		body         map[string]interface{}
		mockGetByURL func(rssURL string) (*model.Subscription, error)
		mockCreate   func(sub *model.Subscription) error
		wantStatus   int
		wantCreated  bool
	}{
		{
			name: "success",
			body: map[string]interface{}{
				"name":    "New Anime",
				"rss_url": "http://example.com/rss",
				"season":  1,
			},
			mockGetByURL: func(rssURL string) (*model.Subscription, error) {
				return nil, gorm.ErrRecordNotFound // URL not in use
			},
			mockCreate: func(sub *model.Subscription) error {
				sub.ID = 1
				return nil
			},
			wantStatus:  http.StatusOK,
			wantCreated: true,
		},
		{
			name: "duplicate rss url",
			body: map[string]interface{}{
				"name":    "New Anime",
				"rss_url": "http://example.com/rss",
			},
			mockGetByURL: func(rssURL string) (*model.Subscription, error) {
				return &model.Subscription{ID: 1, RssURL: rssURL}, nil
			},
			mockCreate:  nil,
			wantStatus:  http.StatusConflict,
			wantCreated: false,
		},
		{
			name: "invalid body",
			body: map[string]interface{}{
				"name": 123, // wrong type
			},
			mockGetByURL: nil,
			mockCreate:   nil,
			wantStatus:   http.StatusBadRequest,
			wantCreated:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockSubscriptionRepo{
				getByRSSURLFunc: tt.mockGetByURL,
				createFunc:      tt.mockCreate,
			}
			handler := NewSubscriptionHandler(mockRepo, nil, nil, nil, "")

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/subscriptions", handler.Create)

			bodyJSON, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/subscriptions", bytes.NewBuffer(bodyJSON))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantCreated {
				assert.GreaterOrEqual(t, mockRepo.createCalls, 1)
			}
		})
	}
}
