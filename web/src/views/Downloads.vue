<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px">
      <h2>下载任务</h2>
      <n-space>
        <n-select
          v-model:value="statusFilter"
          :options="statusOptions"
          style="width: 150px"
          @update:value="loadDownloads"
        />
      </n-space>
    </n-space>

    <n-data-table
      :columns="columns"
      :data="downloads"
      :loading="loading"
      :pagination="pagination"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { NButton, NDataTable, NSelect, NSpace, NTag, useMessage, useDialog } from 'naive-ui'
import { downloadApi, type Download } from '@/api'

const message = useMessage()
const dialog = useDialog()
const loading = ref(false)
const downloads = ref<Download[]>([])
const statusFilter = ref('')

const statusOptions = [
  { label: '全部', value: '' },
  { label: '等待中', value: 'pending' },
  { label: '下载中', value: 'downloading' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' }
]

const pagination = ref({
  page: 1,
  pageSize: 20,
  itemCount: 0,
  onChange: (page: number) => {
    pagination.value.page = page
    loadDownloads()
  }
})

const getStatusTag = (status: string) => {
  const statusMap: Record<string, { type: 'success' | 'warning' | 'error' | 'info', text: string }> = {
    pending: { type: 'info', text: '等待中' },
    downloading: { type: 'warning', text: '下载中' },
    completed: { type: 'success', text: '已完成' },
    failed: { type: 'error', text: '失败' }
  }
  const config = statusMap[status] || { type: 'info', text: status }
  return h(NTag, { type: config.type }, { default: () => config.text })
}

const columns = [
  { title: 'ID', key: 'id', width: 60 },
  { title: '标题', key: 'title', ellipsis: { tooltip: true } },
  { title: '字幕组', key: 'fansub', width: 120 },
  { title: '集数', key: 'episode', width: 80 },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row: Download) => getStatusTag(row.status)
  },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render: (row: Download) => {
      const buttons = []
      if (row.status === 'failed') {
        buttons.push(
          h(NButton, { size: 'small', onClick: () => handleRetry(row.id) }, { default: () => '重试' })
        )
      }
      buttons.push(
        h(NButton, { size: 'small', onClick: () => handleDelete(row.id) }, { default: () => '删除' })
      )
      return h(NSpace, null, { default: () => buttons })
    }
  }
]

const loadDownloads = async () => {
  loading.value = true
  try {
    const res: any = await downloadApi.list(pagination.value.page, pagination.value.pageSize, statusFilter.value)
    downloads.value = res.data?.list || []
    pagination.value.itemCount = res.data?.total || 0
  } catch (error) {
    message.error('加载下载列表失败')
    downloads.value = []
  } finally {
    loading.value = false
  }
}

const handleRetry = async (id: number) => {
  try {
    await downloadApi.retry(id)
    message.success('重试成功')
    loadDownloads()
  } catch (error) {
    message.error('重试失败')
  }
}

const handleDelete = async (id: number) => {
  dialog.warning({
    title: '确认删除',
    content: '确定要删除这个下载任务吗？此操作不可恢复。',
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await downloadApi.delete(id)
        message.success('删除成功')
        loadDownloads()
      } catch (error) {
        message.error('删除失败')
      }
    }
  })
}

onMounted(() => {
  loadDownloads()
})
</script>
