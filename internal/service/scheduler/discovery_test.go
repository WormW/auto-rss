package scheduler

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
	"github.com/stretchr/testify/require"
)

func TestDMHYParsedMagnetPassesSizeGuardAndConfirmedQuality(t *testing.T) {
	data, err := os.ReadFile("../rss/testdata/dmhy.xml")
	require.NoError(t, err)
	items, err := rss.NewParser().Parse(bytes.NewReader(data))
	require.NoError(t, err)
	for i := range items {
		items[i].PubTime = time.Now().UTC()
	}
	fx := newSchedulerLedgerFixture(t, nil)
	sub := fx.createSubscription(t)
	feed := fx.defaultFeed(t, sub.ID)
	raw, err := json.Marshal(subscriptionmatch.Rules{Version: 2, Titles: []string{"落第贤者的学院无双"}, Season: 1, Language: "any", QualityPolicy: subscriptionmatch.DefaultQualityPolicy()})
	require.NoError(t, err)
	require.NoError(t, fx.db.Model(&sub).Update("discovery_rules", string(raw)).Error)
	fx.parser.set(feed.RSSURL, items)
	fx.scheduler.checkRSSFeeds()
	var downloads []model.Download
	require.NoError(t, fx.db.Find(&downloads).Error)
	require.Len(t, downloads, 1)
	require.Equal(t, "3ca0e670c7e085619cd91a6d07c5938490c767b6", downloads[0].TorrentHash)
	require.Equal(t, items[0].TorrentURL, downloads[0].TorrentURL)
}

func TestSchedulerUsesConfirmedPreviewPolicyBeforeBaselineAndDownload(t *testing.T) {
	for _, baseline := range []bool{true, false} {
		name := "download"
		if baseline {
			name = "baseline"
		}
		t.Run(name, func(t *testing.T) {
			fx := newSchedulerLedgerFixture(t, nil)
			sub := fx.createSubscription(t)
			feed := fx.defaultFeed(t, sub.ID)
			rules := subscriptionmatch.Rules{Version: 1, Titles: []string{"Ledger Anime"}, Season: 1, Language: "any", Resolution: 1080, TotalEpisodes: 12}
			raw, _ := json.Marshal(rules)
			require.NoError(t, fx.db.Model(&sub).Update("discovery_rules", string(raw)).Error)
			require.NoError(t, fx.db.Model(&feed).Update("baseline_pending", baseline).Error)
			good := schedulerRSSItem(1, "good", time.Now())
			good.Title = "Ledger Anime - 01 [1080p]"
			bad := schedulerRSSItem(2, "bad", time.Now())
			bad.Title = "Ledger Anime - 02 [720p]"
			wrong := schedulerRSSItem(3, "wrong", time.Now())
			wrong.Title = "Different Anime - 03 [1080p]"
			fx.parser.set(feed.RSSURL, []rss.RSSItem{bad, wrong, good})
			fx.scheduler.checkRSSFeeds()
			var downloads []model.Download
			require.NoError(t, fx.db.Find(&downloads).Error)
			if baseline {
				require.Empty(t, downloads)
			} else {
				require.Len(t, downloads, 1)
				require.Equal(t, "good", downloads[0].TorrentHash)
			}
			require.Zero(t, fx.qb.addCalls)
		})
	}
}

func TestSchedulerQualityPolicySelectsPreferredAndWaitsForFutureRelease(t *testing.T) {
	for _, fallback := range []string{"wait", "allow"} {
		t.Run(fallback, func(t *testing.T) {
			fx := newSchedulerLedgerFixture(t, nil)
			sub := fx.createSubscription(t)
			feed := fx.defaultFeed(t, sub.ID)
			policy := subscriptionmatch.DefaultQualityPolicy()
			policy.Fallback = fallback
			rules := subscriptionmatch.Rules{Version: 2, Titles: []string{"Ledger Anime"}, Season: 1, Language: "any", QualityPolicy: policy}
			raw, err := json.Marshal(rules)
			require.NoError(t, err)
			require.NoError(t, fx.db.Model(&sub).Update("discovery_rules", string(raw)).Error)
			now := time.Now().UTC()
			makeItem := func(ep int, hash, quality string, pub time.Time) rss.RSSItem {
				item := schedulerRSSItem(ep, hash, pub)
				item.Title += " [" + quality + "]"
				return item
			}
			items := []rss.RSSItem{
				makeItem(1, "ep1-4k", "2160p", now),
				makeItem(2, "ep2-4k", "2160p", now),
				makeItem(3, "ep3-720", "720p", now),
				makeItem(1, "ep1-1080", "1080p", now),
			}
			fx.parser.set(feed.RSSURL, items)
			fx.scheduler.checkRSSFeeds()
			var downloads []model.Download
			require.NoError(t, fx.db.Order("episode").Find(&downloads).Error)
			expected := 1
			if fallback == "allow" {
				expected = 2
			}
			require.Len(t, downloads, expected)
			require.Equal(t, "ep1-1080", downloads[0].TorrentHash, "RSS order must not select the 4K item first")
			items = append(items, makeItem(2, "ep2-1080", "1080p", now.Add(time.Minute)))
			fx.parser.set(feed.RSSURL, items)
			fx.scheduler.checkRSSFeeds()
			downloads = nil
			require.NoError(t, fx.db.Order("episode").Find(&downloads).Error)
			require.Len(t, downloads, 2)
			if fallback == "wait" {
				require.Equal(t, "ep2-1080", downloads[1].TorrentHash)
			} else {
				require.Equal(t, "ep2-4k", downloads[1].TorrentHash, "a later preferred release must not replace an existing download")
			}
		})
	}
}
