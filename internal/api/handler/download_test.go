package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
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
