package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/recovery"
	"gorm.io/driver/sqlite"
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

func TestMCPToolRegistryAnnotationsMatchSafetyPolicy(t *testing.T) {
	server := New(Dependencies{})
	tools := registeredMCPToolsByName(t, server.registeredMCPTools())

	readOnlyTools := []string{
		"get_system_overview",
		"list_subscriptions",
		"get_subscription",
		"list_downloads",
		"get_download",
		"preview_recovery_scan",
		"search_mikan",
		"get_mikan_season",
		"get_mikan_fansubs",
		"search_bangumi",
		"get_bangumi_subject",
		"get_calendar",
		"get_disk_status",
		"list_logs",
	}
	writeTools := []string{
		"create_subscription",
		"toggle_subscription",
		"retry_download",
		"refresh_rss",
	}

	if got, want := len(tools), len(readOnlyTools)+len(writeTools); got != want {
		t.Fatalf("registered MCP tool count = %d, want %d; update the annotation policy test when adding tools", got, want)
	}
	for _, name := range readOnlyTools {
		assertMCPToolAnnotations(t, tools[name], true)
	}
	for _, name := range writeTools {
		assertMCPToolAnnotations(t, tools[name], false)
	}
}

func TestMCPPreviewRecoveryToolRegistryIsPreviewOnly(t *testing.T) {
	server := New(Dependencies{})
	tools := registeredMCPToolsByName(t, server.registeredMCPTools())
	preview := tools["preview_recovery_scan"]

	description := strings.ToLower(preview.Tool.Description)
	for _, phrase := range []string{"preview", "dry-run", "read-only", "never applies"} {
		if !strings.Contains(description, phrase) {
			t.Fatalf("preview_recovery_scan description = %q, want phrase %q", preview.Tool.Description, phrase)
		}
	}

	assertFieldSet(t, preview.InputFields, []string{"subscription_id"})
	for _, field := range preview.InputFields {
		normalized := strings.ToLower(field)
		if strings.Contains(normalized, "dry") || strings.Contains(normalized, "apply") {
			t.Fatalf("preview_recovery_scan input exposes dry-run/apply control: %q", field)
		}
	}
	for _, field := range []string{"dry_run", "preview_only", "applied"} {
		if !hasField(preview.OutputFields, field) {
			t.Fatalf("preview_recovery_scan output fields = %v, want %q", preview.OutputFields, field)
		}
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
			if qb.addCalls != 0 || qb.removeTaskCalls != 0 || qb.deleteWithPayloadCalls != 0 {
				t.Fatalf("qBittorrent calls add=%d remove=%d delete=%d, want 0", qb.addCalls, qb.removeTaskCalls, qb.deleteWithPayloadCalls)
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
			if qb.addCalls != 0 || qb.removeTaskCalls != 0 || qb.deleteWithPayloadCalls != 0 {
				t.Fatalf("qBittorrent calls add=%d remove=%d delete=%d, want 0", qb.addCalls, qb.removeTaskCalls, qb.deleteWithPayloadCalls)
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
	if qb.addCalls != 0 || qb.removeTaskCalls != 0 || qb.deleteWithPayloadCalls != 0 {
		t.Fatalf("qBittorrent calls add=%d remove=%d delete=%d, want 0", qb.addCalls, qb.removeTaskCalls, qb.deleteWithPayloadCalls)
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
	if qb.removeTaskCalls != 1 || qb.removedHash != "old-hash" {
		t.Fatalf("RemoveTorrentTask calls=%d hash=%q, want one old-hash removal", qb.removeTaskCalls, qb.removedHash)
	}
	if qb.deleteWithPayloadCalls != 0 {
		t.Fatalf("DeleteTorrentWithPayload calls = %d, want 0", qb.deleteWithPayloadCalls)
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
	if qb.removeTaskCalls != 1 || qb.removedHash != "old-hash" {
		t.Fatalf("remove calls=%d hash=%q, want one old-hash task removal", qb.removeTaskCalls, qb.removedHash)
	}
	if qb.deleteWithPayloadCalls != 0 {
		t.Fatalf("DeleteTorrentWithPayload calls = %d, want 0", qb.deleteWithPayloadCalls)
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

func TestMCPPreviewRecoveryScanAlwaysDryRunAndBoundsOutput(t *testing.T) {
	t.Setenv("AUTO_RSS_ENABLE_RECOVERY_APPLY", "true")
	server, db, sub, existing, missing := newMCPRecoveryFixture(t, 12)

	_, out, err := server.previewRecoveryScan(context.Background(), nil, PreviewRecoveryInput{})
	if err != nil {
		t.Fatalf("previewRecoveryScan returned error: %v", err)
	}

	if !out.DryRun || !out.PreviewOnly || out.Applied {
		t.Fatalf("preview flags = dry_run %v preview_only %v applied %v, want true true false", out.DryRun, out.PreviewOnly, out.Applied)
	}
	if out.ScannedFiles != 17 || out.MatchedFiles != 4 {
		t.Fatalf("scan counts = scanned %d matched %d, want 17 and 4", out.ScannedFiles, out.MatchedFiles)
	}
	if out.OrphanFileCount != 13 || len(out.OrphanFileSamples) != recoveryPreviewSampleLimit || out.OrphanFileOmittedCount != 3 {
		t.Fatalf("orphan summary = count %d samples %d omitted %d, want 13 samples capped at %d omitted 3", out.OrphanFileCount, len(out.OrphanFileSamples), out.OrphanFileOmittedCount, recoveryPreviewSampleLimit)
	}
	if out.SubscriptionCount != 1 || len(out.Subscriptions) != 1 {
		t.Fatalf("subscription summary count=%d len=%d, want 1", out.SubscriptionCount, len(out.Subscriptions))
	}

	preview := out.Subscriptions[0]
	if preview.SubscriptionID != sub.ID || preview.Name != "Fixture Show" {
		t.Fatalf("subscription preview = id %d name %q, want fixture subscription", preview.SubscriptionID, preview.Name)
	}
	if preview.DownloadsToUpdateCount != 1 || preview.DownloadsToCreateCount != 2 || preview.DownloadsMissingCount != 1 {
		t.Fatalf("subscription candidate counts = update %d create %d missing %d, want 1, 2, 1", preview.DownloadsToUpdateCount, preview.DownloadsToCreateCount, preview.DownloadsMissingCount)
	}
	if out.DownloadsToUpdateCount != 1 || out.DownloadsToCreateCount != 2 || out.DownloadsMissingCount != 1 {
		t.Fatalf("total candidate counts = update %d create %d missing %d, want 1, 2, 1", out.DownloadsToUpdateCount, out.DownloadsToCreateCount, out.DownloadsMissingCount)
	}

	var afterSub model.Subscription
	if err := db.First(&afterSub, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if afterSub.CurrentEpisode != 1 || afterSub.LatestEpisode != 2 {
		t.Fatalf("subscription was mutated by preview: current=%d latest=%d", afterSub.CurrentEpisode, afterSub.LatestEpisode)
	}

	var afterExisting model.Download
	if err := db.First(&afterExisting, existing.ID).Error; err != nil {
		t.Fatalf("reload existing download: %v", err)
	}
	if afterExisting.Status != model.DownloadStatusDownloading || afterExisting.RenamedPath != "" {
		t.Fatalf("existing download was mutated by preview: status=%q renamed_path=%q", afterExisting.Status, afterExisting.RenamedPath)
	}

	var afterMissing model.Download
	if err := db.First(&afterMissing, missing.ID).Error; err != nil {
		t.Fatalf("reload missing download: %v", err)
	}
	if afterMissing.Status != model.DownloadStatusCompleted || afterMissing.RenamedPath == "" {
		t.Fatalf("missing download was unexpectedly changed: status=%q renamed_path=%q", afterMissing.Status, afterMissing.RenamedPath)
	}
}

func TestMCPPreviewRecoveryScanRejectsInvalidSubscriptionID(t *testing.T) {
	server, _, _, _, _ := newMCPRecoveryFixture(t, 0)

	_, _, err := server.previewRecoveryScan(context.Background(), nil, PreviewRecoveryInput{SubscriptionID: 999})
	if err == nil {
		t.Fatal("expected invalid subscription_id to return an error")
	}
	if !strings.Contains(err.Error(), "subscription 999 not found") {
		t.Fatalf("error = %q, want subscription not found", err.Error())
	}
}

func TestMCPPreviewRecoveryScanPropagatesScannerErrors(t *testing.T) {
	server, db, sub, existing, missing := newMCPRecoveryFixture(t, 0)
	missingRoot := filepath.Join(t.TempDir(), "does-not-exist")
	if err := server.configRepo.Set("download_path", missingRoot); err != nil {
		t.Fatalf("set missing download_path: %v", err)
	}

	_, _, err := server.previewRecoveryScan(context.Background(), nil, PreviewRecoveryInput{})
	if err == nil {
		t.Fatal("expected scanner error for missing scan root")
	}
	if !strings.Contains(err.Error(), "failed to preview recovery scan") || !strings.Contains(err.Error(), "failed to walk directory") {
		t.Fatalf("error = %q, want wrapped scanner walk error", err.Error())
	}

	var afterSub model.Subscription
	if err := db.First(&afterSub, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if afterSub.CurrentEpisode != 1 || afterSub.LatestEpisode != 2 {
		t.Fatalf("subscription was mutated after scanner error: current=%d latest=%d", afterSub.CurrentEpisode, afterSub.LatestEpisode)
	}

	var afterExisting model.Download
	if err := db.First(&afterExisting, existing.ID).Error; err != nil {
		t.Fatalf("reload existing download: %v", err)
	}
	if afterExisting.Status != model.DownloadStatusDownloading || afterExisting.RenamedPath != "" {
		t.Fatalf("existing download was mutated after scanner error: status=%q renamed_path=%q", afterExisting.Status, afterExisting.RenamedPath)
	}

	var afterMissing model.Download
	if err := db.First(&afterMissing, missing.ID).Error; err != nil {
		t.Fatalf("reload missing download: %v", err)
	}
	if afterMissing.Status != model.DownloadStatusCompleted || afterMissing.RenamedPath == "" {
		t.Fatalf("missing download was unexpectedly changed after scanner error: status=%q renamed_path=%q", afterMissing.Status, afterMissing.RenamedPath)
	}
}

func TestMCPRecoveryPreviewSummaryBoundsAllSampleFields(t *testing.T) {
	limit := recoveryPreviewSampleLimit
	overflow := limit + 2

	result := &recovery.ScanResult{
		ScannedFiles: 100,
		MatchedFiles: 80,
		OrphanFiles:  numberedStrings("orphan", overflow),
		Applied:      false,
		Subscriptions: []recovery.SubscriptionScanResult{
			{
				SubscriptionID:    42,
				Name:              "Large Fixture",
				CurrentEpisodeOld: 1,
				CurrentEpisodeNew: overflow,
				LatestEpisodeOld:  1,
				LatestEpisodeNew:  overflow,
				EpisodesOnDisk:    numberedInts(overflow),
				MatchedEpisodes:   numberedEpisodeFiles(overflow),
				DownloadsToUpdate: numberedUints(overflow),
				DownloadsToCreate: numberedInts(overflow),
				DownloadsMissing:  numberedUints(overflow),
			},
		},
	}

	out := summarizeRecoveryPreview(result)

	if !out.DryRun || !out.PreviewOnly || out.Applied {
		t.Fatalf("preview flags = dry_run %v preview_only %v applied %v, want true true false", out.DryRun, out.PreviewOnly, out.Applied)
	}
	if out.OrphanFileCount != overflow || len(out.OrphanFileSamples) != limit || out.OrphanFileOmittedCount != 2 {
		t.Fatalf("orphan bounds = count %d samples %d omitted %d, want %d %d 2", out.OrphanFileCount, len(out.OrphanFileSamples), out.OrphanFileOmittedCount, overflow, limit)
	}
	if out.DownloadsToUpdateCount != overflow || out.DownloadsToCreateCount != overflow || out.DownloadsMissingCount != overflow {
		t.Fatalf("total candidate counts = update %d create %d missing %d, want %d", out.DownloadsToUpdateCount, out.DownloadsToCreateCount, out.DownloadsMissingCount, overflow)
	}

	if out.SubscriptionCount != 1 || len(out.Subscriptions) != 1 {
		t.Fatalf("subscription summary count=%d len=%d, want 1", out.SubscriptionCount, len(out.Subscriptions))
	}
	preview := out.Subscriptions[0]
	if preview.EpisodesOnDiskCount != overflow || len(preview.EpisodeSamples) != limit || preview.EpisodeOmittedCount != 2 {
		t.Fatalf("episode bounds = count %d samples %d omitted %d, want %d %d 2", preview.EpisodesOnDiskCount, len(preview.EpisodeSamples), preview.EpisodeOmittedCount, overflow, limit)
	}
	if preview.MatchedFileCount != overflow {
		t.Fatalf("MatchedFileCount = %d, want %d", preview.MatchedFileCount, overflow)
	}
	if preview.DownloadsToUpdateCount != overflow || len(preview.DownloadsToUpdateIDs) != limit {
		t.Fatalf("update bounds = count %d samples %d, want %d %d", preview.DownloadsToUpdateCount, len(preview.DownloadsToUpdateIDs), overflow, limit)
	}
	if preview.DownloadsToCreateCount != overflow || len(preview.DownloadsToCreate) != limit {
		t.Fatalf("create bounds = count %d samples %d, want %d %d", preview.DownloadsToCreateCount, len(preview.DownloadsToCreate), overflow, limit)
	}
	if preview.DownloadsMissingCount != overflow || len(preview.DownloadsMissingIDs) != limit {
		t.Fatalf("missing bounds = count %d samples %d, want %d %d", preview.DownloadsMissingCount, len(preview.DownloadsMissingIDs), overflow, limit)
	}
}

func TestMCPRecoveryPreviewInputDoesNotExposeApplyMode(t *testing.T) {
	inputType := reflect.TypeOf(PreviewRecoveryInput{})
	for i := 0; i < inputType.NumField(); i++ {
		field := inputType.Field(i)
		for _, fieldPart := range []string{field.Name, field.Tag.Get("json")} {
			normalized := strings.ToLower(fieldPart)
			if strings.Contains(normalized, "dry") || strings.Contains(normalized, "apply") {
				t.Fatalf("PreviewRecoveryInput exposes dry-run/apply control in field %s: %q", field.Name, fieldPart)
			}
		}
	}

	input := PreviewRecoveryInput{SubscriptionID: 7}
	if input.SubscriptionID != 7 {
		t.Fatalf("SubscriptionID = %d, want 7", input.SubscriptionID)
	}
}

func registeredMCPToolsByName(t *testing.T, definitions []registeredMCPTool) map[string]registeredMCPTool {
	t.Helper()
	tools := make(map[string]registeredMCPTool, len(definitions))
	for _, definition := range definitions {
		name := definition.Tool.Name
		if name == "" {
			t.Fatal("registered MCP tool had empty name")
		}
		if _, exists := tools[name]; exists {
			t.Fatalf("registered MCP tool %q was duplicated", name)
		}
		tools[name] = definition
	}
	return tools
}

func assertMCPToolAnnotations(t *testing.T, definition registeredMCPTool, wantReadOnly bool) {
	t.Helper()
	tool := definition.Tool
	if tool.Name == "" {
		t.Fatal("missing registered MCP tool")
	}
	if tool.Description == "" {
		t.Fatalf("%s description was empty", tool.Name)
	}
	if tool.Annotations == nil {
		t.Fatalf("%s annotations were nil", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint != wantReadOnly {
		t.Fatalf("%s ReadOnlyHint = %v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, wantReadOnly)
	}
	if tool.Annotations.DestructiveHint == nil {
		t.Fatalf("%s DestructiveHint was nil", tool.Name)
	}
	if *tool.Annotations.DestructiveHint != !wantReadOnly {
		t.Fatalf("%s DestructiveHint = %v, want %v", tool.Name, *tool.Annotations.DestructiveHint, !wantReadOnly)
	}
	if tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
		t.Fatalf("%s OpenWorldHint = %v, want true", tool.Name, tool.Annotations.OpenWorldHint)
	}
}

func assertFieldSet(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for _, field := range want {
		if !hasField(got, field) {
			t.Fatalf("fields = %v, want %q", got, field)
		}
	}
}

func hasField(fields []string, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
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
	removeTaskCalls        int
	deleteWithPayloadCalls int
	addedURL               string
	removedHash            string
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
func (c *fakeMCPQBClient) AddTorrentExclusive(torrentURL, savePath, category, expectedHash string) (string, error) {
	return c.AddTorrent(torrentURL, savePath, category)
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

func (c *fakeMCPQBClient) PauseTorrent(hash string) error  { return nil }
func (c *fakeMCPQBClient) ResumeTorrent(hash string) error { return nil }

func (c *fakeMCPQBClient) RemoveTorrentTask(hash string) error {
	c.removeTaskCalls++
	c.removedHash = hash
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

func newMCPRecoveryFixture(t *testing.T, orphanCount int) (*Server, *gorm.DB, model.Subscription, model.Download, model.Download) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	root := t.TempDir()
	requireMCPFile(t, filepath.Join(root, "Fixture Show", "Season 1", "Fixture Show S01E02.mkv"))
	requireMCPFile(t, filepath.Join(root, "Fixture Show", "Raw", "[Fansub] Fixture Show [01][1080p].mkv"))
	requireMCPFile(t, filepath.Join(root, "Fixture Show", "Raw", "Fixture Show bonus footage.mkv"))
	requireMCPFile(t, filepath.Join(root, "Fixture Show", "Nested", "Season 1", "Fixture Show S01E03.mkv"))
	requireMCPFile(t, filepath.Join(root, "Fixture Show", "Nested", "Season 1", "Copy", "Fixture Show S01E03 duplicate.mkv"))
	for i := 0; i < orphanCount; i++ {
		requireMCPFile(t, filepath.Join(root, "Unknown Show", "Unknown Show S01E"+leftPadMCP(i+1)+".mkv"))
	}

	sub := model.Subscription{
		Name:           "Fixture Show",
		Season:         1,
		CurrentEpisode: 1,
		LatestEpisode:  2,
		Enabled:        true,
		Status:         "active",
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	existing := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Fixture Show 02",
		Episode:        2,
		TorrentURL:     "memory://existing",
		TorrentHash:    "existing-02",
		Status:         model.DownloadStatusDownloading,
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create existing download: %v", err)
	}

	missing := model.Download{
		SubscriptionID: sub.ID,
		Title:          "Fixture Show 04",
		Episode:        4,
		TorrentURL:     "memory://missing",
		TorrentHash:    "missing-04",
		RenamedPath:    filepath.Join(root, "Fixture Show", "Season 1", "Fixture Show S01E04.mkv"),
		Status:         model.DownloadStatusCompleted,
	}
	if err := db.Create(&missing).Error; err != nil {
		t.Fatalf("create missing download: %v", err)
	}

	configRepo := repository.NewConfigRepository(db)
	if err := configRepo.Set("download_path", root); err != nil {
		t.Fatalf("set download_path: %v", err)
	}

	server := &Server{
		cfg:              &config.Config{DownloadPath: root},
		db:               db,
		subscriptionRepo: repository.NewSubscriptionRepository(db),
		downloadRepo:     repository.NewDownloadRepository(db),
		configRepo:       configRepo,
	}
	return server, db, sub, existing, missing
}

func requireMCPFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func leftPadMCP(n int) string {
	return fmt.Sprintf("%02d", n)
}

func numberedStrings(prefix string, count int) []string {
	items := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		items = append(items, fmt.Sprintf("%s-%02d", prefix, i))
	}
	return items
}

func numberedInts(count int) []int {
	items := make([]int, 0, count)
	for i := 1; i <= count; i++ {
		items = append(items, i)
	}
	return items
}

func numberedUints(count int) []uint {
	items := make([]uint, 0, count)
	for i := 1; i <= count; i++ {
		items = append(items, uint(i))
	}
	return items
}

func numberedEpisodeFiles(count int) []recovery.EpisodeFile {
	files := make([]recovery.EpisodeFile, 0, count)
	for i := 1; i <= count; i++ {
		files = append(files, recovery.EpisodeFile{
			Path:    fmt.Sprintf("/fixture/episode-%02d.mkv", i),
			Episode: i,
			Season:  1,
		})
	}
	return files
}
