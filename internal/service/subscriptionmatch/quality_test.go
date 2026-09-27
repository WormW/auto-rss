package subscriptionmatch

import (
	"encoding/json"
	"testing"

	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/stretchr/testify/require"
)

func TestQualityExclusionsPreferenceAndUnknowns(t *testing.T) {
	rules := Rules{Version: 2, Titles: []string{"Title"}, Season: 1, Language: "any", QualityPolicy: DefaultQualityPolicy()}
	require.NoError(t, rules.Validate())
	for _, tc := range []struct{ quality, action, reason string }{
		{"720p", "reject", "excluded_resolution"},
		{"1280x720", "reject", "excluded_resolution"},
		{"1080p", "match", "confirmed_rules_match"},
		{"1920x1080", "match", "confirmed_rules_match"},
		{"2160p", "review", "waiting_for_preferred_resolution"},
		{"480p", "review", "waiting_for_preferred_resolution"},
		{"HEVC", "review", "resolution_not_identified"},
		{"1080p 2160p", "review", "ambiguous_resolution"},
		{"1080p 720p", "reject", "excluded_resolution"},
	} {
		item := rss.RSSItem{Title: "Title - 01 [" + tc.quality + "]", Episode: 1}
		require.Equal(t, Decision{tc.action, tc.reason}, rules.Match(item), tc.quality)
	}
	rules.QualityPolicy.Fallback = "allow"
	require.Equal(t, "match", rules.Match(rss.RSSItem{Title: "Title - 01 [2160p]", Episode: 1}).Action)
	require.Equal(t, "reject", rules.Match(rss.RSSItem{Title: "Title - 01 [720p]", Episode: 1}).Action)
	require.Equal(t, "review", rules.Match(rss.RSSItem{Title: "Title - 01", Episode: 1}).Action)
	items := []rss.RSSItem{{Title: "Title - 01 [2160p]"}, {Title: "Title - 01 [1080p]"}, {Title: "Title - 02 [1080p]"}}
	before := append([]rss.RSSItem{}, items...)
	ordered := rules.Prioritize(items)
	require.Equal(t, []rss.RSSItem{items[1], items[2], items[0]}, ordered)
	ordered[0].Title = "changed"
	require.Equal(t, before, items, "preview must not mutate shared provider cache")
}

func TestQualityPolicyCompatibilityAndConflicts(t *testing.T) {
	legacy := Rules{Version: 1, Titles: []string{"Title"}, Season: 1, Language: "any"}
	item := rss.RSSItem{Title: "Title - 01 [720p]", Episode: 1}
	raw, _ := json.Marshal(legacy)
	require.Equal(t, "match", Evaluate(string(raw), item).Action)
	require.Equal(t, "match", Evaluate("", item).Action)
	legacy.QualityPolicy = DefaultQualityPolicy()
	require.ErrorContains(t, legacy.Validate(), "version 2")
	legacy.Version = 2
	legacy.Resolution = 720
	require.ErrorContains(t, legacy.Validate(), "conflicting")
	for _, policy := range []QualityPolicy{
		{ExcludedResolutions: []int{720, 720}, Fallback: "allow"},
		{ExcludedResolutions: []int{999}, Fallback: "allow"},
		{PreferredResolution: 1080, Fallback: "unknown"},
		{PreferredResolution: 999, Fallback: "allow"},
		{Fallback: "wait"},
	} {
		require.Error(t, policy.Validate(0))
	}
}
