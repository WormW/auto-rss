package handler

import (
	"errors"
	"net/http"

	"github.com/WormW/auto-rss/internal/service/subscriptiondiscovery"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SubscriptionDiscoveryHandler struct {
	service *subscriptiondiscovery.Service
}

func NewSubscriptionDiscoveryHandler(service *subscriptiondiscovery.Service) *SubscriptionDiscoveryHandler {
	return &SubscriptionDiscoveryHandler{service}
}
func (h *SubscriptionDiscoveryHandler) Prepare(c *gin.Context) {
	var input subscriptiondiscovery.PrepareInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "invalid discovery request"})
		return
	}
	draft, err := h.service.Prepare(c.Request.Context(), input)
	if err != nil {
		discoveryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": draft})
}
func (h *SubscriptionDiscoveryHandler) Confirm(c *gin.Context) {
	var input subscriptiondiscovery.ConfirmInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "invalid confirmation request"})
		return
	}
	sub, err := h.service.Confirm(c.Request.Context(), input)
	if err != nil {
		discoveryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sub})
}
func discoveryError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, subscriptiondiscovery.ErrConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"code": status, "message": err.Error()})
}
