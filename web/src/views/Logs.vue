<template>
  <div class="logs-page">
    <div class="page-header">
      <h2>系统日志</h2>
      <div class="header-actions">
        <n-select
          v-model:value="moduleFilter"
          :options="moduleOptions"
          class="filter-select"
          size="small"
          placeholder="模块"
          @update:value="loadLogs"
        />
        <n-select
          v-model:value="levelFilter"
          :options="levelOptions"
          class="filter-select"
          size="small"
          placeholder="级别"
          @update:value="loadLogs"
        />
        <n-button size="small" @click="handleRefresh">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
        </n-button>
        <n-button size="small" @click="handleClear" type="warning">
          <template #icon><n-icon><TrashOutline /></n-icon></template>
        </n-button>
      </div>
    </div>

    <!-- 移动端卡片列表 -->
    <div class="mobile-list" v-if="isMobile">
      <n-spin :show="loading">
        <n-empty v-if="logs.length === 0 && !loading" description="暂无日志" />
        <div v-else class="log-cards">
          <n-card
            v-for="log in logs"
            :key="log.id"
            size="small"
            class="log-card"
            :class="{ 'log-card-error': log.level === 'error', 'log-card-warn': log.level === 'warn' }"
          >
            <div class="log-header">
              <n-space :size="4">
                <n-tag size="tiny" :type="getLevelConfig(log.level).type">
                  {{ getLevelConfig(log.level).text }}
                </n-tag>
                <n-tag size="tiny">{{ getModuleText(log.module) }}</n-tag>
              </n-space>
              <span class="log-time">{{ formatTime(log.created_at) }}</span>
            </div>
            <div class="log-message">{{ log.message }}</div>
            <div v-if="formatContext(log.context)" class="log-context">
              {{ formatContext(log.context) }}
            </div>
          </n-card>
        </div>
        <div class="mobile-pagination" v-if="pagination.itemCount > pagination.pageSize">
          <n-pagination
            v-model:page="pagination.page"
            :page-count="Math.ceil(pagination.itemCount / pagination.pageSize)"
            :page-size="pagination.pageSize"
            simple
            @update:page="loadLogs"
          />
        </div>
      </n-spin>
    </div>

    <!-- 桌面端表格 -->
    <n-card v-else>
      <n-data-table
        :columns="columns"
        :data="logs"
        :loading="loading"
        :pagination="pagination"
        :max-height="600"
        :row-class-name="rowClassName"
        virtual-scroll
      />
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, h } from 'vue'
import { NButton, NDataTable, NSelect, NSpace, NCard, NTag, NSpin, NEmpty, NPagination, NIcon, useMessage, useDialog } from 'naive-ui'
import { RefreshOutline, TrashOutline } from '@vicons/ionicons5'
import { api } from '@/api'

const message = useMessage()
const dialog = useDialog()
const loading = ref(false)
const logs = ref<any[]>([])
const levelFilter = ref('')
const moduleFilter = ref('')

// Mobile detection
const isMobile = ref(false)
const checkMobile = () => {
  isMobile.value = window.innerWidth < 768
}
onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
  loadLogs()
})
onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})

// Helper for mobile
const getLevelConfig = (level: string) => {
  const levelMap: Record<string, { type: 'success' | 'warning' | 'error' | 'info', text: string }> = {
    info: { type: 'success', text: 'INFO' },
    warn: { type: 'warning', text: 'WARN' },
    error: { type: 'error', text: 'ERROR' }
  }
  return levelMap[level.toLowerCase()] || { type: 'info', text: level.toUpperCase() }
}

const getModuleText = (module: string) => {
  const moduleMap: Record<string, string> = {
    rss: 'RSS',
    download: '下载',
    organizer: '整理',
    bangumi: 'Bangumi',
    subscription: '订阅',
    config: '配置',
    system: '系统'
  }
  return moduleMap[module] || module
}

