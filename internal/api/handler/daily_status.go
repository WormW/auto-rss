package handler

import (
	"errors"
	"net/http"

	"github.com/WormW/auto-rss/internal/service/dailystatus"
	"github.com/gin-gonic/gin"
)

type DailyStatusHandler struct {
	service *dailystatus.Service
}

func NewDailyStatusHandler(service *dailystatus.Service) *DailyStatusHandler {
	return &DailyStatusHandler{service: service}
}

// Get returns the daily schedule and collection state for an external agent.
// Query parameters date and timezone are optional; both default to the
// server's local civil day and timezone.
func (h *DailyStatusHandler) Get(c *gin.Context) {
	status, err := h.service.Get(c.Request.Context(), c.Query("date"), c.Query("timezone"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		code := 3000
		if errors.Is(err, dailystatus.ErrInvalidDate) || errors.Is(err, dailystatus.ErrInvalidTimezone) {
			statusCode = http.StatusBadRequest
			code = 2000
		}
		c.JSON(statusCode, gin.H{"code": code, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "Success", "data": status})
}
