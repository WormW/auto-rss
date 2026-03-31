<template>
  <div class="disk-monitor-page">
    <h2 class="page-title">磁盘监控</h2>

    <!-- 磁盘状态概览 -->
    <n-grid :cols="3" :x-gap="16" :y-gap="16">
      <n-gi v-for="disk in diskStatus" :key="disk.path">
        <n-card :class="['disk-card', `status-${disk.status}`]" size="small">
          <n-space vertical>
            <n-space justify="space-between" align="center">
              <n-h4 prefix="bar" align-text>{{ disk.path }}</n-h4>
              <n-tag :type="getStatusType(disk.status)" size="large">
                {{ getStatusText(disk.status) }}
              </n-tag>
            </n-space>

            <n-statistic label="可用空间" :value="formatSize(disk.free)">
              <template #suffix>
                <span class="unit">/ {{ formatSize(disk.total) }}</span>
              </template>
            </n-statistic>

            <n-progress
              :percentage="disk.usage_percent"
              :type="'line'"
              :status="getProgressStatus(disk.status)"
              :show-indicator="true"
            />

            <n-descriptions :column="2" size="small" label-placement="left">
              <n-descriptions-item label="使用率">{{ disk.usage_percent }}%</n-descriptions-item>
              <n-descriptions-item label="下载目录">{{ disk.download_path }}</n-descriptions-item>
            </n-descriptions>
          </n-space>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- 自动清理设置 -->
    <n-card title="自动清理设置" class="cleanup-card">
      <n-space vertical :size="16">
        <n-alert type="info" :show-icon="false">
          当磁盘空间低于阈值时，系统会自动清理历史下载文件。活跃下载（7天内）不会被删除。
        </n-alert>

        <n-form :model="cleanupSettings" label-placement="left" label-width="120px">
          <n-form-item label="启用自动清理">
            <n-switch v-model:value="cleanupSettings.enabled" />
          </n-form-item>

          <n-form-item label="清理策略">
            <n-radio-group v-model:value="cleanupSettings.strategy">
              <n-space>
                <n-radio value="age">按时间清理</n-radio>
                <n-radio value="space">按空间清理</n-radio>
                <n-radio value="hybrid">混合模式</n-radio>
              </n-space>
            </n-radio-group>
          </n-form-item>

          <n-form-item label="保留天数">
            <n-input-number
              v-model:value="cleanupSettings.retention_days"
              :min="1"
              :max="365"
              style="width: 150px"
            />
            <n-text depth="3" style="margin-left: 8px;">天前下载的文件会被清理</n-text>
          </n-form-item>

          <n-form-item label="最小保留空间">
            <n-input-number
              v-model:value="cleanupSettings.min_free_gb"
              :min="1"
              :max="100"
              style="width: 150px"
            />
            <n-text depth="3" style="margin-left: 8px;">GB</n-text>
          </n-form-item>

          <n-form-item label="警告阈值">
            <n-input-number
              v-model:value="cleanupSettings.warning_threshold_gb"
              :min="1"
              :max="50"
              style="width: 150px"
            />
            <n-text depth="3" style="margin-left: 8px;">GB - 低于此值发送警告通知</n-text>
          </n-form-item>

          <n-form-item label="临界阈值">
            <n-input-number
              v-model:value="cleanupSettings.critical_threshold_gb"
              :min="1"
              :max="20"
              style="width: 150px"
            />
            <n-text depth="3" style="margin-left: 8px;">GB - 低于此值必须清理</n-text>
          </n-form-item>
        </n-form>

        <n-space justify="end">
          <n-button @click="loadSettings">重置</n-button>
          <n-button type="primary" @click="saveSettings" :loading="saving">保存设置</n-button>
        </n-space>
      </n-space>
    </n-card>

    <!-- 清理历史 -->
    <n-card title="清理历史" class="history-card">
      <n-space vertical :size="16">
        <n-space>
          <n-button @click="loadCleanupHistory" :loading="loadingHistory">刷新</n-button>
          <n-button type="primary" @click="triggerCleanup" :loading="cleaning">立即清理</n-button>
        </n-space>

        <n-table :data="cleanupHistory" size="small">
          <thead>
            <tr>
              <th>时间</th>
              <th>类型</th>
              <th>清理前</th>
              <th>清理后</th>
              <th>删除文件数</th>
              <th>释放空间</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in cleanupHistory" :key="item.id">
              <td>{{ formatTime(item.created_at) }}</td>
              <td>
                <n-tag size="small" :type="item.trigger_type === 'auto' ? 'info' : 'warning'">
                  {{ item.trigger_type === 'auto' ? '自动' : '手动' }}
                </n-tag>
              </td>
              <td>{{ formatSize(item.before_free) }}</td>
              <td>{{ formatSize(item.after_free) }}</td>
              <td>{{ item.deleted_count }}</td>
              <td>{{ formatSize(item.freed_bytes) }}</td>
              <td>
                <n-tag :type="item.status === 'success' ? 'success' : 'error'" size="small">
                  {{ item.status === 'success' ? '成功' : '失败' }}
                </n-tag>
              </td>
            </tr>
          </tbody>
        </n-table>

        <n-empty v-if="cleanupHistory.length === 0" description="暂无清理记录" />
      </n-space>
    </n-card>

    <!-- 磁盘趋势图表 -->
    <n-card title="磁盘使用趋势" class="trend-card">
      <div ref="chartRef" class="chart-container"></div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
