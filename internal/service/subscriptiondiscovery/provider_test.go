package subscriptiondiscovery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
	"github.com/stretchr/testify/require"
)

func TestSourcesResolveExplicitMappingCompileURLAndParseRSS(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/v0/search/subjects":
			fmt.Fprint(w, `{"data":[{"id":42,"type":2,"name":"作品～副題～","name_cn":"番剧"}]}`)
		case "/v0/subjects/42":
			fmt.Fprint(w, `{"id":42,"type":2,"name":"作品～副題～","name_cn":"番剧","eps":12,"total_episodes":99,"infobox":[{"key":"别名","value":[{"v":"Ledger Anime: Subtitle"}]}]}`)
		case "/v0/subjects/42/subjects":
			fmt.Fprint(w, `[{"id":41,"type":2,"name":"前作","relation":"前传"},{"id":500,"type":1,"name":"漫画","relation":"原作"}]`)
		case "/Home/Search":
			fmt.Fprint(w, `<a href="/Home/Bangumi/123">番剧</a>`)
		case "/Home/Bangumi/123":
			fmt.Fprint(w, `<a href="https://bgm.tv/subject/42">Bangumi</a><div class="leftbar-item"><a class="subgroup-name" data-anchor="#group">Group</a></div><div id="group"><a class="mikan-rss" href="/RSS/Bangumi?bangumiId=123&amp;subgroupid=7">RSS</a></div>`)
		default:
			fmt.Fprint(w, `<rss version="2.0" xmlns:nyaa="https://nyaa.si/xmlns/nyaa"><channel><title>Results</title><item><title>[Group] Ledger Anime - 01 [1080p]</title><link>https://example.test/1.torrent</link><nyaa:categoryId>1_2</nyaa:categoryId><nyaa:trusted>Yes</nyaa:trusted></item></channel></rss>`)
		}
	}))
	defer server.Close()
	p := NewHTTPProvider(nil)
	p.BangumiBase = server.URL
	p.MikanBase = server.URL
	p.NyaaBase = server.URL
	ctx := context.Background()
	input := PrepareInput{Query: "番剧", Sources: []string{"mikan", "nyaa"}, Language: "en", Resolution: 1080, TrustedOnly: true, NyaaUploader: "some uploader"}
	candidates, err := p.Search(ctx, input)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	subject, err := p.Subject(ctx, 42)
	require.NoError(t, err)
	require.Contains(t, subject.Aliases, "Ledger Anime")
	require.Equal(t, 12, subject.Episodes, "chapter count must not replace expected episode count")
	require.Len(t, subject.Relations, 1)
	feeds, warnings, err := p.Feeds(ctx, subject, input)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, feeds, 3)
	require.Equal(t, 42, feeds[0].BangumiID)
	require.Contains(t, feeds[0].URL, "bangumiId=123")
	require.Equal(t, "explicit_bangumi_link", feeds[0].MappingEvidence)
	u, err := url.Parse(feeds[1].URL)
	require.NoError(t, err)
	require.Equal(t, `"Ledger Anime" 1080`, u.Query().Get("q"))
	require.Equal(t, "1_2", u.Query().Get("c"))
	require.Equal(t, "2", u.Query().Get("f"))
	require.Equal(t, "some uploader", u.Query().Get("u"))
	items, err := p.Fetch(ctx, feeds[1])
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].Trusted)
	require.Equal(t, "1_2", items[0].CategoryID)
	require.Equal(t, 1, items[0].Episode)
	before := requests.Load()
	_, err = p.Fetch(ctx, feeds[1])
	require.NoError(t, err)
	require.Equal(t, before, requests.Load(), "same preview reuses bounded cache")
	_, err = p.Fetch(ctx, Feed{Source: "nyaa", URL: "http://unexpected.test/"})
	require.ErrorContains(t, err, "origin")
}

func TestNyaaQualityPreferenceDoesNotRemoveFallbackCandidatesFromQuery(t *testing.T) {
	p := NewHTTPProvider(nil)
	input := PrepareInput{Query: "Anime", QualityPolicy: subscriptionmatch.DefaultQualityPolicy()}
	for _, fallback := range []string{"wait", "allow"} {
		input.QualityPolicy.Fallback = fallback
		feeds := p.nyaaFeeds(Subject{Name: "Anime", Aliases: []string{"Anime"}}, input)
		require.Len(t, feeds, 1)
		u, err := url.Parse(feeds[0].URL)
		require.NoError(t, err)
		require.Equal(t, `"Anime"`, u.Query().Get("q"), "a preference must not become a hard search restriction")
	}
}

func TestUpstreamFailureDoesNotFabricateMetadataOrFollowForeignRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://unexpected.test/private", http.StatusFound)
	}))
	defer server.Close()
	p := NewHTTPProvider(nil)
	p.BangumiBase = server.URL
	_, err := p.Search(context.Background(), PrepareInput{Query: "Title"})
	require.ErrorContains(t, err, "unexpected source redirect")
}

func TestMikanRedirectAliasesAreExplicitAndDoNotAllowOtherOrigins(t *testing.T) {
	base, _ := url.Parse("https://mikanime.tv/Home/Search")
	for _, tc := range []struct {
		target  string
		allowed bool
	}{
		{"https://mikanani.me/home/search", true},
		{"http://mikanani.me/home/search", false},
		{"https://mikanani.me:8443/home/search", false},
		{"https://mikanani.me.attacker.test/home/search", false},
		{"https://user:pass@mikanani.me/home/search", false},
	} {
		target, err := url.Parse(tc.target)
		require.NoError(t, err)
		require.Equal(t, tc.allowed, allowedOrigin(base, target), tc.target)
	}
}
