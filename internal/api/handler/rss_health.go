package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
)

// RSSHealthHandler RSS健康检查处理器
type RSSHealthHandler struct {
	healthChecker *rss.RSSHealthChecker
	subRepo       repository.SubscriptionRepository
	configRepo    repository.ConfigRepository
}

// NewRSSHealthHandler 创建RSS健康检查处理器实例
func NewRSSHealthHandler(healthChecker *rss.RSSHealthChecker, subRepo repository.SubscriptionRepository, configRepos ...repository.ConfigRepository) *RSSHealthHandler {
	handler := &RSSHealthHandler{
		healthChecker: healthChecker,
		subRepo:       subRepo,
	}
	if len(configRepos) > 0 {
		handler.configRepo = configRepos[0]
	}
	return handler
}

// HealthCheckSummaryResponse 健康检查汇总响应
type HealthCheckSummaryResponse struct {
	Results   []*rss.HealthCheckResult `json:"results"`
	Summary   HealthCheckSummary       `json:"summary"`
	CheckedAt time.Time                `json:"checked_at"`
}

// HealthCheckSummary 健康检查统计
type HealthCheckSummary struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Unhealthy int `json:"unhealthy"`
	Dead      int `json:"dead"`
	Unknown   int `json:"unknown"`
}

// CheckAll 检查所有订阅的健康状态
// GET /api/v1/rss/health
func (h *RSSHealthHandler) CheckAll(c *gin.Context) {
	logger.Info("Checking all RSS subscriptions health", "client_ip", c.ClientIP())
	if !h.applySystemProxy(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	results, err := h.healthChecker.CheckAllSubscriptions(ctx)
	if err != nil {
		logger.Error("Failed to check all subscriptions health", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to check subscriptions health",
		})
		return
	}

	// 计算统计信息
	summary := calculateSummary(results)

	logger.Info("RSS health check completed",
		"total", summary.Total,
		"healthy", summary.Healthy,
		"unhealthy", summary.Unhealthy,
		"dead", summary.Dead)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": HealthCheckSummaryResponse{
			Results:   results,
			Summary:   summary,
			CheckedAt: time.Now(),
		},
	})
}

// CheckOne 检查单个订阅的健康状态
// GET /api/v1/rss/health/:subscription_id
func (h *RSSHealthHandler) CheckOne(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("subscription_id"), 10, 32)
	if err != nil {
		logger.Warn("Invalid subscription ID in health check request",
			"id_param", c.Param("subscription_id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	logger.Info("Checking single subscription health",
		"subscription_id", id,
		"client_ip", c.ClientIP())

	// 获取订阅信息
	sub, err := h.subRepo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get subscription for health check",
			"subscription_id", id,
			"error", err.Error())
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Subscription not found",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if !h.applySystemProxy(c) {
		return
	}

	result := h.healthChecker.CheckSubscription(ctx, sub)

	logger.Info("Single subscription health check completed",
		"subscription_id", id,
		"name", result.Name,
		"status", result.Status,
		"response_time_ms", result.ResponseTime)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    result,
	})
}

// GetDead 获取所有失效的RSS订阅
// GET /api/v1/rss/dead
func (h *RSSHealthHandler) GetDead(c *gin.Context) {
	logger.Info("Getting dead RSS subscriptions", "client_ip", c.ClientIP())
	if !h.applySystemProxy(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	deadSubs, err := h.healthChecker.GetDeadSubscriptions(ctx)
	if err != nil {
		logger.Error("Failed to get dead subscriptions", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get dead subscriptions",
		})
		return
	}

	logger.Info("Dead subscriptions retrieved", "count", len(deadSubs))

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"count": len(deadSubs),
			"items": deadSubs,
		},
	})
}

