<template>
  <n-popover trigger="click" placement="bottom-end" :show="showPopover" @update:show="handlePopoverChange">
    <template #trigger>
      <n-button text :type="hasRunningTask ? 'primary' : 'default'" @click="togglePopover">
        <template #icon>
          <n-icon>
            <svg v-if="hasRunningTask" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="spinning">
              <path fill="currentColor" d="M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2zm0 18a8 8 0 1 1 8-8a8 8 0 0 1-8 8z" opacity=".5"/>
              <path fill="currentColor" d="M12 4a8 8 0 0 1 8 8h2A10 10 0 0 0 12 2v2z"/>
            </svg>
            <svg v-else xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
              <path fill="currentColor" d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10s10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5l1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"/>
            </svg>
          </n-icon>
        </template>
        {{ hasRunningTask ? '任务执行中' : '任务管理' }}
      </n-button>
    </template>

    <div style="width: 400px; max-height: 500px; overflow-y: auto;">
      <div style="padding: 12px;">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;">
          <span style="font-weight: bold; font-size: 16px;">任务管理</span>
          <n-button text size="small" @click="refreshTasks">
            刷新
          </n-button>
        </div>

        <!-- 当前任务 -->
        <div v-if="currentTask" style="margin-bottom: 16px;">
          <div style="font-size: 14px; color: #666; margin-bottom: 8px;">当前任务</div>
          <n-card size="small">
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span>{{ currentTask.name }}</span>
              <n-tag :type="getStatusType(currentTask.status)" size="small">
                {{ getStatusText(currentTask.status) }}
              </n-tag>
            </div>
            <n-progress
              v-if="currentTask.status === 'running'"
              :percentage="currentTask.progress"
              :show-indicator="true"
              style="margin-top: 8px;"
            />
            <div style="font-size: 12px; color: #999; margin-top: 4px;">
              {{ currentTask.message }}
            </div>
            <n-button
              v-if="currentTask.status === 'running'"
              type="error"
              size="small"
              style="margin-top: 8px;"
              @click="cancelTask"
              :loading="cancelling"
            >
              取消任务
            </n-button>
          </n-card>
        </div>
        <div v-else style="margin-bottom: 16px;">
          <div style="font-size: 14px; color: #666; margin-bottom: 8px;">当前任务</div>
          <div style="text-align: center; color: #999; padding: 16px;">
            暂无正在执行的任务
          </div>
        </div>

        <!-- 任务历史 -->
        <div>
          <div style="font-size: 14px; color: #666; margin-bottom: 8px;">历史任务</div>
          <div v-if="taskHistory.length === 0" style="text-align: center; color: #999; padding: 16px;">
            暂无历史任务
          </div>
          <div v-else>
            <n-card v-for="task in taskHistory" :key="task.id" size="small" style="margin-bottom: 8px;">
              <div style="display: flex; justify-content: space-between; align-items: center;">
                <span style="font-size: 13px;">{{ task.name }}</span>
                <n-tag :type="getStatusType(task.status)" size="small">
                  {{ getStatusText(task.status) }}
                </n-tag>
              </div>
              <div v-if="task.error" style="font-size: 12px; color: #f56c6c; margin-top: 4px;">
                {{ task.error }}
              </div>
              <div v-if="task.result" style="font-size: 12px; color: #67c23a; margin-top: 4px;">
                收集: {{ task.result.collected || 0 }} 条
              </div>
              <div style="font-size: 11px; color: #999; margin-top: 4px;">
                {{ formatTime(task.completed_at) }}
              </div>
            </n-card>
          </div>
        </div>
      </div>
    </div>
  </n-popover>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { NPopover, NButton, NIcon, NCard, NTag, NProgress, useMessage } from 'naive-ui'
import { taskApi, type Task } from '../api'

const message = useMessage()

const showPopover = ref(false)
const currentTask = ref<Task | null>(null)
const isRunning = ref(false)
const taskHistory = ref<Task[]>([])
const cancelling = ref(false)

let pollInterval: ReturnType<typeof setInterval> | null = null

const hasRunningTask = computed(() => isRunning.value)

const togglePopover = () => {
  showPopover.value = !showPopover.value
}

const handlePopoverChange = (show: boolean) => {
  showPopover.value = show
}

const getStatusType = (status: string) => {
  switch (status) {
    case 'running':
      return 'info'
    case 'completed':
      return 'success'
    case 'failed':
      return 'error'
    case 'cancelled':
      return 'warning'
    default:
      return 'default'
  }
}

const getStatusText = (status: string) => {
  switch (status) {
    case 'pending':
      return '等待中'
    case 'running':
      return '执行中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '失败'
    case 'cancelled':
      return '已取消'
    default:
      return status
  }
}

const formatTime = (time?: string) => {
  if (!time) return ''
  const date = new Date(time)
  return date.toLocaleString('zh-CN')
}

const refreshTasks = async () => {
  try {
    const [currentRes, historyRes] = await Promise.all([
      taskApi.getCurrent(),
      taskApi.getHistory()
    ])

    currentTask.value = (currentRes as any).data?.current || null
    isRunning.value = (currentRes as any).data?.is_running || false
    taskHistory.value = (historyRes as any).data || []
  } catch (error) {
    console.error('Failed to fetch tasks:', error)
  }
}

const cancelTask = async () => {
  try {
    cancelling.value = true
    await taskApi.cancel()
    message.success('取消请求已发送')
    await refreshTasks()
  } catch (error: any) {
    message.error(error.response?.data?.message || '取消失败')
  } finally {
    cancelling.value = false
  }
}

onMounted(() => {
  refreshTasks()
  // 每2秒轮询一次
  pollInterval = setInterval(refreshTasks, 2000)
})

onUnmounted(() => {
  if (pollInterval) {
    clearInterval(pollInterval)
  }
})
</script>

<style scoped>
.spinning {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
</style>
