package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"gorm.io/gorm"
)

func TestMCPAuthAndOrigin(t *testing.T) {
	server := &Server{
		cfg: &config.Config{
			MCPToken:          "secret-token",
			MCPAllowedOrigins: []string{"http://localhost:7892"},
		},
	}

	unauthorized := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if server.authorized(unauthorized) {
		t.Fatal("request without bearer token was authorized")
	}

	authorized := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	authorized.Header.Set("Authorization", "Bearer secret-token")
	if !server.authorized(authorized) {
		t.Fatal("request with matching bearer token was not authorized")
	}

	allowedOrigin := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	allowedOrigin.Header.Set("Origin", "http://localhost:7892")
	if !server.allowedOrigin(allowedOrigin) {
		t.Fatal("configured origin was not allowed")
	}

	blockedOrigin := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	blockedOrigin.Header.Set("Origin", "http://evil.example")
	if server.allowedOrigin(blockedOrigin) {
		t.Fatal("unconfigured origin was allowed")
	}
}

func TestMCPWriteCORSHeaders(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "http://localhost:7892")
	rec := httptest.NewRecorder()

	server.writeCORSHeaders(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:7892" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want configured request origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("Access-Control-Allow-Methods was not set")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestMCPRetryDownloadRejectsNonFailedOrStalledWithoutMutation(t *testing.T) {
	for _, status := range []string{
		model.DownloadStatusCompleted,
		model.DownloadStatusDownloading,
		model.DownloadStatusPending,
		model.DownloadStatusOrganizing,
	} {
		t.Run(status, func(t *testing.T) {
			repo := newFakeMCPDownloadRepo(&model.Download{
				ID:          1,
				Title:       "healthy download",
				Status:      status,
				TorrentURL:  "magnet:?xt=urn:btih:test",
				TorrentHash: "existing-hash",
			})
			qb := &fakeMCPQBClient{}
			server := &Server{downloadRepo: repo, qbClient: qb}

			_, _, err := server.retryDownload(context.Background(), nil, RetryDownloadInput{ID: 1})
			if err == nil {
				t.Fatal("expected retry to reject non-failed/non-stalled download")
			}
			if !strings.Contains(err.Error(), "only failed or stalled downloads") {
				t.Fatalf("error = %q, want failed/stalled precondition", err.Error())
			}
			if repo.updateCalls != 0 {
				t.Fatalf("Update calls = %d, want 0", repo.updateCalls)
			}
			if qb.addCalls != 0 || qb.deleteWithPayloadCalls != 0 {
				t.Fatalf("qBittorrent calls add=%d delete=%d, want 0", qb.addCalls, qb.deleteWithPayloadCalls)
			}
			if got := repo.mustGet(t, 1); got.Status != status || got.TorrentHash != "existing-hash" {
				t.Fatalf("download mutated: status=%q hash=%q", got.Status, got.TorrentHash)
			}
		})
	}
}

func TestMCPRetryDownloadRejectsMissingTorrentURLWithoutMutation(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		torrentURL string
	}{
		{name: "failed empty URL", status: model.DownloadStatusFailed, torrentURL: ""},
		{name: "failed blank URL", status: model.DownloadStatusFailed, torrentURL: " \t "},
		{name: "stalled empty URL", status: model.DownloadStatusStalled, torrentURL: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeMCPDownloadRepo(&model.Download{
				ID:          1,
				Title:       "bad retry",
				Status:      tt.status,
				TorrentURL:  tt.torrentURL,
				TorrentHash: "existing-hash",
				RetryCount:  4,
				LastError:   "previous error",
			})
			qb := &fakeMCPQBClient{}
			server := &Server{downloadRepo: repo, qbClient: qb}

			_, _, err := server.retryDownload(context.Background(), nil, RetryDownloadInput{ID: 1})
			if err == nil {
				t.Fatal("expected retry to reject missing torrent URL")
			}
			if !strings.Contains(err.Error(), "no torrent URL") {
				t.Fatalf("error = %q, want missing torrent URL precondition", err.Error())
			}
			if repo.updateCalls != 0 {
				t.Fatalf("Update calls = %d, want 0", repo.updateCalls)
			}
			if qb.addCalls != 0 || qb.deleteWithPayloadCalls != 0 {
				t.Fatalf("qBittorrent calls add=%d delete=%d, want 0", qb.addCalls, qb.deleteWithPayloadCalls)
			}
			got := repo.mustGet(t, 1)
			if got.Status != tt.status || got.RetryCount != 4 || got.LastError != "previous error" || got.TorrentHash != "existing-hash" {
				t.Fatalf("download mutated: %#v", got)
			}
		})
	}
}

