package downloader

import "testing"

func TestExtractHashFromTorrentURL(t *testing.T) {
	cases := []struct {
		url  string
		hash string
	}{
		{
			url:  "https://mikanime.tv/Download/20260218/4f9c570f6b9ff86278629d352ba8272633a18c3b.torrent",
			hash: "4f9c570f6b9ff86278629d352ba8272633a18c3b",
		},
		{
			url:  "https://example.com/a/ABCDEF0123456789ABCDEF0123456789ABCDEF01.torrent?x=1",
			hash: "abcdef0123456789abcdef0123456789abcdef01",
		},
		{
			url:  "https://example.com/a/not-a-hash.torrent",
			hash: "",
		},
	}

	for _, tc := range cases {
		if got := extractHashFromTorrentURL(tc.url); got != tc.hash {
			t.Fatalf("extractHashFromTorrentURL(%q) = %q, want %q", tc.url, got, tc.hash)
		}
	}
}
