package utils

import "testing"

func TestNormalizeFeedURLPreservesQueryMeaningAndSortsKeys(t *testing.T) {
	got := NormalizeFeedURL(" HTTPS://Example.COM:443/rss?b=2&a=1&a=3 ")
	want := "https://example.com/rss?a=1&a=3&b=2"
	if got != want {
		t.Fatalf("NormalizeFeedURL() = %q, want %q", got, want)
	}
}
