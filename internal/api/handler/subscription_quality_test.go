package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionPreviewPrefers1080FromSavedPolicyWithoutMutations(t *testing.T) {
	fx := newEpisodeCollectionFixture(t)
	sub := fx.createSubscription(t, nil)
	policy := subscriptionmatch.DefaultQualityPolicy()
	policy.Fallback = "allow"
	raw, err := json.Marshal(subscriptionmatch.Rules{Version: 2, Titles: []string{sub.Name}, Season: 1, Language: "any", QualityPolicy: policy})
	require.NoError(t, err)
	require.NoError(t, fx.db.Model(&sub).Update("discovery_rules", string(raw)).Error)
	items := []rss.RSSItem{
		{Title: sub.Name + " - 01 [2160p]", Episode: 1, TorrentURL: "https://example.test/4k.torrent"},
		{Title: sub.Name + " - 01 [1080p]", Episode: 1, TorrentURL: "https://example.test/1080.torrent"},
		{Title: sub.Name + " - 02 [720p]", Episode: 2, TorrentURL: "https://example.test/720.torrent"},
	}
	beforeDownloads, beforeLedgers, beforeCandidates := databaseCounts(t, fx.db)
	w, preview, _ := performEpisodePreviewItems(t, fx.handler, sub, items)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, preview, 3)
	require.Contains(t, preview[0].Title, "1080p")
	require.Equal(t, "download", preview[0].Action)
	require.Equal(t, "skip", preview[1].Action)
	require.Equal(t, "excluded_resolution", preview[2].Reason)
	afterDownloads, afterLedgers, afterCandidates := databaseCounts(t, fx.db)
	require.Equal(t, beforeDownloads, afterDownloads)
	require.Equal(t, beforeLedgers, afterLedgers)
	require.Equal(t, beforeCandidates, afterCandidates)
}
