package bangumi

import "testing"

func TestExtractSeasonFromName(t *testing.T) {
	tests := []struct {
		name     string
		nameCN   string
		expected int
	}{
		{name: "Jian Lai 2", nameCN: "剑来 第二季", expected: 2},
		{name: "Sword of Coming Season 2", nameCN: "", expected: 2},
		{name: "Some Anime S3", nameCN: "", expected: 3},
		{name: "Some Anime", nameCN: "第二季", expected: 2},
		{name: "Some Anime Season II", nameCN: "", expected: 2},
		{name: "Some Anime", nameCN: "", expected: 1},
	}

	for _, tt := range tests {
		got := extractSeasonFromName(tt.name, tt.nameCN)
		if got != tt.expected {
			t.Fatalf("extractSeasonFromName(%q, %q) = %d, want %d", tt.name, tt.nameCN, got, tt.expected)
		}
	}
}

func TestChineseNumeralToInt(t *testing.T) {
	tests := map[string]int{
		"一":  1,
		"二":  2,
		"十":  10,
		"十一": 11,
		"二十": 20,
		"二十三": 23,
		"两":  2,
	}

	for input, expected := range tests {
		if got := chineseNumeralToInt(input); got != expected {
			t.Fatalf("chineseNumeralToInt(%q) = %d, want %d", input, got, expected)
		}
	}
}
