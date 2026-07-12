package task

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
)

var ErrTaskRunning = errors.New("task already running")

// TaskType 任务类型
type TaskType string

const (
	TaskTypeCollect     TaskType = "collect"     // 采集任务
	TaskTypeImport      TaskType = "import"      // 导入任务
	TaskTypeReplacement TaskType = "replacement" // 剧集资源替换任务
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"   // 等待中
	TaskStatusRunning   TaskStatus = "running"   // 运行中
	TaskStatusCompleted TaskStatus = "completed" // 已完成
	TaskStatusFailed    TaskStatus = "failed"    // 失败
	TaskStatusCancelled TaskStatus = "cancelled" // 已取消
)

// Task 任务信息
type Task struct {
	ID             string     `json:"id"`
	Type           TaskType   `json:"type"`
	Status         TaskStatus `json:"status"`
	SubscriptionID uint       `json:"subscription_id,omitempty"`
	Name           string     `json:"name"`
	Progress       int        `json:"progress"` // 进度 0-100
	Message        string     `json:"message"`  // 当前状态消息
	Error          string     `json:"error,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Result         any        `json:"result,omitempty"`
}

// Manager 任务管理器
type Manager struct {
	mu          sync.RWMutex
	currentTask *Task
	cancelFunc  context.CancelFunc
	taskHistory []*Task
	maxHistory  int
}

var (
	instance *Manager
	once     sync.Once
)

// GetManager 获取任务管理器单例
func GetManager() *Manager {
	once.Do(func() {
		instance = &Manager{
			maxHistory: 10,
		}
	})
	return instance
}

// GetCurrentTask 获取当前任务
func (m *Manager) GetCurrentTask() *Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentTask
}

// GetTaskHistory 获取任务历史
func (m *Manager) GetTaskHistory() []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Task, len(m.taskHistory))
	copy(result, m.taskHistory)
	return result
}

// IsRunning 检查是否有任务在运行
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentTask != nil && m.currentTask.Status == TaskStatusRunning
}

// StartTask 启动新任务
func (m *Manager) StartTask(taskType TaskType, subscriptionID uint, name string, fn func(ctx context.Context, task *Task) error) (*Task, error) {
	return m.StartPreparedTask(taskType, subscriptionID, name, nil, fn)
}

// StartPreparedTask atomically checks global task availability, prepares any
// external claim, and installs the task before starting its callback.
func (m *Manager) StartPreparedTask(taskType TaskType, subscriptionID uint, name string, prepare func() error, fn func(ctx context.Context, task *Task) error) (*Task, error) {
	m.mu.Lock()

	// 检查是否有任务在运行
	if m.currentTask != nil && m.currentTask.Status == TaskStatusRunning {
		runningTaskName := m.currentTask.Name
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: 已有任务在运行中: %s", ErrTaskRunning, runningTaskName)
	}
	if prepare != nil {
		if err := prepare(); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}

	// 创建新任务
	now := time.Now()
	task := &Task{
		ID:             fmt.Sprintf("%s_%d", taskType, now.UnixNano()),
		Type:           taskType,
		Status:         TaskStatusRunning,
		SubscriptionID: subscriptionID,
		Name:           name,
		Progress:       0,
		Message:        "任务开始",
		StartedAt:      &now,
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.currentTask = task
	m.cancelFunc = cancel
	m.mu.Unlock()

	logger.Info("Task started",
		"task_id", task.ID,
		"type", task.Type,
		"name", task.Name)

	// 异步执行任务
	go func() {
		err := fn(ctx, task)

		m.mu.Lock()
		defer m.mu.Unlock()

		now := time.Now()
		task.CompletedAt = &now

		if ctx.Err() == context.Canceled {
			task.Status = TaskStatusCancelled
			task.Message = "任务已取消"
			logger.Info("Task cancelled", "task_id", task.ID)
		} else if err != nil {
			task.Status = TaskStatusFailed
			task.Error = err.Error()
			task.Message = "任务失败"
			logger.Error("Task failed", "task_id", task.ID, "error", err.Error())
		} else {
			task.Status = TaskStatusCompleted
			task.Progress = 100
			task.Message = "任务完成"
			logger.Info("Task completed", "task_id", task.ID)
		}

		// 添加到历史记录
		m.taskHistory = append([]*Task{task}, m.taskHistory...)
		if len(m.taskHistory) > m.maxHistory {
			m.taskHistory = m.taskHistory[:m.maxHistory]
		}

		// 清除当前任务引用（保留在历史中）
		m.currentTask = nil
		m.cancelFunc = nil
	}()

	return task, nil
}

// CancelTask 取消当前任务
func (m *Manager) CancelTask() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// D-08: 在持有锁期间检查 currentTask 状态
	if m.currentTask == nil {
		return fmt.Errorf("没有正在运行的任务")
	}
	if m.currentTask.Status != TaskStatusRunning {
		return fmt.Errorf("任务不在运行中")
	}

	// D-09: 确保任务已完成时不调用 cancelFunc
	if m.cancelFunc == nil {
		return fmt.Errorf("取消函数不可用")
	}

	m.cancelFunc()
	logger.Info("Task cancel requested", "task_id", m.currentTask.ID)
	return nil
}

// IsTaskRunning 检查指定 ID 的任务是否仍在运行
func (m *Manager) IsTaskRunning(taskID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentTask != nil && m.currentTask.ID == taskID && m.currentTask.Status == TaskStatusRunning
}

// UpdateProgress 更新任务进度
func (m *Manager) UpdateProgress(progress int, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentTask != nil && m.currentTask.Status == TaskStatusRunning {
		m.currentTask.Progress = progress
		m.currentTask.Message = message
	}
}

// SetResult 设置任务结果
func (m *Manager) SetResult(result any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentTask != nil {
		m.currentTask.Result = result
	}
}