func TestMCPRetryDownloadRejectsMissingRecordWithoutMutation(t *testing.T) {
	repo := newFakeMCPDownloadRepo()
	qb := &fakeMCPQBClient{}
	server := &Server{downloadRepo: repo, qbClient: qb}

	_, _, err := server.retryDownload(context.Background(), nil, RetryDownloadInput{ID: 99})
	if err == nil {
		t.Fatal("expected retry to reject missing record")
	}
	if !strings.Contains(err.Error(), "download 99 was not found") {
		t.Fatalf("error = %q, want missing record error", err.Error())
	}
	if repo.updateCalls != 0 {
		t.Fatalf("Update calls = %d, want 0", repo.updateCalls)
	}
	if qb.addCalls != 0 || qb.deleteWithPayloadCalls != 0 {
		t.Fatalf("qBittorrent calls add=%d delete=%d, want 0", qb.addCalls, qb.deleteWithPayloadCalls)
	}
}

func TestMCPRetryDownloadRecordsQBAddFailureState(t *testing.T) {
	repo := newFakeMCPDownloadRepo(&model.Download{
		ID:          1,
		Title:       "failed download",
		Status:      model.DownloadStatusFailed,
		TorrentURL:  "magnet:?xt=urn:btih:test",
		TorrentHash: "old-hash",
		RetryCount:  2,
		LastError:   "previous error",
	})
	qb := &fakeMCPQBClient{addErr: errors.New("qBittorrent rejected torrent")}
	server := &Server{downloadRepo: repo, qbClient: qb}

	_, _, err := server.retryDownload(context.Background(), nil, RetryDownloadInput{ID: 1})
	if err == nil {
		t.Fatal("expected retry to return qBittorrent add error")
	}
	if !strings.Contains(err.Error(), "qBittorrent could not add the torrent") {
		t.Fatalf("error = %q, want qBittorrent add failure", err.Error())
	}
	if qb.deleteWithPayloadCalls != 1 {
		t.Fatalf("DeleteTorrentWithPayload calls = %d, want 1", qb.deleteWithPayloadCalls)
	}
	if qb.addCalls != 1 {
		t.Fatalf("AddTorrent calls = %d, want 1", qb.addCalls)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("Update calls = %d, want 1", repo.updateCalls)
	}
	got := repo.mustGet(t, 1)
	if got.Status != model.DownloadStatusFailed {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
	if got.LastError != "qBittorrent rejected torrent" {
		t.Fatalf("LastError = %q, want qBittorrent error", got.LastError)
	}
	if got.RetryCount != 0 || got.NextRetryAt != nil || got.TorrentHash != "" {
		t.Fatalf("retry fields not reset before recorded failure: retry=%d next=%v hash=%q", got.RetryCount, got.NextRetryAt, got.TorrentHash)
	}
}

func TestMCPRetryDownloadSuccessResetsFieldsAndUsesFakeQB(t *testing.T) {
	nextRetryAt := time.Now().Add(time.Hour)
	repo := newFakeMCPDownloadRepo(&model.Download{
		ID:             1,
		Title:          "retry me",
		Status:         model.DownloadStatusStalled,
		TorrentURL:     "magnet:?xt=urn:btih:test",
		TorrentHash:    "old-hash",
		RetryCount:     5,
		RetryReason:    "auto_retry",
		NextRetryAt:    &nextRetryAt,
		LastError:      "stalled",
		ErrorMessage:   "stalled",
		Subscription:   model.Subscription{Name: "Test Anime"},
		SubscriptionID: 12,
	})
	qb := &fakeMCPQBClient{addHash: "new-hash"}
	server := &Server{downloadRepo: repo, qbClient: qb}

	_, out, err := server.retryDownload(context.Background(), nil, RetryDownloadInput{ID: 1})
	if err != nil {
		t.Fatalf("retryDownload returned error: %v", err)
	}
	if out.Message != "download retry was queued" {
		t.Fatalf("Message = %q, want queued", out.Message)
	}
	if qb.deleteWithPayloadCalls != 1 || qb.deletedHash != "old-hash" {
		t.Fatalf("delete calls=%d hash=%q, want one old-hash deletion", qb.deleteWithPayloadCalls, qb.deletedHash)
	}
	if qb.addCalls != 1 || qb.addedURL != "magnet:?xt=urn:btih:test" {
		t.Fatalf("add calls=%d url=%q, want one fake add with torrent URL", qb.addCalls, qb.addedURL)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("Update calls = %d, want 1", repo.updateCalls)
	}
	got := repo.mustGet(t, 1)
	if got.Status != model.DownloadStatusDownloading {
		t.Fatalf("Status = %q, want downloading", got.Status)
	}
	if got.RetryCount != 0 || got.RetryReason != "mcp_retry" || got.NextRetryAt != nil {
		t.Fatalf("retry fields = count %d reason %q next %v, want reset mcp_retry nil", got.RetryCount, got.RetryReason, got.NextRetryAt)
	}
	if got.LastError != "" || got.ErrorMessage != "" {
		t.Fatalf("errors = last %q message %q, want cleared", got.LastError, got.ErrorMessage)
	}
	if got.TorrentHash != "new-hash" || out.Download.TorrentHash != "new-hash" {
		t.Fatalf("torrent hash persisted=%q output=%q, want new-hash", got.TorrentHash, out.Download.TorrentHash)
	}
}

type fakeMCPDownloadRepo struct {
	downloads   map[uint]model.Download
	updateCalls int
}

func newFakeMCPDownloadRepo(downloads ...*model.Download) *fakeMCPDownloadRepo {
	repo := &fakeMCPDownloadRepo{downloads: make(map[uint]model.Download)}
	for _, download := range downloads {
		repo.downloads[download.ID] = cloneMCPDownload(*download)
	}
	return repo
}

func (r *fakeMCPDownloadRepo) mustGet(t *testing.T, id uint) model.Download {
	t.Helper()
	download, ok := r.downloads[id]
	if !ok {
		t.Fatalf("download %d not found", id)
	}
	return cloneMCPDownload(download)
}

func (r *fakeMCPDownloadRepo) Create(download *model.Download) error {
	r.downloads[download.ID] = cloneMCPDownload(*download)
	return nil
}

func (r *fakeMCPDownloadRepo) Update(download *model.Download) error {
	r.updateCalls++
	r.downloads[download.ID] = cloneMCPDownload(*download)
	return nil
}

func (r *fakeMCPDownloadRepo) Delete(id uint) error {
	delete(r.downloads, id)
	return nil
}

func (r *fakeMCPDownloadRepo) GetByID(id uint) (*model.Download, error) {
	download, ok := r.downloads[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cloned := cloneMCPDownload(download)
	return &cloned, nil
}

func (r *fakeMCPDownloadRepo) GetByHash(hash string) (*model.Download, error) {
	for _, download := range r.downloads {
		if download.TorrentHash == hash {
			cloned := cloneMCPDownload(download)
			return &cloned, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeMCPDownloadRepo) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	return nil, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	return nil, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	return nil, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) List(offset, limit int, status string) ([]model.Download, int64, error) {
	return nil, 0, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) ListBySubscriptionID(subscriptionID uint) ([]model.Download, error) {
	return nil, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) UpdateStatus(id uint, status string) error {
	return errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) BatchDelete(ids []uint) error {
	return errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) DeleteByStatus(status string) error {
	return errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) DeleteAll() error {
	return errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	return nil, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	return nil, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) CreateInTx(tx *gorm.DB, download *model.Download) error {
	return errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) UpdateInTx(tx *gorm.DB, download *model.Download) error {
	return errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) GetDownloadHistory(filter *repository.DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
	return nil, 0, errUnusedMCPFakeMethod
}

func (r *fakeMCPDownloadRepo) GetDownloadStatistics(days int) (*repository.DownloadStatistics, error) {
	return nil, errUnusedMCPFakeMethod
}

func cloneMCPDownload(download model.Download) model.Download {
	if download.NextRetryAt != nil {
		next := *download.NextRetryAt
		download.NextRetryAt = &next
	}
	if download.DownloadedAt != nil {
		downloaded := *download.DownloadedAt
		download.DownloadedAt = &downloaded
	}
	return download
}

type fakeMCPQBClient struct {
	addHash                string
	addErr                 error
	addCalls               int
	deleteWithPayloadCalls int
	addedURL               string
	deletedHash            string
}

func (c *fakeMCPQBClient) Login(host, username, password string) error {
	return nil
}

func (c *fakeMCPQBClient) TestConnection(host, username, password string) error {
	return nil
}

func (c *fakeMCPQBClient) AddTorrent(torrentURL string, savePath string, category string) (string, error) {
	c.addCalls++
	c.addedURL = torrentURL
	if c.addErr != nil {
		return "", c.addErr
	}
	if c.addHash != "" {
		return c.addHash, nil
	}
	return "fake-hash", nil
}

func (c *fakeMCPQBClient) AddTorrentFile(filename string, fileContent []byte, savePath string, category string) (string, error) {
	return "", nil
}

func (c *fakeMCPQBClient) GetTorrentInfo(hash string) (*downloader.TorrentInfo, error) {
	return nil, nil
}

func (c *fakeMCPQBClient) GetTorrentsByCategory(category string) ([]*downloader.TorrentInfo, error) {
	return nil, nil
}

func (c *fakeMCPQBClient) SetCategory(hash string, category string) error {
	return nil
}

func (c *fakeMCPQBClient) SetLocation(hash string, location string) error {
	return nil
}

func (c *fakeMCPQBClient) RenameTorrentFile(hash string, oldPath string, newPath string) error {
	return nil
}

func (c *fakeMCPQBClient) RemoveTorrentTask(hash string) error {
	return nil
}

func (c *fakeMCPQBClient) DeleteTorrentWithPayload(hash string) error {
	c.deleteWithPayloadCalls++
	c.deletedHash = hash
	return nil
}

func (c *fakeMCPQBClient) GetTorrentFiles(hash string) ([]downloader.TorrentFile, error) {
	return nil, nil
}

func (c *fakeMCPQBClient) GetVersion() (string, error) {
	return "", nil
}

func (c *fakeMCPQBClient) SetProxy(proxyURL string) error {
	return nil
}

func (c *fakeMCPQBClient) DownloadTorrentFile(url string) ([]byte, error) {
	return nil, nil
}

var errUnusedMCPFakeMethod = errors.New("unused fake MCP repository method")
