package subscriptiondiscovery

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
	"github.com/stretchr/testify/require"
)

func TestDMHYQueryCompilationKeepsQualityLocalAndSourcesSeparate(t *testing.T) {
	p := NewHTTPProvider(nil)
	subject := Subject{NameCN: "落第贤者的学院无双～副标题～", Name: "落第賢者の学院無双～副題～"}
	input := PrepareInput{Query: "落第贤者的学院无双", Resolution: 1080, Language: "en", TrustedOnly: true, NyaaQuery: "unrelated", NyaaUploader: "uploader"}
	feeds := p.dmhyFeeds(subject, input)
	require.Len(t, feeds, 2)
	for i, name := range []string{"落第贤者的学院无双", "落第賢者の学院無双"} {
		u, err := url.Parse(feeds[i].URL)
		require.NoError(t, err)
		require.Equal(t, "share.dmhy.org", u.Host)
		require.Equal(t, "/topics/rss/rss.xml", u.Path)
		require.Equal(t, url.Values{"keyword": {name}}, u.Query(), "Nyaa options and bare 1080 must not leak into DMHY searches")
		page, err := url.Parse(feeds[i].PageURL)
		require.NoError(t, err)
		require.Equal(t, "/topics/list", page.Path)
		require.Equal(t, u.Query(), page.Query())
	}
	input.DmhyQuery = "作品 1080p team_id:123 | 别名 & +"
	feeds = p.dmhyFeeds(subject, input)
	require.Len(t, feeds, 1)
	u, err := url.Parse(feeds[0].URL)
	require.NoError(t, err)
	require.Equal(t, input.DmhyQuery, u.Query().Get("keyword"))
	input.DmhyQuery = strings.Repeat("a", 500)
	require.Len(t, p.dmhyFeeds(subject, input), 1, "accepted custom queries must not be silently discarded")
	input.DmhyQuery = ""
	input.DmhyTeamID = 657
	feeds = p.dmhyFeeds(subject, input)
	require.Len(t, feeds, 2)
	require.Equal(t, 657, feeds[0].TeamID)
	require.Contains(t, feeds[0].Query, "team_id:657")
	u, err = url.Parse(feeds[0].URL)
	require.NoError(t, err)
	require.Contains(t, u.Query().Get("keyword"), "team_id:657")
	input.Sources = []string{"nyaa"}
	require.ErrorContains(t, normalizeInput(&input), "requires dmhy")
	input.Sources = []string{"dmhy"}
	_, err = p.Fetch(context.Background(), Feed{Source: "dmhy", URL: "https://nyaa.si/?page=rss"})
	require.ErrorContains(t, err, "origin")
	_, err = p.Fetch(context.Background(), Feed{Source: "unknown", URL: "https://nyaa.si/?page=rss"})
	require.ErrorContains(t, err, "source")
}

