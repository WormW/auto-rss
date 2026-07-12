package handler

import (
	"bytes"
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
	feedHandler := NewSubscriptionFeedHandler(feedRepo, feedService, episode.NewService(episodeRepo))
	router := gin.New()
	subscriptions := router.Group("/subscriptions")
	subscriptions.POST("", subHandler.Create)
	subscriptions.PUT("/:id", subHandler.Update)
	registerSubscriptionFeedRoutes(subscriptions, feedHandler)
	return &subscriptionFeedHandlerFixture{t: t, db: db, router: router}
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
