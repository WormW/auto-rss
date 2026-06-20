package downloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
)

func TestEnsureTVShowNFOCreatesBangumiMetadata(t *testing.T) {
	showRoot := t.TempDir()
	subscription := &model.Subscription{
		ID:                1,
		Name:              "ATRI <My Dear Moments> & Friends",
		BangumiID:         12345,
		BangumiSummary:    "Robots & sea <summer>",
		AirDate:           "2024-07-01",
		AirYear:           2024,
		BangumiScore:      7.8,
		BangumiCover:      "https://example.com/cover.jpg",
		BangumiCoverLocal: "covers/12345.jpg",
	}

	if err := ensureTVShowNFO(showRoot, subscription); err != nil {
		t.Fatalf("ensureTVShowNFO() error = %v", err)
	}

	content := readFileString(t, filepath.Join(showRoot, tvShowNFOFileName))
	assertContains(t, content, "<tvshow>")
	assertContains(t, content, "<bangumiid>12345</bangumiid>")
	assertContains(t, content, "<uniqueid type=\"bangumi\" default=\"true\">12345</uniqueid>")
	assertContains(t, content, "<title>ATRI &lt;My Dear Moments&gt; &amp; Friends</title>")
	assertContains(t, content, "<plot>Robots &amp; sea &lt;summer&gt;</plot>")
	assertContains(t, content, "<premiered>2024-07-01</premiered>")
	assertContains(t, content, "<year>2024</year>")
	assertContains(t, content, "<rating>7.8</rating>")
	assertContains(t, content, "<thumb>covers/12345.jpg</thumb>")
}

func TestEnsureTVShowNFOUpdatesExistingWithoutOverwritingRichMetadata(t *testing.T) {
	showRoot := t.TempDir()
	nfoPath := filepath.Join(showRoot, tvShowNFOFileName)
	existing := `<?xml version="1.0" encoding="UTF-8"?>
<tvshow>
  <title>User Title</title>
  <genre>Drama</genre>
</tvshow>
`
	if err := os.WriteFile(nfoPath, []byte(existing), 0644); err != nil {
		t.Fatalf("write existing nfo: %v", err)
	}

	subscription := &model.Subscription{Name: "Generated Title", BangumiID: 67890}
	if err := ensureTVShowNFO(showRoot, subscription); err != nil {
		t.Fatalf("ensureTVShowNFO() error = %v", err)
	}

	content := readFileString(t, nfoPath)
	assertContains(t, content, "<title>User Title</title>")
	assertContains(t, content, "<genre>Drama</genre>")
	assertContains(t, content, "<bangumiid>67890</bangumiid>")
	assertContains(t, content, "<uniqueid type=\"bangumi\" default=\"true\">67890</uniqueid>")
	if strings.Contains(content, "Generated Title") {
		t.Fatalf("existing rich metadata was overwritten: %s", content)
	}
}

func TestEnsureTVShowNFOSkipsWithoutBangumiID(t *testing.T) {
	showRoot := t.TempDir()
	subscription := &model.Subscription{Name: "No ID", BangumiID: 0}

	if err := ensureTVShowNFO(showRoot, subscription); err != nil {
		t.Fatalf("ensureTVShowNFO() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(showRoot, tvShowNFOFileName)); !os.IsNotExist(err) {
		t.Fatalf("tvshow.nfo should not be created without Bangumi ID, stat err = %v", err)
	}
}

func TestTVShowRootFromRenamedPathUsesShowRootNotSeasonDirectory(t *testing.T) {
	tests := []struct {
		name        string
		renamedPath string
		want        string
	}{
		{
			name:        "standard media library path",
			renamedPath: filepath.Join("media", "Frieren", "Season 1", "Frieren S01E01.mkv"),
			want:        filepath.Join("media", "Frieren"),
		},
		{
			name:        "flat show directory path",
			renamedPath: filepath.Join("media", "Frieren", "Frieren S01E01.mkv"),
			want:        filepath.Join("media", "Frieren"),
		},
		{
			name:        "season underscore path",
			renamedPath: filepath.Join("media", "Frieren", "Season_1", "Frieren S01E01.mkv"),
			want:        filepath.Join("media", "Frieren"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tvShowRootFromRenamedPath(tt.renamedPath); got != tt.want {
				t.Fatalf("tvShowRootFromRenamedPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected content to contain %q, got:\n%s", want, got)
	}
}
