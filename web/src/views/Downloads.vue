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
        <n-button
          type="error"
          :disabled="selectedRowKeys.length === 0"
          @click="handleBatchDelete"
        >
          批量删除 {{ selectedRowKeys.length > 0 ? `(${selectedRowKeys.length})` : '' }}
        </n-button>
        <n-dropdown :options="clearOptions" @select="handleClear">
          <n-button type="warning">
            一键清空
          </n-button>
        </n-dropdown>
      </n-space>
    </n-space>

    <n-data-table
      :columns="columns"
      :data="downloads"
      :loading="loading"
      :pagination="pagination"
      :remote="true"
      :row-key="(row: Download) => row.id"
      v-model:checked-row-keys="selectedRowKeys"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { NButton, NDataTable, NSelect, NSpace, NTag, NDropdown, NTooltip, NIcon, useMessage, useDialog } from 'naive-ui'
import { RefreshOutline, TrashOutline } from '@vicons/ionicons5'
import { downloadApi, type Download } from '@/api'

const message = useMessage()
const dialog = useDialog()
const loading = ref(false)
const downloads = ref<Download[]>([])
const statusFilter = ref('')
const selectedRowKeys = ref<number[]>([])

const statusOptions = [
  { label: '全部', value: '' },
  { label: '等待中', value: 'pending' },
  { label: '下载中', value: 'downloading' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' }
]

const clearOptions = [
  { label: '清空全部', key: 'all' },
  { label: '清空已完成', key: 'completed' },
  { label: '清空失败', key: 'failed' },
  { label: '清空等待中', key: 'pending' }
]

const pagination = ref({
  page: 1,
  pageSize: 20,
  pageCount: 1,
  itemCount: 0,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  onChange: (page: number) => {
    pagination.value.page = page
    loadDownloads()
  },
  onUpdatePageSize: (pageSize: number) => {
    pagination.value.pageSize = pageSize
    pagination.value.page = 1
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
  { type: 'selection' as const },
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
    width: 120,
    render: (row: Download) => {
      const buttons: Array<ReturnType<typeof h>> = []
      if (row.status === 'failed') {
        buttons.push(
          h(
            NTooltip,
            { trigger: 'hover' },
            {
              trigger: () => h(
                NButton,
                { size: 'small', circle: true, secondary: true, type: 'warning', onClick: () => handleRetry(row.id) },
                { icon: () => h(NIcon, null, { default: () => h(RefreshOutline) }) }
              ),
              default: () => '重试下载'
            }
          )
        )
      }
      buttons.push(
        h(
          NTooltip,
          { trigger: 'hover' },
          {
            trigger: () => h(
              NButton,
              { size: 'small', circle: true, secondary: true, type: 'error', onClick: () => handleDelete(row.id) },
              { icon: () => h(NIcon, null, { default: () => h(TrashOutline) }) }
            ),
            default: () => '删除任务'
          }
        )
      )
      return h(NSpace, null, { default: () => buttons })
    }
  }
]

const loadDownloads = async () => {
  loading.value = true
  selectedRowKeys.value = []
  try {
    const res: any = await downloadApi.list(pagination.value.page, pagination.value.pageSize, statusFilter.value)
    downloads.value = res.data?.list || []
    pagination.value.itemCount = res.data?.total || 0
    pagination.value.pageCount = Math.ceil((res.data?.total || 0) / pagination.value.pageSize)
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

const handleBatchDelete = async () => {
  if (selectedRowKeys.value.length === 0) {
    message.warning('请先选择要删除的任务')
    return
  }

  dialog.warning({
    title: '确认批量删除',
    content: `确定要删除选中的 ${selectedRowKeys.value.length} 个下载任务吗？此操作不可恢复。`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await downloadApi.batchDelete(selectedRowKeys.value)
        message.success(`成功删除 ${selectedRowKeys.value.length} 个任务`)
        selectedRowKeys.value = []
        loadDownloads()
      } catch (error) {
        message.error('批量删除失败')
      }
    }
  })
}

const handleClear = async (key: string) => {
  const statusText: Record<string, string> = {
    all: '全部',
    completed: '已完成',
    failed: '失败',
    pending: '等待中'
  }

  dialog.warning({
    title: '确认清空',
    content: `确定要清空${statusText[key]}的下载任务吗？此操作不可恢复。`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const status = key === 'all' ? undefined : key
        await downloadApi.clear(status)
        message.success(`已清空${statusText[key]}任务`)
        loadDownloads()
      } catch (error) {
        message.error('清空失败')
      }
    }
  })
}

onMounted(() => {
  loadDownloads()
})
</script>
