package rss

import (
	"testing"

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
