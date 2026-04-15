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
  useMessage
} from 'naive-ui'
import { diskApi, type DiskStatus } from '@/api'

const message = useMessage()

// 磁盘状态
const diskStatus = ref<DiskStatus[]>([])

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

.trend-card {
  margin-top: 20px;
}

.chart-container {
  width: 100%;
  height: 300px;
}
</style>
