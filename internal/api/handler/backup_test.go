package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	backupservice "github.com/WormW/auto-rss/internal/service/backup"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupBackupHandlerTest(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Config{},
		&model.RSSSource{},
		&model.Subscription{},
		&model.SubscriptionGroup{},
		&model.SubscriptionTag{},
		&model.SubscriptionTagRelation{},
		&model.NotificationSetting{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	if err := db.Create(&model.Config{Key: "download_path", Value: "/original"}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}

	handler := NewBackupHandler(backupservice.NewService(db))
	router := gin.New()
	router.POST("/backup/preview", handler.Preview)
	router.POST("/backup/import", handler.Import)
	return router, db
}

func TestBackupHandlerMapsServicePackageLimitToRequestEntityTooLarge(t *testing.T) {
	for _, endpoint := range []string{"/backup/preview", "/backup/import"} {
		t.Run(endpoint, func(t *testing.T) {
			router, db := setupBackupHandlerTest(t)
			padding := strings.Repeat("x", backupservice.MaxPackageBytes)
			body := `{"source_format":"auto-rss","strategy":"overwrite","data":{"app":"auto-rss","schema_version":"1.0","padding":"` + padding + `"}}`
			if int64(len(body)) >= maxBackupRequestBytes {
				t.Fatalf("service-limit body %d must remain below HTTP limit %d", len(body), maxBackupRequestBytes)
			}

			response := performBackupRequest(router, endpoint, body)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413: %s", response.Code, response.Body.String())
			}
			assertBackupConfigUnchanged(t, db)
		})
	}
}

func TestBackupHandlerRejectsOversizedWrapperBeforeBindingOrService(t *testing.T) {
	for _, endpoint := range []string{"/backup/preview", "/backup/import"} {
		t.Run(endpoint, func(t *testing.T) {
			router, db := setupBackupHandlerTest(t)
			data := `{"app":"auto-rss","schema_version":"1.0","configs":[{"key":"download_path","value":"/changed"}]}`
			body := `{"source_format":"auto-rss","strategy":"overwrite","data":` + data + `,"padding":"` +
				strings.Repeat("x", int(maxBackupRequestBytes)) + `"}`

			response := performBackupRequest(router, endpoint, body)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413: %s", response.Code, response.Body.String())
			}
			assertBackupConfigUnchanged(t, db)
		})
	}
}

func performBackupRequest(router *gin.Engine, endpoint, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	return response
}

func assertBackupConfigUnchanged(t *testing.T, db *gorm.DB) {
	t.Helper()
	var cfg model.Config
	if err := db.Where("key = ?", "download_path").First(&cfg).Error; err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Value != "/original" {
		t.Fatalf("oversized request changed config to %q", cfg.Value)
	}
	var count int64
	if err := db.Model(&model.Config{}).Count(&count).Error; err != nil {
		t.Fatalf("count configs: %v", err)
	}
	if count != 1 {
		t.Fatalf("oversized request left %d configs", count)
	}
}