func TestDMHYBroadRSSLearnsTeamIDsAndPreviewGroups(t *testing.T) {
	data, err := os.ReadFile("../rss/testdata/dmhy.xml")
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/topics/rss/rss.xml":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write(bytes.ReplaceAll(data, []byte("http://share.dmhy.org"), []byte("http://"+r.Host)))
		case "/topics/list":
			fmt.Fprint(w, `<html><table><tbody>
<tr><td class="title"><span class="tag"><a href="/topics/list/team_id/657">LoliHouse</a></span><a href="/topics/view/1_example.html">[Group] 落第贤者的学院无双 - 01 [1080p]</a></td></tr>
<tr><td class="title"><span class="tag"><a href="/topics/list/team_id/123">OtherGroup</a></span><a href="/topics/view/2_example.html">[Group] 落第贤者的学院无双 - 02 [720p]</a></td></tr>
</tbody></table></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := NewHTTPProvider(nil)
	p.DmhyBase = server.URL
	feed := Feed{Source: "dmhy", URL: server.URL + "/topics/rss/rss.xml?keyword=Anime", PageURL: server.URL + "/topics/list?keyword=Anime"}
	items, err := p.Fetch(context.Background(), feed)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "657", items[0].SourceGroupID)
	require.Equal(t, "LoliHouse", items[0].SourceGroupName)
	require.Equal(t, "123", items[1].SourceGroupID)
	require.Equal(t, "OtherGroup", items[1].SourceGroupName)

	rules := subscriptionmatch.Rules{Version: 2, Titles: []string{"落第贤者的学院无双"}, Season: 1, Language: "any", QualityPolicy: subscriptionmatch.DefaultQualityPolicy()}
	preview := makePreview(feed, rules, items, Subject{NameCN: "落第贤者的学院无双"})
	require.Len(t, preview.GroupCandidates, 2)
	require.Equal(t, 657, preview.GroupCandidates[0].ID)
	require.Equal(t, "LoliHouse", preview.GroupCandidates[0].Name)
	require.Equal(t, []int{1}, preview.GroupCandidates[0].MatchedEpisodes)
	require.Equal(t, 123, preview.GroupCandidates[1].ID)
	require.Equal(t, "OtherGroup", preview.GroupCandidates[1].Name)
	require.Empty(t, preview.GroupCandidates[1].MatchedEpisodes)
}

func TestDMHYPrepareConfirmPersistsRSSWithDefaultQuality(t *testing.T) {
	s, db, _ := newDiscoveryFixture(t)
	data, err := os.ReadFile("../rss/testdata/dmhy.xml")
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/42":
			fmt.Fprint(w, `{"id":42,"type":2,"name":"落第賢者の学院無双～副題～","name_cn":"落第贤者的学院无双～副标题～","eps":12}`)
		case "/v0/subjects/42/subjects":
			fmt.Fprint(w, `[]`)
		case "/topics/rss/rss.xml":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := NewHTTPProvider(nil)
	p.BangumiBase, p.DmhyBase = server.URL, server.URL
	s.provider = p
	draft, err := s.Prepare(context.Background(), PrepareInput{Query: "落第贤者的学院无双", BangumiID: 42, Sources: []string{"dmhy"}})
	require.NoError(t, err)
	require.Len(t, draft.Feeds, 2)
	for _, preview := range draft.Feeds {
		require.Equal(t, 1, preview.Matched)
		require.Equal(t, 1, preview.Rejected)
		require.False(t, preview.Rules.ScopedFeed, "keyword search is not proof of a single-season scope")
		require.False(t, preview.Rules.TrustedOnly)
		require.True(t, preview.Confirmable)
	}
	var count int64
	require.NoError(t, db.Table("subscriptions").Count(&count).Error)
	require.Zero(t, count)
	sub, err := s.Confirm(context.Background(), ConfirmInput{draft.ID, draft.Revision, draft.Feeds[0].Feed.ID})
	require.NoError(t, err)
	require.Equal(t, draft.Feeds[0].Feed.URL, sub.RssURL)
	rules, err := subscriptionmatch.Decode(sub.DiscoveryRules)
	require.NoError(t, err)
	require.Equal(t, subscriptionmatch.DefaultQualityPolicy(), rules.QualityPolicy)
	require.NoError(t, db.Table("downloads").Count(&count).Error)
	require.Zero(t, count)
}

func TestAllDefaultSourcesGetPreviewSlotsWithinSixFeedLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Home/Search" {
			fmt.Fprint(w, `<a href="/Home/Bangumi/123">Anime</a>`)
			return
		}
		fmt.Fprint(w, `<a href="https://bgm.tv/subject/42">Bangumi</a>`)
		for i := 1; i <= 4; i++ {
			fmt.Fprintf(w, `<div class="leftbar-item"><a class="subgroup-name" data-anchor="#g%d">Group %d</a></div><div id="g%d"><a class="mikan-rss" href="/RSS/Bangumi?bangumiId=123&amp;subgroupid=%d">RSS</a></div>`, i, i, i, i)
		}
	}))
	defer server.Close()
	p := NewHTTPProvider(nil)
	p.MikanBase = server.URL
	input := PrepareInput{Query: "Anime"}
	require.NoError(t, normalizeInput(&input))
	require.Equal(t, []string{"mikan", "nyaa", "dmhy"}, input.Sources)
	feeds, warnings, err := p.Feeds(context.Background(), Subject{ID: 42, Name: "作品", NameCN: "番剧", Aliases: []string{"Anime", "作品"}}, input)
	require.NoError(t, err)
	require.Len(t, feeds, 6)
	require.Contains(t, warnings, "source_candidates_limited_to_6")
	counts := map[string]int{}
	for _, feed := range feeds {
		counts[feed.Source]++
		require.NotEmpty(t, feed.ID)
	}
	require.Equal(t, map[string]int{"mikan": 2, "nyaa": 2, "dmhy": 2}, counts)
	input.Sources = []string{"dmhy", "dmhy"}
	require.Error(t, normalizeInput(&input))
}
