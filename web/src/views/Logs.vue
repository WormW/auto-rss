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
import { NButton, NDataTable, NSelect, NSpace, NCard, NTag, useMessage, useDialog } from 'naive-ui'
import { api } from '@/api'

const message = useMessage()
const dialog = useDialog()
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
  const config = levelMap[level.toLowerCase()] || { type: 'info', text: level.toUpperCase() }
  return h(NTag, { type: config.type, size: 'small' }, { default: () => config.text })
}

const columns = [
  {
    title: '时间',
    key: 'created_at',
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
    title: '上下文',
    key: 'context',
    width: 200,
    ellipsis: {
      tooltip: true
    },
    render: (row: any) => {
      if (!row.context || row.context === '{}') return '-'
      try {
        const ctx = JSON.parse(row.context)
        const keys = Object.keys(ctx)
        if (keys.length === 0) return '-'
        return keys.slice(0, 3).map(k => `${k}=${JSON.stringify(ctx[k])}`).join(', ')
      } catch {
        return row.context
      }
    }
  }
]

const loadLogs = async () => {
  loading.value = true
  try {
    const response: any = await api.get('/logs', {
      params: {
        page: pagination.value.page,
        page_size: pagination.value.pageSize,
        level: levelFilter.value || undefined
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
    console.error('Failed to load logs:', error)
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

onMounted(() => {
  loadLogs()
})
</script>
