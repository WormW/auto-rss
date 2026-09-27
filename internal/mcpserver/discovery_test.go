package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/pkg/database"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscriptiondiscovery"
	"github.com/stretchr/testify/require"
)

type discoveryProviderFixture struct{}

func (discoveryProviderFixture) Search(context.Context, subscriptiondiscovery.PrepareInput) ([]subscriptiondiscovery.Subject, error) {
	return []subscriptiondiscovery.Subject{{ID: 42, Name: "Anime", Aliases: []string{"Anime"}}}, nil
}
func (discoveryProviderFixture) Subject(context.Context, int) (subscriptiondiscovery.Subject, error) {
	return subscriptiondiscovery.Subject{ID: 42, Name: "Anime", Aliases: []string{"Anime"}, Episodes: 12}, nil
}
func (discoveryProviderFixture) Feeds(context.Context, subscriptiondiscovery.Subject, subscriptiondiscovery.PrepareInput) ([]subscriptiondiscovery.Feed, []string, error) {
	return []subscriptiondiscovery.Feed{{ID: "choice", Source: "nyaa", URL: "https://nyaa.si/?page=rss&q=Anime"}}, nil, nil
}
func (discoveryProviderFixture) Fetch(context.Context, subscriptiondiscovery.Feed) ([]rss.RSSItem, error) {
	return []rss.RSSItem{{Title: "[Group] Anime - 01 [1080p]", Episode: 1, TorrentURL: "https://example.test/1.torrent"}}, nil
}

func TestMCPDiscoveryTransportPreparesAndConfirmsReviewedRevision(t *testing.T) {
	db, err := database.Init(filepath.Join(t.TempDir(), "discovery.db"))
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	s := New(Dependencies{DB: db, Config: &config.Config{MCPToken: "test-token"}, Discovery: subscriptiondiscovery.New(db, discoveryProviderFixture{})})
	handler := s.Handler()
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	require.Equal(t, 401, unauthorized.Code)
	call := func(method string, params any) map[string]json.RawMessage {
		payload, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		require.NoError(t, e)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		require.Equal(t, 200, response.Code, response.Body.String())
		var envelope struct {
			Result map[string]json.RawMessage `json:"result"`
			Error  json.RawMessage            `json:"error"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.Empty(t, envelope.Error, response.Body.String())
		return envelope.Result
	}
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "discovery-test", "version": "1"}})
	toolCall := func(name string, args any, out any) {
		result := call("tools/call", map[string]any{"name": name, "arguments": args})
		require.NotEqual(t, "true", string(result["isError"]))
		var content []struct {
			Text string `json:"text"`
		}
		require.NoError(t, json.Unmarshal(result["content"], &content))
		require.NotEmpty(t, content)
		require.NoError(t, json.Unmarshal([]byte(content[0].Text), out))
	}
	var draft subscriptiondiscovery.Draft
	toolCall("prepare_subscription", subscriptiondiscovery.PrepareInput{Query: "Anime", BangumiID: 42, Resolution: 1080}, &draft)
	require.Equal(t, "any", draft.Intent.Language)
	require.True(t, draft.Feeds[0].Confirmable)
	var count int64
	require.NoError(t, db.Table("subscriptions").Count(&count).Error)
	require.Zero(t, count)
	input := subscriptiondiscovery.ConfirmInput{DraftID: draft.ID, Revision: draft.Revision, FeedID: "choice"}
	var created, repeated CreateSubscriptionOutput
	toolCall("confirm_subscription", input, &created)
	toolCall("confirm_subscription", input, &repeated)
	require.NotZero(t, created.Subscription.ID)
	require.Equal(t, created.Subscription.ID, repeated.Subscription.ID)
	require.NoError(t, db.Table("subscriptions").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Table("downloads").Count(&count).Error)
	require.Zero(t, count)
}
