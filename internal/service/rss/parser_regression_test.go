package rss

import (
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/utils"
	ext "github.com/mmcdole/gofeed/extensions"
)

func TestExtractEpisodeRegressionCases(t *testing.T) {
	p := NewParser()

	cases := []struct {
		name    string
		title   string
		episode int
		fansub  string
	}{
		{
			name:    "ani standard dash pattern",
			title:   "[ANi] 某番剧 - 12 [1080P][Baha][WEB-DL]",
			episode: 12,
			fansub:  "ANi",
		},
		{
			name:    "chinese episode format",
			title:   "[LoliHouse] 某番剧 第03集 [WebRip 1080p]",
			episode: 3,
			fansub:  "LoliHouse",
		},
		{
			name:    "ep short format",
			title:   "[桜都字幕组] 某番剧 EP7 [1080p]",
			episode: 7,
			fansub:  "桜都字幕组",
		},
		{
			name:    "episode long format",
			title:   "[MCE汉化组] Some Anime Episode 24 END",
			episode: 24,
			fansub:  "MCE汉化组",
		},
		{
			name:    "season episode format",
			title:   "[VCB-Studio] Some Anime S01E09 10bit",
			episode: 9,
			fansub:  "VCB-Studio",
		},
		{
			name:    "bracket episode format",
			title:   "[Nekomoe kissaten] Some Anime [11][WebRip 1080p]",
			episode: 11,
			fansub:  "Nekomoe kissaten",
		},
		{
			name:    "no fansub with dash episode",
			title:   "Some Anime - 176 (1080p)",
			episode: 176,
			fansub:  "",
		},
		{
			name:    "chinese hua pattern",
			title:   "[DMG] 某番剧 12话 1080p",
			episode: 12,
			fansub:  "DMG",
		},
		{
			name:    "same episode old version",
			title:   "[ANi] 某番剧 - 08 [1080P]",
			episode: 8,
			fansub:  "ANi",
		},
		{
			name:    "same episode v2 version",
			title:   "[ANi] 某番剧 - 08 v2 [1080P]",
			episode: 8,
			fansub:  "ANi",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			episode := p.ExtractEpisode(tc.title)
			if episode != tc.episode {
				t.Fatalf("ExtractEpisode(%q) = %d, want %d", tc.title, episode, tc.episode)
			}

			fansub := p.ExtractFansub(tc.title)
			if fansub != tc.fansub {
				t.Fatalf("ExtractFansub(%q) = %q, want %q", tc.title, fansub, tc.fansub)
			}
		})
	}
}

func TestExtractEpisodeIgnoresYearLikeNumber(t *testing.T) {
	p := NewParser()
	title := "[ANi] Some Movie [2024] [1080P]"
	if got := p.ExtractEpisode(title); got != 0 {
		t.Fatalf("ExtractEpisode(%q) = %d, want 0", title, got)
	}
}

func TestExtractInfoHashFromExtensions(t *testing.T) {
	exts := ext.Extensions{
		"nyaa": {
			"infoHash": {{Value: "58A7526E95FE511B80CBDAE8A7DDB7D107BE9871"}},
		},
	}

	got := utils.ExtractInfoHashFromExtensions(exts)
	want := "58a7526e95fe511b80cbdae8a7ddb7d107be9871"
	if got != want {
		t.Fatalf("extractInfoHashFromExtensions() = %q, want %q", got, want)
	}
}

func TestExtractInfoHashFromTorrentURL(t *testing.T) {
	url := "https://mikanime.tv/Download/20260218/4f9c570f6b9ff86278629d352ba8272633a18c3b.torrent"
	got := utils.ExtractInfoHashFromTorrentURL(url)
	want := "4f9c570f6b9ff86278629d352ba8272633a18c3b"
	if got != want {
		t.Fatalf("extractInfoHashFromTorrentURL() = %q, want %q", got, want)
	}
}

func TestParsePreservesItemSpecificRSSURL(t *testing.T) {
	feed := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Mikan</title>
    <item>
      <title>[ANi] Some Anime - 01 [1080P]</title>
      <link>https://mikanani.me/Home/Bangumi/3026</link>
      <enclosure url="https://mikanani.me/Download/20260618/58a7526e95fe511b80cbdae8a7ddb7d107be9871.torrent" type="application/x-bittorrent" />
    </item>
  </channel>
</rss>`)

	items, err := NewParser().Parse(feed)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Parse() returned %d items, want 1", len(items))
	}

	want := "https://mikanani.me/RSS/Bangumi?bangumiId=3026"
	if items[0].RssURL != want {
		t.Fatalf("RssURL = %q, want %q", items[0].RssURL, want)
	}
	if items[0].TorrentURL != "https://mikanani.me/Download/20260618/58a7526e95fe511b80cbdae8a7ddb7d107be9871.torrent" {
		t.Fatalf("TorrentURL = %q", items[0].TorrentURL)
	}
}

func TestParseLeavesItemRSSURLEmptyWhenAbsent(t *testing.T) {
	feed := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Generic torrents</title>
    <item>
      <title>[ANi] Some Anime - 01 [1080P]</title>
      <link>https://example.com/downloads/episode-01.torrent</link>
    </item>
  </channel>
</rss>`)

	items, err := NewParser().Parse(feed)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Parse() returned %d items, want 1", len(items))
	}
	if items[0].RssURL != "" {
		t.Fatalf("RssURL = %q, want empty", items[0].RssURL)
	}
}