// TriggerCheckResponse 触发检查响应
type TriggerCheckResponse struct {
	TaskID    string    `json:"task_id"`
	TaskName  string    `json:"task_name"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Message   string    `json:"message"`
}

// TriggerCheck 触发异步健康检查任务
// POST /api/v1/rss/health-check
func (h *RSSHealthHandler) TriggerCheck(c *gin.Context) {
	logger.Info("Triggering async RSS health check", "client_ip", c.ClientIP())
	if !h.applySystemProxy(c) {
		return
	}

	manager := task.GetManager()

	// 检查是否已有任务在运行
	if manager.IsRunning() {
		currentTask := manager.GetCurrentTask()
		if currentTask != nil && currentTask.Type == "rss_health_check" {
			logger.Warn("RSS health check already running",
				"task_id", currentTask.ID)
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "RSS health check already in progress",
				"data": gin.H{
					"task_id": currentTask.ID,
					"status":  currentTask.Status,
				},
			})
			return
		}
	}

	// 启动异步任务
	newTask, err := manager.StartTask("rss_health_check", 0, "RSS订阅健康检查", func(ctx context.Context, t *task.Task) error {
		return h.runAsyncHealthCheck(ctx, t)
	})

	if err != nil {
		logger.Error("Failed to start RSS health check task", "error", err.Error())
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": err.Error(),
		})
		return
	}

	logger.Info("RSS health check task started", "task_id", newTask.ID)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "RSS health check task started",
		"data": TriggerCheckResponse{
			TaskID:    newTask.ID,
			TaskName:  newTask.Name,
			Status:    string(newTask.Status),
			StartedAt: *newTask.StartedAt,
			Message:   "任务已启动，请通过 /api/v1/tasks/current 查看进度",
		},
	})
}

func (h *RSSHealthHandler) applySystemProxy(c *gin.Context) bool {
	proxyURL := ""
	if h.configRepo != nil {
		if config, err := h.configRepo.Get("system_proxy"); err == nil && config != nil {
			proxyURL = config.Value
		}
	}
	if err := h.healthChecker.SetProxy(proxyURL); err != nil {
		logger.Error("Failed to apply RSS health check proxy", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "RSS 代理配置无效",
		})
		return false
	}
	return true
}

// runAsyncHealthCheck 执行异步健康检查
func (h *RSSHealthHandler) runAsyncHealthCheck(ctx context.Context, t *task.Task) error {
	manager := task.GetManager()
	manager.UpdateProgress(5, "正在获取订阅列表...")

	// 获取所有活跃订阅
	subs, err := h.subRepo.GetActiveSubscriptions()
	if err != nil {
		logger.Error("Failed to get active subscriptions for health check", "error", err.Error())
		return err
	}

	totalSubs := len(subs)
	if totalSubs == 0 {
		manager.UpdateProgress(100, "没有需要检查的订阅")
		manager.SetResult(gin.H{
			"checked": 0,
			"healthy": 0,
			"dead":    0,
		})
		return nil
	}

	manager.UpdateProgress(10, "开始检查订阅健康状态...")

	var results []*rss.HealthCheckResult
	for i, sub := range subs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 检查单个订阅
		result := h.healthChecker.CheckSubscription(ctx, &sub)
		results = append(results, result)

		// 更新进度
		progress := 10 + (i+1)*90/totalSubs
		message := "检查中..."
		if result.Status == rss.HealthStatusDead {
			message = "发现失效订阅"
		}
		manager.UpdateProgress(progress, message)

		// 避免请求过快
		if i < totalSubs-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}

	manager.UpdateProgress(100, "检查完成")

	// 计算统计信息
	summary := calculateSummary(results)

	manager.SetResult(gin.H{
		"checked":   totalSubs,
		"healthy":   summary.Healthy,
		"unhealthy": summary.Unhealthy,
		"dead":      summary.Dead,
		"unknown":   summary.Unknown,
		"results":   results,
	})

	logger.Info("Async RSS health check completed",
		"total", summary.Total,
		"healthy", summary.Healthy,
		"unhealthy", summary.Unhealthy,
		"dead", summary.Dead)

	return nil
}

// calculateSummary 计算健康检查统计信息
func calculateSummary(results []*rss.HealthCheckResult) HealthCheckSummary {
	summary := HealthCheckSummary{
		Total: len(results),
	}

	for _, r := range results {
		switch r.Status {
		case rss.HealthStatusHealthy:
			summary.Healthy++
		case rss.HealthStatusUnhealthy:
			summary.Unhealthy++
		case rss.HealthStatusDead:
			summary.Dead++
		case rss.HealthStatusUnknown:
			summary.Unknown++
		}
	}

	return summary
}
