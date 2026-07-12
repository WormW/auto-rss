package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/subscription"
	"github.com/WormW/auto-rss/internal/service/subscriptionfeed"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SubscriptionFeedHandler struct {
	repo           repository.SubscriptionFeedRepository
	service        *subscriptionfeed.Service
	episodeService *episode.Service
}

func NewSubscriptionFeedHandler(
	repo repository.SubscriptionFeedRepository,
	service *subscriptionfeed.Service,
	episodeService *episode.Service,
) *SubscriptionFeedHandler {
	return &SubscriptionFeedHandler{repo: repo, service: service, episodeService: episodeService}
}

func registerSubscriptionFeedRoutes(group *gin.RouterGroup, handler *SubscriptionFeedHandler) {
	group.POST("/feeds/preview", handler.PreviewDetached)
	group.GET("/:id/feeds", handler.List)
	group.POST("/:id/feeds", handler.Create)
	group.PUT("/:id/feeds/:feedId", handler.Update)
	group.DELETE("/:id/feeds/:feedId", handler.Delete)
	group.POST("/:id/feeds/preview", handler.PreviewNew)
	group.POST("/:id/feeds/:feedId/preview", handler.PreviewExisting)
}

func (h *SubscriptionFeedHandler) PreviewDetached(c *gin.Context) {
	h.preview(c)
}

func RegisterSubscriptionFeedRoutes(group *gin.RouterGroup, handler *SubscriptionFeedHandler) {
	registerSubscriptionFeedRoutes(group, handler)
}

func (h *SubscriptionFeedHandler) List(c *gin.Context) {
	subscriptionID, ok := parseFeedID(c, "id")
	if !ok {
		return
	}
	feeds, err := h.repo.ListBySubscription(subscriptionID)
	if err != nil {
		feedError(c, err)
		return
	}
	feedSuccess(c, feeds)
}

func (h *SubscriptionFeedHandler) Create(c *gin.Context) {
	subscriptionID, ok := parseFeedID(c, "id")
	if !ok {
		return
	}
	input, ok := bindFeedInput(c)
	if !ok {
		return
	}
	feed, err := h.service.Create(c.Request.Context(), subscriptionID, input)
	if err != nil {
		feedError(c, err)
		return
	}
	h.refreshProgress(subscriptionID)
	feedSuccess(c, feed)
}

func (h *SubscriptionFeedHandler) Update(c *gin.Context) {
	subscriptionID, feedID, ok := h.feedScope(c)
	if !ok {
		return
	}
	input, ok := bindFeedInput(c)
	if !ok {
		return
	}
	feed, err := h.service.Update(c.Request.Context(), feedID, input)
	if err != nil {
		feedError(c, err)
		return
	}
	h.refreshProgress(subscriptionID)
	feedSuccess(c, feed)
}

func (h *SubscriptionFeedHandler) Delete(c *gin.Context) {
	subscriptionID, feedID, ok := h.feedScope(c)
	if !ok {
		return
	}
	if err := h.service.Delete(feedID); err != nil {
		feedError(c, err)
		return
	}
	h.refreshProgress(subscriptionID)
	feedSuccess(c, gin.H{"id": feedID})
}

func (h *SubscriptionFeedHandler) PreviewNew(c *gin.Context) {
	if _, ok := parseFeedID(c, "id"); !ok {
		return
	}
	h.preview(c)
}

func (h *SubscriptionFeedHandler) PreviewExisting(c *gin.Context) {
	if _, _, ok := h.feedScope(c); !ok {
		return
	}
	h.preview(c)
}

func (h *SubscriptionFeedHandler) preview(c *gin.Context) {
	input, ok := bindFeedInput(c)
	if !ok {
		return
	}
	preview, err := h.service.Preview(c.Request.Context(), input)
	if err != nil {
		feedError(c, err)
		return
	}
	feedSuccess(c, preview)
}

func (h *SubscriptionFeedHandler) feedScope(c *gin.Context) (uint, uint, bool) {
	subscriptionID, ok := parseFeedID(c, "id")
	if !ok {
		return 0, 0, false
	}
	feedID, ok := parseFeedID(c, "feedId")
	if !ok {
		return 0, 0, false
	}
	feed, err := h.repo.GetByID(feedID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && feed.SubscriptionID != subscriptionID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "Feed not found"})
		return 0, 0, false
	}
	if err != nil {
		feedError(c, err)
		return 0, 0, false
	}
	return subscriptionID, feedID, true
}

func (h *SubscriptionFeedHandler) refreshProgress(subscriptionID uint) {
	if h.episodeService != nil {
		_ = h.episodeService.RefreshSubscriptionProgress(subscriptionID)
	}
}

func parseFeedID(c *gin.Context, name string) (uint, bool) {
	parsed, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil || parsed == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid ID"})
		return 0, false
	}
	return uint(parsed), true
}

func bindFeedInput(c *gin.Context) (subscriptionfeed.Input, bool) {
	var input subscriptionfeed.Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid request body"})
		return subscriptionfeed.Input{}, false
	}
	return input, true
}

func feedSuccess(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "Success", "data": data})
}

func feedError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "Feed operation failed"
	switch {
	case errors.Is(err, subscriptionfeed.ErrInvalidURL),
		errors.Is(err, subscriptionfeed.ErrNegativeOffset),
		errors.Is(err, subscriptionfeed.ErrNoMappableEpisodes):
		status = http.StatusUnprocessableEntity
		message = err.Error()
	case errors.As(err, new(*subscriptionfeed.FetchError)):
		status = http.StatusBadGateway
		message = err.Error()
	case errors.Is(err, gorm.ErrRecordNotFound):
		status = http.StatusNotFound
		message = "Feed not found"
	case errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "UNIQUE constraint failed"):
		status = http.StatusConflict
		message = "Feed URL already exists for this subscription"
	case errors.Is(err, subscription.ErrDuplicateInitialFeed):
		status = http.StatusConflict
		message = "Duplicate feed URL in request"
	}
	c.JSON(status, gin.H{"code": status, "message": message})
}
