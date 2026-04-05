package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetManager resets the singleton for test isolation
func resetManager() {
	instance = nil
	once = sync.Once{}
}

func TestManager_ConcurrentIsRunning(t *testing.T) {
	resetManager()
	manager := GetManager()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.IsRunning()
		}()
	}

	wg.Wait()
	// Test passes if no race condition detected
}

func TestManager_ConcurrentGetCurrentTask(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a task first
	_, err := manager.StartTask(TaskTypeCollect, 1, "test-task", func(ctx context.Context, t *Task) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.GetCurrentTask()
		}()
	}

	wg.Wait()
}

func TestManager_ConcurrentGetTaskHistory(t *testing.T) {
	resetManager()
	manager := GetManager()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.GetTaskHistory()
		}()
	}

	wg.Wait()
}

func TestManager_ConcurrentStartAndCancel(t *testing.T) {
	resetManager()
	manager := GetManager()

	var wg sync.WaitGroup

	// Multiple goroutines trying to start tasks
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, _ = manager.StartTask(TaskTypeCollect, uint(id), "test-task", func(ctx context.Context, t *Task) error {
				time.Sleep(50 * time.Millisecond)
				return nil
			})
		}(i)
	}

	// Concurrent cancels
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			_ = manager.CancelTask()
		}()
	}

	wg.Wait()
}

func TestManager_ConcurrentUpdateProgress(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a task
	_, err := manager.StartTask(TaskTypeCollect, 1, "test-task", func(ctx context.Context, t *Task) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(progress int) {
			defer wg.Done()
			manager.UpdateProgress(progress, "updating")
		}(i)
	}

	wg.Wait()
}

func TestManager_ConcurrentSetResult(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a task
	_, err := manager.StartTask(TaskTypeCollect, 1, "test-task", func(ctx context.Context, t *Task) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			manager.SetResult(map[string]int{"id": id})
		}(i)
	}

	wg.Wait()
}

func TestManager_StartTask_ExclusiveExecution(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start first task
	task1, err := manager.StartTask(TaskTypeCollect, 1, "first-task", func(ctx context.Context, t *Task) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)
	assert.NotNil(t, task1)

	// Try to start second task while first is running
	task2, err := manager.StartTask(TaskTypeCollect, 2, "second-task", func(ctx context.Context, t *Task) error {
		return nil
	})
	assert.Error(t, err)
	assert.Nil(t, task2)
	assert.Contains(t, err.Error(), "已有任务在运行中")
}

func TestManager_CancelTask_NoRunningTask(t *testing.T) {
	resetManager()
	manager := GetManager()

	err := manager.CancelTask()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "没有正在运行的任务")
}

