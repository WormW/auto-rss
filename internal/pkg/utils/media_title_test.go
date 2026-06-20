package utils

import "testing"

func TestNormalizeMediaTitleAndSeason(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		season     int
		wantTitle  string
		wantSeason int
	}{
		{name: "Chinese season suffix", title: "入间同学入魔了 第四季", season: 4, wantTitle: "入间同学入魔了", wantSeason: 4},
		{name: "Japanese series suffix", title: "魔入りました！入間くん 第4シリーズ", season: 4, wantTitle: "魔入りました！入間くん", wantSeason: 4},
		{name: "Chinese numeric suffix", title: "灵笼 Incarnation 第2季", season: 1, wantTitle: "灵笼 Incarnation", wantSeason: 2},
		{name: "English season suffix", title: "剑来 Season 2", season: 1, wantTitle: "剑来", wantSeason: 2},
		{name: "Tail S suffix", title: "剑来 S2", season: 1, wantTitle: "剑来", wantSeason: 2},
		{name: "Normal title unchanged", title: "葬送的芙莉莲", season: 1, wantTitle: "葬送的芙莉莲", wantSeason: 1},
		{name: "Season defaults without suffix", title: "剑来", season: 0, wantTitle: "剑来", wantSeason: 1},
		{name: "Middle S token unchanged", title: "BanG Dream! It's MyGO!!!!!", season: 1, wantTitle: "BanG Dream! It's MyGO!!!!!", wantSeason: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotSeason := NormalizeMediaTitleAndSeason(tt.title, tt.season)
			if gotTitle != tt.wantTitle || gotSeason != tt.wantSeason {
				t.Fatalf("NormalizeMediaTitleAndSeason(%q, %d) = (%q, %d), want (%q, %d)",
					tt.title, tt.season, gotTitle, gotSeason, tt.wantTitle, tt.wantSeason)
			}
		})
	}
}

func TestMediaLibraryTitle(t *testing.T) {
	if got := MediaLibraryTitle("入间同学入魔了 第四季"); got != "入间同学入魔了" {
		t.Fatalf("MediaLibraryTitle() = %q, want %q", got, "入间同学入魔了")
	}
}
