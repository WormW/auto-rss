package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
)

// TaskHandler 任务处理器
type TaskHandler struct{}

// NewTaskHandler 创建任务处理器
func NewTaskHandler() *TaskHandler {
	return &TaskHandler{}
}

// GetCurrent 获取当前任务
func (h *TaskHandler) GetCurrent(c *gin.Context) {
	manager := task.GetManager()
	currentTask := manager.GetCurrentTask()

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"current":    currentTask,
			"is_running": manager.IsRunning(),
		},
	})
}

// GetHistory 获取任务历史
func (h *TaskHandler) GetHistory(c *gin.Context) {
	manager := task.GetManager()
	history := manager.GetTaskHistory()

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    history,
	})
}

// Cancel 取消当前任务
func (h *TaskHandler) Cancel(c *gin.Context) {
	manager := task.GetManager()

	if err := manager.CancelTask(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "任务取消请求已发送",
	})
}
