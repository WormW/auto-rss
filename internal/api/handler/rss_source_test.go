package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type mockRSSSourceRepo struct {
	source *model.RSSSource
}

func (m *mockRSSSourceRepo) Create(source *model.RSSSource) error { return nil }
func (m *mockRSSSourceRepo) Update(source *model.RSSSource) error { return nil }
func (m *mockRSSSourceRepo) Delete(id uint) error                 { return nil }
func (m *mockRSSSourceRepo) FindByID(id uint) (*model.RSSSource, error) {
	if m.source == nil || m.source.ID != id {
		return nil, errors.New("not found")
	}
	return m.source, nil
}
func (m *mockRSSSourceRepo) List(page, pageSize int, enabled *bool) ([]model.RSSSource, int64, error) {
	return nil, 0, nil
}
func (m *mockRSSSourceRepo) FindByName(name string) (*model.RSSSource, error) {
	return nil, nil
}

type mockRSSSourceConfigRepo struct{}

func (m *mockRSSSourceConfigRepo) Get(key string) (*model.Config, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *mockRSSSourceConfigRepo) GetCached(key string) (string, error) { return "", nil }
func (m *mockRSSSourceConfigRepo) Set(key, value string) error          { return nil }
func (m *mockRSSSourceConfigRepo) Delete(key string) error              { return nil }
func (m *mockRSSSourceConfigRepo) GetAll() ([]model.Config, error)      { return nil, nil }

func TestRSSSourceHandler_FetchAnimes_UsesItemSpecificRSSURL(t *testing.T) {
	items := []rss.RSSItem{
		{Title: "[ANi] Some Anime - 01 [1080P]", Fansub: "ANi", Episode: 1},
		{Title: "[ANi] Some Anime - 02 [1080P]", Fansub: "ANi", Episode: 2, RssURL: "https://mikanani.me/RSS/Bangumi?bangumiId=3026"},
	}
	animes := fetchAnimesForTest(t, items, "https://mikanani.me/RSS/Classic")

	if len(animes) != 1 {
		t.Fatalf("got %d animes, want 1", len(animes))
	}
	if animes[0].RssURL != "https://mikanani.me/RSS/Bangumi?bangumiId=3026" {
		t.Fatalf("RssURL = %q", animes[0].RssURL)
	}
	if got := animes[0].Episodes; len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("Episodes = %#v, want [1 2]", got)
	}
}

func TestRSSSourceHandler_FetchAnimes_FallsBackToSourceURL(t *testing.T) {
	sourceURL := "https://mikanani.me/RSS/Classic"
	items := []rss.RSSItem{
		{Title: "[ANi] Some Anime - 01 [1080P]", Fansub: "ANi", Episode: 1},
	}
	animes := fetchAnimesForTest(t, items, sourceURL)

	if len(animes) != 1 {
		t.Fatalf("got %d animes, want 1", len(animes))
	}
	if animes[0].RssURL != sourceURL {
		t.Fatalf("RssURL = %q, want %q", animes[0].RssURL, sourceURL)
	}
}

func TestRSSSourceHandler_FetchAnimes_GroupsGenericFeedTitles(t *testing.T) {
	sourceURL := "https://example.com/feeds/season.xml"
	itemRSSURL := "https://example.com/subscriptions/some-anime/rss"
	items := []rss.RSSItem{
		{Title: "Some Anime - 01 [1080p][CHS]", Episode: 1, TorrentURL: "magnet:?xt=urn:btih:1111111111111111111111111111111111111111", RssURL: sourceURL},
		{Title: "Some Anime - 02 [1080p][CHS]", Episode: 2, TorrentURL: "https://example.com/downloads/episode-02.torrent", RssURL: sourceURL},
		{Title: "Some Anime - 03 [1080p][CHS]", Episode: 3, TorrentURL: "magnet:?xt=urn:btih:3333333333333333333333333333333333333333", RssURL: itemRSSURL},
	}
	animes := fetchAnimesForTest(t, items, sourceURL)

	if len(animes) != 1 {
		t.Fatalf("got %d animes, want 1", len(animes))
	}
	if animes[0].Title != "Some Anime" {
		t.Fatalf("Title = %q, want Some Anime", animes[0].Title)
	}
	if animes[0].RssURL != itemRSSURL {
		t.Fatalf("RssURL = %q, want %q", animes[0].RssURL, itemRSSURL)
	}
	if got := animes[0].Episodes; len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "3" {
		t.Fatalf("Episodes = %#v, want [1 2 3]", got)
	}
}

func fetchAnimesForTest(t *testing.T, items []rss.RSSItem, sourceURL string) []model.RSSAnime {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewRSSSourceHandler(
		&mockRSSSourceRepo{source: &model.RSSSource{ID: 1, Name: "Mikan", BaseURL: sourceURL, Enabled: true}},
		&mockRSSSourceConfigRepo{},
		&mockRSSParser{items: items},
	)
	router.GET("/rss-sources/:id/animes", handler.FetchAnimes)

	request := httptest.NewRequest(http.MethodGet, "/rss-sources/1/animes", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var body struct {
		Data []model.RSSAnime `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Data
}
