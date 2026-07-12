package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "backup.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
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
			assertBackupImportNotApplied(t, db)
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
			assertBackupImportNotApplied(t, db)
		})
	}
}

func TestBackupHandlerRejectsTrailingBodyAfterValidJSON(t *testing.T) {
	valid := validBackupImportRequest()
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "oversized whitespace",
			body: valid + strings.Repeat(" ", int(maxBackupRequestBytes)-len(valid)+1),
			want: http.StatusRequestEntityTooLarge,
		},
		{
			name: "second JSON value within limit",
			body: valid + ` {}`,
			want: http.StatusBadRequest,
		},
		{
			name: "second JSON value beyond limit",
			body: valid + strings.Repeat(" ", int(maxBackupRequestBytes)-len(valid)) + `{}`,
			want: http.StatusRequestEntityTooLarge,
		},
	}

	for _, endpoint := range []string{"/backup/preview", "/backup/import"} {
		for _, tt := range tests {
			t.Run(endpoint+"/"+tt.name, func(t *testing.T) {
				router, db := setupBackupHandlerTest(t)
				response := performBackupRequest(router, endpoint, tt.body)
				if response.Code != tt.want {
					t.Fatalf("status = %d, want %d: %s", response.Code, tt.want, response.Body.String())
				}
				assertBackupImportNotApplied(t, db)
			})
		}
	}
}

func TestBackupHandlerRejectsKnownOversizedContentLength(t *testing.T) {
	for _, endpoint := range []string{"/backup/preview", "/backup/import"} {
		t.Run(endpoint, func(t *testing.T) {
			router, db := setupBackupHandlerTest(t)
			response := performBackupRequestWithContentLength(
				router,
				endpoint,
				validBackupImportRequest(),
				maxBackupRequestBytes+1,
			)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413: %s", response.Code, response.Body.String())
			}
			assertBackupImportNotApplied(t, db)
		})
	}
}

func TestBackupHandlerRejectsOversizedChunkedBody(t *testing.T) {
	valid := validBackupImportRequest()
	body := valid + strings.Repeat(" ", int(maxBackupRequestBytes)-len(valid)+1)
	for _, endpoint := range []string{"/backup/preview", "/backup/import"} {
		t.Run(endpoint, func(t *testing.T) {
			router, db := setupBackupHandlerTest(t)
			response := performBackupRequestWithContentLength(router, endpoint, body, -1)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413: %s", response.Code, response.Body.String())
			}
			assertBackupImportNotApplied(t, db)
		})
	}
}

func TestBackupHandlerDoesNotRejectBodyAtExactWrapperLimit(t *testing.T) {
	valid := validBackupImportRequest()
	body := valid + strings.Repeat(" ", int(maxBackupRequestBytes)-len(valid))
	if int64(len(body)) != maxBackupRequestBytes {
		t.Fatalf("body length = %d, want %d", len(body), maxBackupRequestBytes)
	}
	for _, endpoint := range []string{"/backup/preview", "/backup/import"} {
		t.Run(endpoint, func(t *testing.T) {
			router, _ := setupBackupHandlerTest(t)
			response := performBackupRequest(router, endpoint, body)
			if response.Code == http.StatusRequestEntityTooLarge {
				t.Fatalf("exact-limit request was rejected as oversized: %s", response.Body.String())
			}
		})
	}
}

func TestBackupHandlerRejectsMissingData(t *testing.T) {
	for _, endpoint := range []string{"/backup/preview", "/backup/import"} {
		t.Run(endpoint, func(t *testing.T) {
			router, db := setupBackupHandlerTest(t)
			response := performBackupRequest(router, endpoint, `{"source_format":"auto-rss","strategy":"overwrite"}`)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
			assertBackupImportNotApplied(t, db)
		})
	}
}

func performBackupRequest(router *gin.Engine, endpoint, body string) *httptest.ResponseRecorder {
	return performBackupRequestWithContentLength(router, endpoint, body, int64(len(body)))
}

func performBackupRequestWithContentLength(
	router *gin.Engine,
	endpoint string,
	body string,
	contentLength int64,
) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.ContentLength = contentLength
	if contentLength == -1 {
		request.TransferEncoding = []string{"chunked"}
	}
	router.ServeHTTP(response, request)
	return response
}

func validBackupImportRequest() string {
	return `{"source_format":"auto-rss","strategy":"overwrite","data":{"app":"auto-rss","schema_version":"1.0","configs":[{"key":"download_path","value":"/changed"}]}}`
}

func assertBackupImportNotApplied(t *testing.T, db *gorm.DB) {
	t.Helper()
	var cfg model.Config
	if err := db.Where("key = ?", "download_path").First(&cfg).Error; err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Value != "/original" {
		t.Fatalf("rejected request changed config to %q", cfg.Value)
	}
	var count int64
	if err := db.Model(&model.Config{}).Count(&count).Error; err != nil {
		t.Fatalf("count configs: %v", err)
	}
	if count != 1 {
		t.Fatalf("rejected request left %d configs", count)
	}
}
