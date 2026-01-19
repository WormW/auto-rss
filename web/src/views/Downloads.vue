<template>
  <div class="downloads-page">
    <div class="page-header">
      <h2>下载任务</h2>
      <div class="header-actions">
        <n-select
          v-model:value="statusFilter"
          :options="statusOptions"
          class="status-select"
          size="small"
          @update:value="loadDownloads"
        />
        <n-button
          type="error"
          size="small"
          :disabled="selectedRowKeys.length === 0"
          @click="handleBatchDelete"
        >
          <template #icon><n-icon><TrashOutline /></n-icon></template>
          <span class="btn-text">删除 {{ selectedRowKeys.length > 0 ? `(${selectedRowKeys.length})` : '' }}</span>
        </n-button>
        <n-dropdown :options="clearOptions" @select="handleClear">
          <n-button type="warning" size="small">
            <span class="btn-text">清空</span>
          </n-button>
        </n-dropdown>
      </div>
    </div>

    <!-- 移动端卡片列表 -->
    <div class="mobile-list" v-if="isMobile">
      <n-spin :show="loading">
        <n-empty v-if="downloads.length === 0 && !loading" description="暂无下载任务" />
        <div v-else class="download-cards">
          <n-card v-for="item in downloads" :key="item.id" size="small" class="download-card">
            <div class="card-header">
              <n-checkbox
                :checked="selectedRowKeys.includes(item.id)"
                @update:checked="(checked) => toggleSelect(item.id, checked)"
              />
              <span class="card-title">{{ item.title }}</span>
            </div>
            <div class="card-info">
              <n-space :size="4" wrap>
                <n-tag size="tiny" v-if="item.fansub">{{ item.fansub }}</n-tag>
                <n-tag size="tiny" v-if="item.episode">第{{ item.episode }}集</n-tag>
                <n-tag size="tiny" :type="getStatusConfig(item.status).type">
                  {{ getStatusConfig(item.status).text }}
                </n-tag>
              </n-space>
            </div>
            <div class="card-actions">
              <n-button v-if="item.status === 'failed'" text size="small" type="warning" @click="handleRetry(item.id)">
                <template #icon><n-icon><RefreshOutline /></n-icon></template>
                重试
              </n-button>
              <n-button text size="small" type="error" @click="handleDelete(item.id)">
                <template #icon><n-icon><TrashOutline /></n-icon></template>
                删除
              </n-button>
            </div>
          </n-card>
        </div>
        <div class="mobile-pagination" v-if="pagination.itemCount > pagination.pageSize">
          <n-pagination
            v-model:page="pagination.page"
            :page-count="pagination.pageCount"
            :page-size="pagination.pageSize"
            simple
            @update:page="loadDownloads"
          />
        </div>
      </n-spin>
    </div>

    <!-- 桌面端表格 -->
    <n-data-table
      v-else
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
import { ref, onMounted, h, onUnmounted } from 'vue'
import { NButton, NDataTable, NSelect, NSpace, NTag, NDropdown, NTooltip, NIcon, NCard, NCheckbox, NSpin, NEmpty, NPagination, useMessage, useDialog } from 'naive-ui'
import { RefreshOutline, TrashOutline } from '@vicons/ionicons5'
import { downloadApi, type Download } from '@/api'

const message = useMessage()
const dialog = useDialog()
const loading = ref(false)
const downloads = ref<Download[]>([])
const statusFilter = ref('')
const selectedRowKeys = ref<number[]>([])

// Mobile detection
const isMobile = ref(false)
const checkMobile = () => {
  isMobile.value = window.innerWidth < 768
}
onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
  loadDownloads()
})
onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})

// Toggle selection for mobile
const toggleSelect = (id: number, checked: boolean) => {
  if (checked) {
    if (!selectedRowKeys.value.includes(id)) {
      selectedRowKeys.value.push(id)
    }
  } else {
    selectedRowKeys.value = selectedRowKeys.value.filter(k => k !== id)
  }
}

// Status config helper
const getStatusConfig = (status: string) => {
  const statusMap: Record<string, { type: 'success' | 'warning' | 'error' | 'info', text: string }> = {
    pending: { type: 'info', text: '等待中' },
    downloading: { type: 'warning', text: '下载中' },
    completed: { type: 'success', text: '已完成' },
    failed: { type: 'error', text: '失败' }
  }
  return statusMap[status] || { type: 'info', text: status }
}

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
  const config = getStatusConfig(status)
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
</script>

<style scoped>
.downloads-page {
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

.status-select {
  width: 120px;
}

/* 移动端列表 */
.mobile-list {
  display: none;
}

.download-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.download-card {
  border-radius: 8px;
}

.card-header {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 8px;
}

.card-title {
  flex: 1;
  font-size: 14px;
  font-weight: 500;
  line-height: 1.4;
  word-break: break-all;
}

.card-info {
  margin-bottom: 8px;
  padding-left: 26px;
}

.card-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--n-border-color);
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

  .status-select {
    width: 100px;
  }

  .btn-text {
    display: none;
  }

  .mobile-list {
    display: block;
  }

  .n-data-table {
    display: none;
  }
}

/* 桌面端隐藏移动列表 */
@media (min-width: 769px) {
  .mobile-list {
    display: none !important;
  }
}
</style>