const formatTime = (time: string) => {
  const date = new Date(time)
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

const levelOptions = [
  { label: '全部', value: '' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' }
]

const moduleOptions = [
  { label: '全部', value: '' },
  { label: 'RSS', value: 'rss' },
  { label: '下载', value: 'download' },
  { label: '整理', value: 'organizer' },
  { label: 'Bangumi', value: 'bangumi' },
  { label: '订阅', value: 'subscription' },
  { label: '配置', value: 'config' },
  { label: '系统', value: 'system' }
]

const pagination = ref({
  page: 1,
  pageSize: 50,
  itemCount: 0,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
  onChange: (page: number) => {
    pagination.value.page = page
    loadLogs()
  },
  onUpdatePageSize: (pageSize: number) => {
    pagination.value.pageSize = pageSize
    pagination.value.page = 1
    loadLogs()
  }
})

const getLevelTag = (level: string) => {
  const levelMap: Record<string, { type: 'success' | 'warning' | 'error' | 'info', text: string }> = {
    info: { type: 'success', text: 'INFO' },
    warn: { type: 'warning', text: 'WARN' },
    error: { type: 'error', text: 'ERROR' }
  }
  const config = levelMap[level.toLowerCase()] || { type: 'info', text: level.toUpperCase() }
  return h(NTag, { type: config.type, size: 'small' }, { default: () => config.text })
}

const getModuleTag = (module: string) => {
  const moduleMap: Record<string, string> = {
    rss: 'RSS',
    download: '下载',
    organizer: '整理',
    bangumi: 'Bangumi',
    subscription: '订阅',
    config: '配置',
    system: '系统'
  }
  return moduleMap[module] || module
}

const formatContext = (context: string) => {
  if (!context || context === '{}') return ''
  try {
    const ctx = JSON.parse(context)
    const keys = Object.keys(ctx)
    if (keys.length === 0) return ''
    return keys.map(k => {
      const v = ctx[k]
      if (typeof v === 'string') return `${k}: ${v}`
      return `${k}: ${JSON.stringify(v)}`
    }).join(' | ')
  } catch {
    return context
  }
}

const rowClassName = (row: any) => {
  if (row.level === 'error') return 'log-row-error'
  if (row.level === 'warn') return 'log-row-warn'
  return ''
}

const columns = [
  {
    title: '时间',
    key: 'created_at',
    width: 160,
    render: (row: any) => {
      const date = new Date(row.created_at)
      return date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
      })
    }
  },
  {
    title: '级别',
    key: 'level',
    width: 80,
    render: (row: any) => getLevelTag(row.level)
  },
  {
    title: '模块',
    key: 'module',
    width: 80,
    render: (row: any) => getModuleTag(row.module)
  },
  {
    title: '消息',
    key: 'message',
    ellipsis: { tooltip: true }
  },
  {
    title: '详情',
    key: 'context',
    width: 250,
    ellipsis: { tooltip: true },
    render: (row: any) => formatContext(row.context) || '-'
  }
]

const loadLogs = async () => {
  loading.value = true
  try {
    const response: any = await api.get('/logs', {
      params: {
        page: pagination.value.page,
        page_size: pagination.value.pageSize,
        level: levelFilter.value || undefined,
        module: moduleFilter.value || undefined
      }
    })

    if (response.code === 0) {
      logs.value = response.data.list || []
      pagination.value.itemCount = response.data.total || 0
    } else {
      message.error('加载日志失败: ' + response.message)
    }
  } catch (error: any) {
    message.error('加载日志失败: ' + (error.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

const handleRefresh = () => {
  loadLogs()
  message.success('日志已刷新')
}

const handleClear = () => {
  dialog.warning({
    title: '确认清空日志',
    content: '此操作将删除7天前的所有日志记录,是否继续?',
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const response: any = await api.post('/logs/clear')
        if (response.code === 0) {
          message.success('日志已清空')
          loadLogs()
        } else {
          message.error('清空日志失败: ' + response.message)
        }
      } catch (error: any) {
        message.error('清空日志失败: ' + (error.message || '未知错误'))
      }
    }
  })
}
</script>

<style scoped>
.logs-page {
  max-width: 100%;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 12px;
}

.page-header h2 {
  margin: 0;
  font-size: 20px;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.filter-select {
  width: 90px;
}

/* 移动端列表 */
.mobile-list {
  display: none;
}

.log-cards {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.log-card {
  border-radius: 8px;
}

.log-card-error {
  border-left: 3px solid #d03050;
  background-color: rgba(208, 48, 80, 0.05);
}

.log-card-warn {
  border-left: 3px solid #f0a020;
  background-color: rgba(240, 160, 32, 0.05);
}

.log-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.log-time {
  font-size: 11px;
  color: var(--n-text-color-3);
}

.log-message {
  font-size: 13px;
  line-height: 1.4;
  word-break: break-all;
}

.log-context {
  font-size: 11px;
  color: var(--n-text-color-3);
  margin-top: 4px;
  word-break: break-all;
}

.mobile-pagination {
  margin-top: 16px;
  display: flex;
  justify-content: center;
}

/* 移动端响应式 */
@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: stretch;
  }

  .page-header h2 {
    font-size: 18px;
    margin-bottom: 8px;
  }

  .header-actions {
    justify-content: space-between;
  }

  .filter-select {
    width: 80px;
  }

  .mobile-list {
    display: block;
  }

  .n-card {
    display: none;
  }
}

/* 桌面端隐藏移动列表 */
@media (min-width: 769px) {
  .mobile-list {
    display: none !important;
  }
}

/* 表格行颜色 */
:deep(.log-row-error) {
  background-color: rgba(208, 48, 80, 0.1);
}
:deep(.log-row-warn) {
  background-color: rgba(240, 160, 32, 0.1);
}
</style>