func TestParseGenericEnclosureFeedExtractsItemFields(t *testing.T) {
	feed := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Generic seasonal torrents</title>
    <link>https://feeds.example.test/shows</link>
    <item>
      <title>Space Show S02E08 1080p WEB-DL</title>
      <guid isPermaLink="false">space-show-s02e08</guid>
      <link>https://feeds.example.test/releases/space-show-s02e08</link>
      <pubDate>Wed, 24 Jun 2026 12:30:00 +0000</pubDate>
      <enclosure url="https://cdn.example.test/torrents/space-show-s02e08.torrent" type="application/x-bittorrent" length="123456" />
    </item>
  </channel>
</rss>`)

	items, err := NewParser().Parse(feed)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Parse() returned %d items, want 1", len(items))
	}

	item := items[0]
	if item.Title != "Space Show S02E08 1080p WEB-DL" {
		t.Fatalf("Title = %q", item.Title)
	}
	if item.Episode != 8 {
		t.Fatalf("Episode = %d, want 8", item.Episode)
	}
	if item.TorrentURL != "https://cdn.example.test/torrents/space-show-s02e08.torrent" {
		t.Fatalf("TorrentURL = %q", item.TorrentURL)
	}
	if item.PubDate != "Wed, 24 Jun 2026 12:30:00 +0000" {
		t.Fatalf("PubDate = %q", item.PubDate)
	}

	wantPubTime := time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)
	if !item.PubTime.Equal(wantPubTime) {
		t.Fatalf("PubTime = %s, want %s", item.PubTime.Format(time.RFC3339), wantPubTime.Format(time.RFC3339))
	}
}

func TestParseGenericFeedUsesChannelSourceFallback(t *testing.T) {
	feed := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Generic torrents</title>
    <link>https://example.com/anime</link>
    <atom:link xmlns:atom="http://www.w3.org/2005/Atom" href="https://example.com/feeds/season.xml" rel="self" type="application/rss+xml" />
    <item>
      <title>Some Anime - 01 [1080p][CHS]</title>
      <link>magnet:?xt=urn:btih:1111111111111111111111111111111111111111</link>
      <pubDate>Sun, 24 May 2026 10:00:00 +0800</pubDate>
    </item>
    <item>
      <title>Some Anime - 02 [1080p][CHS]</title>
      <link>https://example.com/releases/episode-02</link>
      <enclosure url="https://example.com/downloads/episode-02.torrent" type="application/x-bittorrent" />
    </item>
    <item>
      <title>Some Anime - 03 [1080p][CHS]</title>
      <link>https://example.com/subscriptions/some-anime/rss</link>
      <enclosure url="magnet:?xt=urn:btih:3333333333333333333333333333333333333333" type="application/x-bittorrent" />
    </item>
  </channel>
</rss>`)

	items, err := NewParser().Parse(feed)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("Parse() returned %d items, want 3", len(items))
	}

	if items[0].Title != "Some Anime - 01 [1080p][CHS]" || items[0].Episode != 1 {
		t.Fatalf("first item title/episode = %q/%d", items[0].Title, items[0].Episode)
	}
	if items[0].TorrentURL != "magnet:?xt=urn:btih:1111111111111111111111111111111111111111" {
		t.Fatalf("first TorrentURL = %q", items[0].TorrentURL)
	}
	if items[0].RssURL != "https://example.com/feeds/season.xml" {
		t.Fatalf("first RssURL = %q", items[0].RssURL)
	}

	if items[1].TorrentURL != "https://example.com/downloads/episode-02.torrent" {
		t.Fatalf("second TorrentURL = %q", items[1].TorrentURL)
	}
	if items[1].RssURL != "https://example.com/feeds/season.xml" {
		t.Fatalf("second RssURL = %q", items[1].RssURL)
	}

	if items[2].TorrentURL != "magnet:?xt=urn:btih:3333333333333333333333333333333333333333" {
		t.Fatalf("third TorrentURL = %q", items[2].TorrentURL)
	}
	if items[2].RssURL != "https://example.com/subscriptions/some-anime/rss" {
		t.Fatalf("third RssURL = %q", items[2].RssURL)
	}
}
