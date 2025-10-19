<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px">
      <h2>系统日志</h2>
      <n-space>
        <n-select
          v-model:value="levelFilter"
          :options="levelOptions"
          style="width: 120px"
          placeholder="日志级别"
          @update:value="loadLogs"
        />
        <n-button @click="handleRefresh">刷新</n-button>
        <n-button @click="handleClear" type="warning">清空日志</n-button>
      </n-space>
    </n-space>

    <n-card>
      <n-data-table
        :columns="columns"
        :data="logs"
        :loading="loading"
        :pagination="pagination"
        :max-height="600"
        virtual-scroll
      />
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { NButton, NDataTable, NSelect, NSpace, NCard, NTag, useMessage } from 'naive-ui'

const message = useMessage()
const loading = ref(false)
const logs = ref<any[]>([])
const levelFilter = ref('')

const levelOptions = [
  { label: '全部', value: '' },
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' }
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
    debug: { type: 'info', text: 'DEBUG' },
    info: { type: 'success', text: 'INFO' },
    warn: { type: 'warning', text: 'WARN' },
    error: { type: 'error', text: 'ERROR' }
  }
  const config = levelMap[level.toLowerCase()] || { type: 'info', text: level }
  return h(NTag, { type: config.type, size: 'small' }, { default: () => config.text })
}

const columns = [
  {
    title: '时间',
    key: 'timestamp',
    width: 180,
    render: (row: any) => {
      const date = new Date(row.created_at)
      return date.toLocaleString('zh-CN', {
        year: 'numeric',
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
    width: 100,
    render: (row: any) => getLevelTag(row.level)
  },
  {
    title: '消息',
    key: 'message',
    ellipsis: {
      tooltip: true
    }
  },
  {
    title: '来源',
    key: 'source',
    width: 150,
    ellipsis: {
      tooltip: true
    }
  }
]

const loadLogs = async () => {
  loading.value = true
  try {
    // TODO: 实现日志查询 API
    // 模拟数据
    const mockLogs = Array.from({ length: 100 }, (_, i) => ({
      id: i + 1,
      level: ['debug', 'info', 'warn', 'error'][Math.floor(Math.random() * 4)],
      message: `这是第 ${i + 1} 条日志消息`,
      source: ['RSS Parser', 'Downloader', 'Scheduler', 'API'][Math.floor(Math.random() * 4)],
      created_at: new Date(Date.now() - Math.random() * 86400000 * 7).toISOString()
    }))

    // 根据级别过滤
    let filteredLogs = mockLogs
    if (levelFilter.value) {
      filteredLogs = mockLogs.filter(log => log.level === levelFilter.value)
    }

    // 分页
    const start = (pagination.value.page - 1) * pagination.value.pageSize
    const end = start + pagination.value.pageSize
    logs.value = filteredLogs.slice(start, end)
    pagination.value.itemCount = filteredLogs.length
  } catch (error) {
    message.error('加载日志失败')
  } finally {
    loading.value = false
  }
}

const handleRefresh = () => {
  loadLogs()
  message.success('日志已刷新')
}

const handleClear = () => {
  // TODO: 实现清空日志功能
  message.info('清空日志功能待实现')
}

onMounted(() => {
  loadLogs()
})
</script>
