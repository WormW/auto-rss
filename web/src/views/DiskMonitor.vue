<template>
  <div class="disk-monitor-page">
    <div class="page-header">
      <h2 class="page-title">磁盘监控</h2>
      <n-button :loading="loading" size="small" @click="loadDiskData">
        刷新
      </n-button>
    </div>

    <n-alert
      v-if="settings && settings.media_library_status !== 'connected'"
      :type="settings.media_library_status === 'failed' ? 'error' : 'warning'"
      class="media-alert"
      :title="mediaStatusText"
    >
      {{ settings.media_library_message }}
    </n-alert>

    <n-grid :cols="3" :x-gap="16" :y-gap="16" responsive="screen">
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
              :percentage="Number(disk.usage_percent.toFixed(1))"
              type="line"
              :status="getProgressStatus(disk.status)"
              :show-indicator="true"
            />

            <n-descriptions :column="2" size="small" label-placement="left">
              <n-descriptions-item label="使用率">{{ disk.usage_percent.toFixed(1) }}%</n-descriptions-item>
              <n-descriptions-item label="下载目录">{{ disk.download_path }}</n-descriptions-item>
            </n-descriptions>
          </n-space>
        </n-card>
      </n-gi>
    </n-grid>

    <section class="trend-section">
      <div class="section-header">
        <h3>磁盘使用趋势</h3>
        <span class="section-subtitle">{{ trendSubtitle }}</span>
      </div>
      <div class="trend-chart" role="img" aria-label="磁盘使用率趋势">
        <svg v-if="trendPoints.length > 1" viewBox="0 0 720 260" preserveAspectRatio="none">
          <defs>
            <linearGradient id="diskTrendFill" x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stop-color="#18a058" stop-opacity="0.24" />
              <stop offset="100%" stop-color="#18a058" stop-opacity="0.02" />
            </linearGradient>
          </defs>
          <path :d="trendAreaPath" fill="url(#diskTrendFill)" />
          <path :d="trendLinePath" fill="none" stroke="#18a058" stroke-width="3" stroke-linecap="round" />
          <circle
            v-for="point in trendPoints"
            :key="point.key"
            :cx="point.x"
            :cy="point.y"
            r="3"
            fill="#18a058"
          />
        </svg>
        <n-empty v-else description="暂无趋势数据" />
      </div>
      <div v-if="latestSample" class="trend-stats">
        <span>当前使用率 {{ latestSample.usage_percent.toFixed(1) }}%</span>
        <span>可用 {{ formatSize(latestSample.free) }}</span>
        <span>更新时间 {{ formatTime(latestSample.created_at) }}</span>
      </div>
    </section>

    <section class="cleanup-section">
      <div class="section-header">
        <h3>清理记录</h3>
        <n-button size="small" type="primary" :loading="cleanupLoading" @click="runCleanup">
          手动清理
        </n-button>
      </div>
      <n-empty v-if="cleanupRecords.length === 0" description="暂无清理记录" />
      <div v-else class="cleanup-list">
        <div v-for="record in cleanupRecords" :key="record.id" class="cleanup-row">
          <div>
            <strong>{{ record.trigger === 'auto' ? '自动清理' : '手动清理' }}</strong>
            <span>{{ formatTime(record.created_at) }}</span>
          </div>
          <div>
            删除 {{ record.deleted_count }} 项 / 跳过 {{ record.skipped_count }} 项 / 释放 {{ formatSize(record.freed_bytes) }}
          </div>
          <n-tag :type="record.media_library_status === 'failed' ? 'error' : 'default'" size="small">
            {{ getMediaStatusText(record.media_library_status) }}
          </n-tag>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NGi,
  NGrid,
  NH4,
  NProgress,
  NSpace,
  NStatistic,
  NTag,
  useMessage
} from 'naive-ui'
import { diskApi, type DiskCleanupRecord, type DiskHistory, type DiskSample, type DiskSettings, type DiskStatus } from '@/api'

const message = useMessage()

const diskStatus = ref<DiskStatus[]>([])
const samples = ref<DiskSample[]>([])
const cleanupRecords = ref<DiskCleanupRecord[]>([])
const settings = ref<DiskSettings | null>(null)
const loading = ref(false)
const cleanupLoading = ref(false)
let refreshTimer: ReturnType<typeof setInterval> | null = null

const mediaStatusText = computed(() => getMediaStatusText(settings.value?.media_library_status || 'unconfigured'))
const latestSample = computed(() => samples.value[samples.value.length - 1])
const trendSubtitle = computed(() => samples.value.length > 1 ? `${samples.value.length} 个采样点` : '等待采样')