func TestManager_IsTaskRunning(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a task
	task, err := manager.StartTask(TaskTypeCollect, 1, "test-task", func(ctx context.Context, t *Task) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	// Should be running
	assert.True(t, manager.IsTaskRunning(task.ID))

	// Wait for task to complete
	time.Sleep(300 * time.Millisecond)

	// Should not be running anymore
	assert.False(t, manager.IsTaskRunning(task.ID))
}

func TestManager_TaskHistory(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Complete a few tasks
	for i := 0; i < 5; i++ {
		task, err := manager.StartTask(TaskTypeCollect, uint(i), "test-task", func(ctx context.Context, t *Task) error {
			return nil
		})
		require.NoError(t, err)

		// Wait for completion
		time.Sleep(50 * time.Millisecond)
		assert.False(t, manager.IsTaskRunning(task.ID))
	}

	history := manager.GetTaskHistory()
	assert.GreaterOrEqual(t, len(history), 5)
}

func TestManager_TaskHistory_MaxLimit(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Complete more tasks than maxHistory
	for i := 0; i < 15; i++ {
		task, err := manager.StartTask(TaskTypeCollect, uint(i), "test-task", func(ctx context.Context, t *Task) error {
			return nil
		})
		require.NoError(t, err)

		// Wait for completion
		time.Sleep(20 * time.Millisecond)
		assert.False(t, manager.IsTaskRunning(task.ID))
	}

	history := manager.GetTaskHistory()
	// maxHistory is 10, so history should be capped
	assert.LessOrEqual(t, len(history), 10)
}

func TestManager_TaskLifecycle(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Initially no task running
	assert.False(t, manager.IsRunning())
	assert.Nil(t, manager.GetCurrentTask())

	// Start a task
	task, err := manager.StartTask(TaskTypeCollect, 1, "lifecycle-task", func(ctx context.Context, t *Task) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)
	assert.NotNil(t, task)
	assert.Equal(t, TaskStatusRunning, task.Status)
	assert.Equal(t, "lifecycle-task", task.Name)

	// Task should be running
	assert.True(t, manager.IsRunning())
	assert.NotNil(t, manager.GetCurrentTask())
	assert.Equal(t, task.ID, manager.GetCurrentTask().ID)

	// Wait for completion
	time.Sleep(100 * time.Millisecond)

	// Task should be completed
	assert.False(t, manager.IsRunning())
	assert.Nil(t, manager.GetCurrentTask())

	// Check history
	history := manager.GetTaskHistory()
	require.GreaterOrEqual(t, len(history), 1)
	assert.Equal(t, task.ID, history[0].ID)
	assert.Equal(t, TaskStatusCompleted, history[0].Status)
}

func TestManager_CancelRunningTask(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a long-running task
	_, err := manager.StartTask(TaskTypeCollect, 1, "cancellable-task", func(ctx context.Context, t *Task) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})
	require.NoError(t, err)

	// Verify task is running
	assert.True(t, manager.IsRunning())

	// Cancel the task
	err = manager.CancelTask()
	require.NoError(t, err)

	// Wait for cancellation to complete
	time.Sleep(100 * time.Millisecond)

	// Verify task is no longer running
	assert.False(t, manager.IsRunning())

	// Check history
	history := manager.GetTaskHistory()
	require.GreaterOrEqual(t, len(history), 1)
	assert.Equal(t, TaskStatusCancelled, history[0].Status)
}

func TestManager_TaskFailure(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a task that will fail
	_, err := manager.StartTask(TaskTypeCollect, 1, "failing-task", func(ctx context.Context, t *Task) error {
		return assert.AnError
	})
	require.NoError(t, err)

	// Wait for task to complete
	time.Sleep(50 * time.Millisecond)

	// Check history
	history := manager.GetTaskHistory()
	require.GreaterOrEqual(t, len(history), 1)
	assert.Equal(t, TaskStatusFailed, history[0].Status)
	assert.NotEmpty(t, history[0].Error)
}

func TestManager_UpdateProgressAndResult(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a task
	_, err := manager.StartTask(TaskTypeCollect, 1, "progress-task", func(ctx context.Context, t *Task) error {
		// Simulate work with progress updates
		for i := 0; i <= 100; i += 25 {
			manager.UpdateProgress(i, "processing")
			time.Sleep(10 * time.Millisecond)
		}
		manager.SetResult(map[string]string{"status": "done"})
		return nil
	})
	require.NoError(t, err)

	// Wait for task to complete
	time.Sleep(200 * time.Millisecond)

	// Check history
	history := manager.GetTaskHistory()
	require.GreaterOrEqual(t, len(history), 1)
	assert.Equal(t, TaskStatusCompleted, history[0].Status)
	assert.NotNil(t, history[0].Result)
}

func TestManager_MultipleConcurrentOperations(t *testing.T) {
	resetManager()
	manager := GetManager()

	// Start a task
	_, err := manager.StartTask(TaskTypeCollect, 1, "concurrent-ops", func(ctx context.Context, t *Task) error {
		time.Sleep(500 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	var wg sync.WaitGroup

	// Mix of operations
	for i := 0; i < 50; i++ {
		wg.Add(4)

		go func() {
			defer wg.Done()
			_ = manager.IsRunning()
		}()

		go func() {
			defer wg.Done()
			_ = manager.GetCurrentTask()
		}()

		go func() {
			defer wg.Done()
			_ = manager.GetTaskHistory()
		}()

		go func(id int) {
			defer wg.Done()
			manager.UpdateProgress(id, "concurrent update")
		}(i)
	}

	wg.Wait()
}

func TestManager_GetManager_Singleton(t *testing.T) {
	resetManager()

	// Get manager multiple times
	m1 := GetManager()
	m2 := GetManager()
	m3 := GetManager()

	// All should be the same instance
	assert.Same(t, m1, m2)
	assert.Same(t, m2, m3)
}
