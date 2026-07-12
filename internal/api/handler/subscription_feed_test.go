package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscription"
	"github.com/WormW/auto-rss/internal/service/subscriptionfeed"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionOPMLRoundTripGroupsFeedsIntoOneSubscription(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	fx.seedSubscriptionWithFeeds(2)

	exported := fx.get("/subscriptions/export?format=opml")
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	assert.Contains(t, exported.Body.String(), `autoRssOffset="100"`)
	assert.Contains(t, exported.Body.String(), `autoRssSubscription=`)

	target := newSubscriptionFeedHandlerFixture(t)
	imported := target.postJSON("/subscriptions/import", marshalImportRequest("opml", exported.Body.String()))
	require.Equal(t, http.StatusOK, imported.Code, imported.Body.String())
	var subscriptions, feeds int64
	require.NoError(t, target.db.Model(&model.Subscription{}).Count(&subscriptions).Error)
	require.NoError(t, target.db.Model(&model.SubscriptionFeed{}).Count(&feeds).Error)
	assert.EqualValues(t, 1, subscriptions)
	assert.EqualValues(t, 2, feeds)
	var restored []model.SubscriptionFeed
	require.NoError(t, target.db.Order("episode_offset ASC").Find(&restored).Error)
	require.Len(t, restored, 2)
	assert.Equal(t, []int{0, 100}, []int{restored[0].EpisodeOffset, restored[1].EpisodeOffset})
}

func TestSubscriptionJSONRoundTripPreservesFeedsFromAPIResponse(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	fx.seedSubscriptionWithFeeds(2)

	exported := fx.get("/subscriptions/export?format=json")
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	assert.Contains(t, exported.Body.String(), `"version":"2.0"`)

	target := newSubscriptionFeedHandlerFixture(t)
	imported := target.postJSON("/subscriptions/import", marshalImportRequest("json", exported.Body.String()))
	require.Equal(t, http.StatusOK, imported.Code, imported.Body.String())
	assert.Contains(t, imported.Body.String(), `"success":1`)

	var feeds []model.SubscriptionFeed
	require.NoError(t, target.db.Order("episode_offset ASC").Find(&feeds).Error)
	require.Len(t, feeds, 2)
	assert.Equal(t, []int{0, 100}, []int{feeds[0].EpisodeOffset, feeds[1].EpisodeOffset})
	assert.Zero(t, feeds[0].LastError)
	assert.Nil(t, feeds[0].LastCheckTime)
	assert.True(t, feeds[0].BaselinePending)
}

func TestSubscriptionOPMLRoundTripUsesXMLAttributeEscaping(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	sub := model.Subscription{Name: `Anime & "Friends"`, Season: 2, Status: "active", Enabled: true}
	require.NoError(t, fx.db.Create(&sub).Error)
	feed := model.SubscriptionFeed{
		SubscriptionID:   sub.ID,
		Name:             `A & B`,
		Fansub:           `Group "One"`,
		RSSURL:           "https://feed.test/rss?a=1&b=2",
		RSSURLNormalized: "https://feed.test/rss?a=1&b=2",
		Enabled:          true,
	}
	require.NoError(t, fx.db.Create(&feed).Error)

	exported := fx.get("/subscriptions/export?format=opml")
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	assert.Contains(t, exported.Body.String(), `Anime &amp; &#34;Friends&#34;`)
	assert.Contains(t, exported.Body.String(), `https://feed.test/rss?a=1&amp;b=2`)

	target := newSubscriptionFeedHandlerFixture(t)
	imported := target.postJSON("/subscriptions/import", marshalImportRequest("opml", exported.Body.String()))
	require.Equal(t, http.StatusOK, imported.Code, imported.Body.String())
	var restored model.SubscriptionFeed
	require.NoError(t, target.db.First(&restored).Error)
	assert.Equal(t, `A & B`, restored.Name)
	assert.Equal(t, `Group "One"`, restored.Fansub)
	assert.Equal(t, "https://feed.test/rss?a=1&b=2", restored.RSSURL)
}

func TestSubscriptionOPMLImportRejectsInvalidOffsetForGroup(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	opml := `<?xml version="1.0"?><opml version="2.0"><body>` +
		`<outline type="rss" text="Anime - A" title="Anime" xmlUrl="https://a.test/rss" autoRssSubscription="Anime" autoRssSeason="1" autoRssFeed="A" autoRssOffset="invalid" />` +
		`</body></opml>`

	imported := fx.postJSON("/subscriptions/import", marshalImportRequest("opml", opml))
	require.Equal(t, http.StatusOK, imported.Code, imported.Body.String())
	assert.Contains(t, imported.Body.String(), `"failed":1`)
	assert.Contains(t, imported.Body.String(), "invalid episode offset")

	var subscriptions, feeds int64
	require.NoError(t, fx.db.Model(&model.Subscription{}).Count(&subscriptions).Error)
	require.NoError(t, fx.db.Model(&model.SubscriptionFeed{}).Count(&feeds).Error)
	assert.Zero(t, subscriptions)
	assert.Zero(t, feeds)
}

func marshalImportRequest(format, data string) string {
	encoded, err := json.Marshal(map[string]string{"format": format, "data": data})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestCreateSubscriptionWithFeedsPersistsSubscriptionAndFeeds(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	recorder := fx.postJSON("/subscriptions", `{
	  "name":"Anime","season":1,
	  "feeds":[
	    {"name":"A","rss_url":"https://a.test/rss","episode_offset":0,"enabled":true},
	    {"name":"B","rss_url":"https://b.test/rss","episode_offset":100,"enabled":true}
	  ]
	}`)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var feeds []model.SubscriptionFeed
	require.NoError(t, fx.db.Order("id").Find(&feeds).Error)
	require.Len(t, feeds, 2)
	assert.Equal(t, []int{0, 100}, []int{feeds[0].EpisodeOffset, feeds[1].EpisodeOffset})
}

func TestLegacyRSSUpdateRejectsAmbiguousMultiFeedSubscription(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	sub := fx.seedSubscriptionWithFeeds(2)

	recorder := fx.putJSON(fmt.Sprintf("/subscriptions/%d", sub.ID), `{"rss_url":"https://new.test/rss"}`)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "feed API")
}