const trendPoints = computed(() => {
  if (samples.value.length === 0) return []
  const width = 720
  const height = 260
  const paddingX = 28
  const paddingY = 24
  const innerWidth = width - paddingX * 2
  const innerHeight = height - paddingY * 2
  const maxIndex = Math.max(samples.value.length - 1, 1)

  return samples.value.map((sample, index) => ({
    key: `${sample.created_at}-${index}`,
    x: paddingX + (index / maxIndex) * innerWidth,
    y: paddingY + (1 - Math.min(Math.max(sample.usage_percent, 0), 100) / 100) * innerHeight
  }))
})

const trendLinePath = computed(() => trendPoints.value.map((point, index) => `${index === 0 ? 'M' : 'L'} ${point.x} ${point.y}`).join(' '))
const trendAreaPath = computed(() => {
  if (trendPoints.value.length === 0) return ''
  const first = trendPoints.value[0]
  const last = trendPoints.value[trendPoints.value.length - 1]
  return `${trendLinePath.value} L ${last.x} 236 L ${first.x} 236 Z`
})

const getStatusType = (status: string) => {
  const map: Record<string, 'success' | 'warning' | 'error' | 'default'> = {
    healthy: 'success',
    warning: 'warning',
    critical: 'error'
  }
  return map[status] || 'default'
}

const getStatusText = (status: string) => {
  const map: Record<string, string> = {
    healthy: '健康',
    warning: '警告',
    critical: '危险'
  }
  return map[status] || status
}

const getProgressStatus = (status: string) => getStatusType(status)

const getMediaStatusText = (status: string) => {
  const map: Record<string, string> = {
    connected: '媒体库已连接',
    unconfigured: '媒体库未配置',
    failed: '媒体库连接失败'
  }
  return map[status] || status
}

const formatSize = (bytes: number) => {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(2)} ${units[i]}`
}

const formatTime = (value: string) => new Date(value).toLocaleString()

const loadDiskData = async () => {
  loading.value = true
  try {
    const [statusRes, historyRes, settingsRes] = await Promise.all([
      diskApi.getStatus(),
      diskApi.getHistory(1, 20),
      diskApi.getSettings()
    ])
    diskStatus.value = (statusRes as any).data || []
    const history = (historyRes as any).data as DiskHistory
    samples.value = history?.samples || []
    cleanupRecords.value = history?.cleanup || history?.list || []
    settings.value = (settingsRes as any).data as DiskSettings
  } catch (error: any) {
    message.error(error.message || '加载磁盘监控数据失败')
  } finally {
    loading.value = false
  }
}

const runCleanup = async () => {
  cleanupLoading.value = true
  try {
    await diskApi.cleanup({ strategy: settings.value?.strategy || 'hybrid', keep_days: settings.value?.retention_days || 30, keep_gb: settings.value?.min_free_gb || 50 })
    message.success('清理完成')
    await loadDiskData()
  } catch (error: any) {
    message.error(error.message || '清理失败')
  } finally {
    cleanupLoading.value = false
  }
}

const handleVisibilityChange = () => {
  if (document.visibilityState === 'visible') {
    loadDiskData()
  }
}

onMounted(() => {
  loadDiskData()
  refreshTimer = setInterval(loadDiskData, 30000)
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.disk-monitor-page {
  max-width: 1400px;
  margin: 0 auto;
}

.page-header,
.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.page-title {
  margin: 0 0 20px;
}

.media-alert {
  margin-bottom: 16px;
}

.disk-card {
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.disk-card.status-critical {
  border: 2px solid var(--n-color-error);
}

.disk-card.status-warning {
  border: 2px solid var(--n-color-warning);
}

.unit,
.section-subtitle {
  font-size: 12px;
  color: var(--n-text-color-3);
}

.trend-section,
.cleanup-section {
  margin-top: 20px;
  padding-top: 4px;
}

.section-header h3 {
  margin: 0 0 12px;
  font-size: 18px;
}

.trend-chart {
  width: 100%;
  height: 300px;
  border: 1px solid var(--n-border-color);
  border-radius: 8px;
  background: var(--n-card-color);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

.trend-chart svg {
  width: 100%;
  height: 100%;
}

.trend-stats,
.cleanup-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--n-text-color-2);
}

.trend-stats {
  margin-top: 10px;
  font-size: 13px;
}

.cleanup-list {
  border: 1px solid var(--n-border-color);
  border-radius: 8px;
  overflow: hidden;
}

.cleanup-row {
  min-height: 54px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--n-border-color);
}

.cleanup-row:last-child {
  border-bottom: 0;
}

.cleanup-row strong {
  display: block;
  color: var(--n-text-color-1);
}

.cleanup-row span {
  font-size: 12px;
}

@media (max-width: 768px) {
  .page-header,
  .section-header,
  .trend-stats,
  .cleanup-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .trend-chart {
    height: 240px;
  }
}
</style>
