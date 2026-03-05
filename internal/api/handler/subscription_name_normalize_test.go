package handler

import "testing"

func TestNormalizeSubscriptionNameAndSeason(t *testing.T) {
	tests := []struct {
		name           string
		season         int
		wantName       string
		wantSeason     int
	}{
		{name: "剑来 第二季", season: 1, wantName: "剑来", wantSeason: 2},
		{name: "剑来 Season 2", season: 1, wantName: "剑来", wantSeason: 2},
		{name: "剑来 S2", season: 1, wantName: "剑来", wantSeason: 2},
		{name: "剑来", season: 2, wantName: "剑来", wantSeason: 2},
		{name: "剑来", season: 0, wantName: "剑来", wantSeason: 1},
	}

	for _, tt := range tests {
		gotName, gotSeason := normalizeSubscriptionNameAndSeason(tt.name, tt.season)
		if gotName != tt.wantName || gotSeason != tt.wantSeason {
			t.Fatalf("normalizeSubscriptionNameAndSeason(%q, %d) = (%q, %d), want (%q, %d)",
				tt.name, tt.season, gotName, gotSeason, tt.wantName, tt.wantSeason)
		}
	}
}