func TestFeedPreviewAndCRUDRoutes(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	sub := fx.seedSubscriptionWithFeeds(0)

	preview := fx.postJSON(fmt.Sprintf("/subscriptions/%d/feeds/preview", sub.ID), `{"rss_url":"https://a.test/rss","episode_offset":100}`)
	created := fx.postJSON(fmt.Sprintf("/subscriptions/%d/feeds", sub.ID), `{"name":"A","rss_url":"https://a.test/rss","episode_offset":100,"enabled":true}`)

	assert.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	assert.Equal(t, http.StatusOK, created.Code, created.Body.String())
}

func TestCreateAllowsSameFeedURLInDifferentSubscriptions(t *testing.T) {
	fx := newSubscriptionFeedHandlerFixture(t)
	first := fx.postJSON("/subscriptions", `{"name":"Anime A","season":1,"feeds":[{"name":"A","rss_url":"https://shared.test/rss","episode_offset":0,"enabled":true}]}`)
	second := fx.postJSON("/subscriptions", `{"name":"Anime B","season":1,"feeds":[{"name":"A","rss_url":"https://shared.test/rss","episode_offset":0,"enabled":true}]}`)

	assert.Equal(t, http.StatusOK, first.Code, first.Body.String())
	assert.Equal(t, http.StatusOK, second.Code, second.Body.String())
}

type subscriptionFeedHandlerFixture struct {
	t      *testing.T
	db     *gorm.DB
	router *gin.Engine
}

func newSubscriptionFeedHandlerFixture(t *testing.T) *subscriptionFeedHandlerFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.SubscriptionFeed{},
		&model.SubscriptionFeedSeenItem{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	subRepo := repository.NewSubscriptionRepository(db)
	feedRepo := repository.NewSubscriptionFeedRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	parser := &handlerFeedParser{}
	feedService := subscriptionfeed.NewService(db, feedRepo, parser)
	creator := subscription.NewCreator(db, feedService, episodeRepo)
	subHandler := NewSubscriptionHandlerWithFeeds(
		subRepo, nil, nil, nil, "", episodeRepo, feedRepo, feedService, creator,
	)
	subHandler.bangumiEnricher = nil
	feedHandler := NewSubscriptionFeedHandler(feedRepo, feedService, episode.NewService(episodeRepo))
	router := gin.New()
	subscriptions := router.Group("/subscriptions")
	subscriptions.POST("", subHandler.Create)
	subscriptions.GET("/export", subHandler.ExportSubscriptions)
	subscriptions.POST("/import", subHandler.ImportSubscriptions)
	subscriptions.PUT("/:id", subHandler.Update)
	registerSubscriptionFeedRoutes(subscriptions, feedHandler)
	return &subscriptionFeedHandlerFixture{t: t, db: db, router: router}
}

func (fx *subscriptionFeedHandlerFixture) get(path string) *httptest.ResponseRecorder {
	fx.t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	fx.router.ServeHTTP(recorder, request)
	return recorder
}

func (fx *subscriptionFeedHandlerFixture) postJSON(path, body string) *httptest.ResponseRecorder {
	return fx.requestJSON(http.MethodPost, path, body)
}

func (fx *subscriptionFeedHandlerFixture) putJSON(path, body string) *httptest.ResponseRecorder {
	return fx.requestJSON(http.MethodPut, path, body)
}

func (fx *subscriptionFeedHandlerFixture) requestJSON(method, path, body string) *httptest.ResponseRecorder {
	fx.t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	fx.router.ServeHTTP(recorder, request)
	return recorder
}

func (fx *subscriptionFeedHandlerFixture) seedSubscriptionWithFeeds(count int) model.Subscription {
	fx.t.Helper()
	sub := model.Subscription{Name: fmt.Sprintf("Anime %d", time.Now().UnixNano()), Status: "active", Enabled: true, BangumiCoverLocal: "present"}
	require.NoError(fx.t, fx.db.Create(&sub).Error)
	for index := 0; index < count; index++ {
		feed := model.SubscriptionFeed{
			SubscriptionID:   sub.ID,
			Name:             fmt.Sprintf("Feed %d", index+1),
			RSSURL:           fmt.Sprintf("https://feed-%d.test/rss", index+1),
			RSSURLNormalized: fmt.Sprintf("https://feed-%d.test/rss", index+1),
			EpisodeOffset:    index * 100,
			Enabled:          true,
		}
		require.NoError(fx.t, fx.db.Create(&feed).Error)
	}
	return sub
}

type handlerFeedParser struct{}

func (p *handlerFeedParser) FetchAndParse(string) ([]rss.RSSItem, error) {
	return []rss.RSSItem{{Title: "Anime 101", Episode: 101}}, nil
}

func (p *handlerFeedParser) FetchAndParseWithTimeout(string, time.Duration) ([]rss.RSSItem, error) {
	return p.FetchAndParse("")
}

func (p *handlerFeedParser) Parse(interface{}) ([]rss.RSSItem, error) { return nil, nil }
func (p *handlerFeedParser) ExtractFansub(string) string              { return "" }
func (p *handlerFeedParser) ExtractEpisode(string) int                { return 0 }
func (p *handlerFeedParser) SetProxy(string) error                    { return nil }
