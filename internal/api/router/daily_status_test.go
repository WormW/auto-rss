package router

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/dailystatus"
	"github.com/stretchr/testify/require"
)

func TestDailyStatusRESTAndMCPAgree(t *testing.T) {
	r, _, db := setupRouterForTestWithConfig(t, false, func(cfg *config.Config) {
		cfg.MCPEnabled = true
		cfg.MCPToken = "daily-test-token"
	})
	sub := model.Subscription{Name: "Daily Anime", AirDay: "1", Status: "active", Enabled: true}
	require.NoError(t, db.Create(&sub).Error)
	stamp := time.Date(2026, 9, 27, 16, 0, 0, 0, time.UTC) // Shanghai midnight, included.
	require.NoError(t, db.Create(&model.Download{SubscriptionID: sub.ID, Episode: 1, Title: "ep 1", TorrentHash: "daily-1", CreatedAt: stamp}).Error)
	require.NoError(t, db.Create(&model.Download{SubscriptionID: sub.ID, Episode: 2, Title: "ep 2", TorrentHash: "daily-2", CreatedAt: stamp.Add(24 * time.Hour)}).Error) // Next midnight, excluded.
	rest := performRouterRequest(r, http.MethodGet, "/api/v1/daily/status?date=2026-09-28&timezone=Asia%2FShanghai", nil)
	require.Equal(t, http.StatusOK, rest.Code, rest.Body.String())
	var response struct {
		Data dailystatus.Status `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rest.Body.Bytes(), &response))
	require.Len(t, response.Data.CollectedToday, 1)
	require.Equal(t, 1, response.Data.CollectedToday[0].DownloadCount)
	require.Equal(t, []int{1}, response.Data.CollectedToday[0].Episodes)
	call := performRouterMCPRequest(r, http.MethodPost, "", "Bearer daily-test-token", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_daily_status","arguments":{"date":"2026-09-28","timezone":"Asia/Shanghai"}}}`)
	require.Equal(t, http.StatusOK, call.Code, call.Body.String())
	var rpc struct {
		Error  any `json:"error"`
		Result struct {
			IsError           bool               `json:"isError"`
			StructuredContent dailystatus.Status `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(call.Body.Bytes(), &rpc))
	require.Nil(t, rpc.Error)
	require.False(t, rpc.Result.IsError, call.Body.String())
	require.Equal(t, response.Data, rpc.Result.StructuredContent)
	for _, path := range []string{"/api/v1/daily/status?date=2026-02-30", "/api/v1/daily/status?timezone=bad-zone"} {
		require.Equal(t, http.StatusBadRequest, performRouterRequest(r, http.MethodGet, path, nil).Code)
	}
}

func TestDailyStatusRequiresRESTAuthenticationWhenEnabled(t *testing.T) {
	r, _ := setupRouterForTest(t, true)
	require.Equal(t, http.StatusUnauthorized, performRouterRequest(r, http.MethodGet, "/api/v1/daily/status", nil).Code)
}
