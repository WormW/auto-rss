package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/calendar"
	"github.com/gin-gonic/gin"
)

// CalendarHandler 追番日历处理器
type CalendarHandler struct {
	calendarSvc *calendar.Calendar
}

// NewCalendarHandler 创建日历处理器实例
func NewCalendarHandler(subscriptionRepo repository.SubscriptionRepository, downloadRepo repository.DownloadRepository) *CalendarHandler {
	return &CalendarHandler{
		calendarSvc: calendar.NewCalendar(subscriptionRepo, downloadRepo),
	}
}

// GetWeekSchedule 获取本周排期
func (h *CalendarHandler) GetWeekSchedule(c *gin.Context) {
	weekOffset, _ := strconv.Atoi(c.DefaultQuery("week", "0"))

	schedule, err := h.calendarSvc.GetWeekSchedule(weekOffset)
	if err != nil {
		logger.Error("Failed to get week schedule", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取日历失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    schedule,
	})
}

// GetTodaySchedule 获取今日排期
func (h *CalendarHandler) GetTodaySchedule(c *gin.Context) {
	items, err := h.calendarSvc.GetTodaySchedule()
	if err != nil {
		logger.Error("Failed to get today schedule", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取今日更新失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    items,
	})
}