// import * as echarts from 'echarts'
import {
  NCard,
  NGrid,
  NGi,
  NSpace,
  NTag,
  NH4,
  NStatistic,
  NProgress,
  NDescriptions,
  NDescriptionsItem,
  NForm,
  NFormItem,
  NSwitch,
  NRadio,
  NRadioGroup,
  NInputNumber,
  NButton,
  NTable,
  NText,
  NAlert,
  NEmpty,
  useMessage
} from 'naive-ui'
import { diskApi, type DiskStatus, type CleanupSettings, type CleanupHistoryItem } from '@/api'

const message = useMessage()

// 磁盘状态
const diskStatus = ref<DiskStatus[]>([])

// 清理设置
const cleanupSettings = ref<CleanupSettings>({
  enabled: true,
  strategy: 'hybrid',
  retention_days: 30,
  min_free_gb: 10,
  warning_threshold_gb: 10,
  critical_threshold_gb: 5
})

// 清理历史
const cleanupHistory = ref<CleanupHistoryItem[]>([])

// 加载状态
const saving = ref(false)
const cleaning = ref(false)
const loadingHistory = ref(false)

// 图表
const chartRef = ref<HTMLElement | null>(null)
// let chart: echarts.ECharts | null = null

// 刷新定时器
let refreshTimer: ReturnType<typeof setInterval> | null = null

// 状态类型映射
const getStatusType = (status: string) => {
  const map: Record<string, any> = {
    'healthy': 'success',
    'warning': 'warning',
    'critical': 'error'
  }
  return map[status] || 'default'
}

// 状态文本映射
const getStatusText = (status: string) => {
  const map: Record<string, string> = {
    'healthy': '健康',
    'warning': '警告',
    'critical': '危险'
  }
  return map[status] || status
}

// 进度条状态
const getProgressStatus = (status: string) => {
  const map: Record<string, any> = {
    'healthy': 'success',
    'warning': 'warning',
    'critical': 'error'
  }
  return map[status] || 'default'
}

// 格式化大小
const formatSize = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(2) + ' ' + units[i]
}

// 格式化时间
const formatTime = (time: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString()
}

// 加载磁盘状态
const loadDiskStatus = async () => {
  try {
    const res: any = await diskApi.getStatus()
    diskStatus.value = res.data || []
    updateChart()
  } catch (error: any) {
    message.error(error.message || '加载磁盘状态失败')
  }
}

// 加载清理设置
const loadSettings = async () => {
  try {
    const res: any = await diskApi.getSettings()
    cleanupSettings.value = res.data
  } catch (error: any) {
    message.error(error.message || '加载设置失败')
  }
}

// 保存清理设置
const saveSettings = async () => {
  saving.value = true
  try {
    await diskApi.updateSettings(cleanupSettings.value)
    message.success('设置已保存')
  } catch (error: any) {
    message.error(error.message || '保存失败')
  } finally {
    saving.value = false
  }
}

// 加载清理历史
const loadCleanupHistory = async () => {
  loadingHistory.value = true
  try {
    const res: any = await diskApi.getHistory()
    cleanupHistory.value = res.data || []
  } catch (error: any) {
    message.error(error.message || '加载历史失败')
  } finally {
    loadingHistory.value = false
  }
}

// 触发清理
const triggerCleanup = async () => {
  cleaning.value = true
  try {
    const res: any = await diskApi.triggerCleanup()
    const result = res.data
    if (result.cleaned) {
      message.success(`清理完成，删除 ${result.deleted_count} 个文件，释放 ${formatSize(result.freed_bytes)}`)
    } else {
      message.info('无需清理，磁盘空间充足')
    }
    loadDiskStatus()
    loadCleanupHistory()
  } catch (error: any) {
    message.error(error.message || '清理失败')
  } finally {
    cleaning.value = false
  }
}

// 初始化图表
const initChart = () => {
  // 图表功能暂不使用，简化界面
}

// 更新图表数据
const updateChart = () => {
  // 图表功能暂不使用
}

// 页面可见性变化处理
const handleVisibilityChange = () => {
  if (document.visibilityState === 'visible') {
    loadDiskStatus()
  }
}

onMounted(async () => {
  await loadDiskStatus()
  await loadSettings()
  await loadCleanupHistory()

  nextTick(() => {
    initChart()
  })

  // 每 30 秒刷新一次
  refreshTimer = setInterval(() => {
    loadDiskStatus()
  }, 30000)

  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onUnmounted(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
  }
  // chart cleanup if needed
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.disk-monitor-page {
  max-width: 1400px;
  margin: 0 auto;
}

.page-title {
  margin-bottom: 20px;
}

.disk-card {
  transition: all 0.3s;
}

.disk-card.status-critical {
  border: 2px solid var(--n-color-error);
}

.disk-card.status-warning {
  border: 2px solid var(--n-color-warning);
}

.unit {
  font-size: 12px;
  color: var(--n-text-color-3);
}

.cleanup-card,
.history-card,
.trend-card {
  margin-top: 20px;
}

.chart-container {
  width: 100%;
  height: 300px;
}
</style>
