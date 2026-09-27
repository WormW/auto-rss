package subscriptionmatch

import (
	"encoding/json"
	"testing"

	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/stretchr/testify/require"
)

func TestConfirmedRequirementsAreConjunctiveAndExplainUnknowns(t *testing.T) {
	rules := Rules{Version: 1, Titles: []string{"Ledger Anime", "台账番剧"}, Season: 2, Language: "chs", Resolution: 1080, Required: []string{"HEVC"}, Excluded: []string{"dub"}, TotalEpisodes: 12}
	cases := []struct{ title, action, reason string }{
		{"[Group] Ledger Anime S02E01 [1080p][CHS][HEVC]", "match", "confirmed_rules_match"},
		{"[Group] 台账番剧 第二季 01 [1920x1080][简中][HEVC]", "match", "confirmed_rules_match"},
		{"[Group] Another Anime S02E01 [1080p][CHS][HEVC]", "reject", "title_mismatch"},
		{"[Group] Ledger Anime S01E01 [1080p][CHS][HEVC]", "reject", "season_mismatch"},
		{"[Group] Ledger Anime - 01 [1080p][CHS][HEVC]", "review", "season_not_identified"},
		{"[Group] Ledger Anime S02E01 [720p][CHS][HEVC]", "review", "required_resolution_not_identified"},
		{"[Group] Ledger Anime S02E01 [1080p][HEVC]", "review", "required_language_not_identified"},
		{"[Group] Ledger Anime S02E01 [1080p][CHT][HEVC]", "review", "required_language_not_identified"},
		{"[Group] Ledger Anime S02E01 [1080p][CHS][AVC]", "reject", "required_keyword_missing: HEVC"},
		{"[Group] Ledger Anime S02E01 [1080p][CHS][HEVC] dub", "reject", "excluded_keyword: dub"},
		{"[Group] Ledger Anime S02 01-12 [1080p][CHS][HEVC]", "reject", "batch_resource"},
		{"[Group] Ledger Anime S02 OVA 01 [1080p][CHS][HEVC]", "reject", "special_or_movie"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			got := rules.Match(rss.RSSItem{Title: tc.title, Episode: 1})
			require.Equal(t, Decision{tc.action, tc.reason}, got)
		})
	}
	raw, _ := json.Marshal(rules)
	require.Equal(t, "review", Evaluate(`{"version":99}`, rss.RSSItem{}).Action)
	require.Equal(t, rules.Match(rss.RSSItem{Title: cases[0].title, Episode: 1}), Evaluate(string(raw), rss.RSSItem{Title: cases[0].title, Episode: 1}))
}

func TestExactScopeOffsetLanguageAndUploaderEvidence(t *testing.T) {
	rules := Rules{Version: 1, Titles: []string{"Title"}, Season: 2, ScopedFeed: true, EpisodeOffset: 12, TotalEpisodes: 12, Language: "en", Resolution: 1080, TrustedOnly: true}
	item := rss.RSSItem{Title: "[Group] Title - 13 [1080p]", Episode: 13, CategoryID: "1_2", Trusted: true}
	require.Equal(t, "match", rules.Match(item).Action)
	item.CategoryID = "1_3"
	require.Equal(t, "review", rules.Match(item).Action, "non-English does not establish English or Chinese")
	item.CategoryID = "1_2"
	item.Trusted = false
	require.Equal(t, "reject", rules.Match(item).Action)
	item.Trusted = true
	item.Episode = 25
	require.Equal(t, "episode_out_of_range", rules.Match(item).Reason)
	item.Episode = 13
	item.Title = "[Group] DifferentTitle - 13 [1080p]"
	require.Equal(t, "title_mismatch", rules.Match(item).Reason)
	rules.ScopedFeedURL = "https://mikanime.tv/RSS/Bangumi?bangumiId=42"
	rules.EpisodeOffset = 0
	raw, _ := json.Marshal(rules)
	item = rss.RSSItem{Title: "Title - 01 [1080p]", Episode: 1, CategoryID: "1_2", Trusted: true}
	require.Equal(t, "match", EvaluateForFeed(string(raw), item, rules.ScopedFeedURL, 0).Action)
	require.Equal(t, "review", EvaluateForFeed(string(raw), item, "https://nyaa.si/?page=rss", 0).Action)
	require.Equal(t, "episode_mapping_changed_reprepare_required", EvaluateForFeed(string(raw), item, rules.ScopedFeedURL, 12).Reason)
	rules.Season = 1
	item.Title = "Title II - 01 [1080p]"
	require.Equal(t, "review", rules.Match(item).Action)
	rules.Fansub = "LoliHouse"
	item.Title = "Title - 01 [1080p]"
	item.Fansub = "Partner&LoliHouse"
	require.Equal(t, "match", rules.Match(item).Action)
	item.Fansub = "LoliHouse2"
	require.Equal(t, "fansub_mismatch", rules.Match(item).Reason)
}
