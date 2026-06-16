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
                <n-tag
                  size="tiny"
                  :type="getMediaLibraryStatusConfig(item.media_library_refresh_status).type"
                >
                  {{ getMediaLibraryStatusConfig(item.media_library_refresh_status).text }}
                </n-tag>
                <n-tag size="tiny" :type="getMetadataStatusConfig(item).type">
                  {{ getMetadataStatusConfig(item).text }}
                </n-tag>
              </n-space>
            </div>
            <div v-if="item.media_library_path" class="card-library-path">
              {{ item.media_library_path }}
            </div>
            <div class="card-actions">
              <n-button v-if="item.status === 'failed' || item.status === 'stalled'" text size="small" @click="openDiagnostics(item)">
                <template #icon><n-icon><InformationCircleOutline /></n-icon></template>
                诊断
              </n-button>
              <n-button v-if="item.status === 'failed'" text size="small" type="warning" @click="handleRetry(item.id)">
                <template #icon><n-icon><RefreshOutline /></n-icon></template>
                重试
              </n-button>
              <n-button v-if="item.status === 'completed'" text size="small" @click="handleMediaRefresh(item.id)">
                <template #icon><n-icon><SyncOutline /></n-icon></template>
                刷新媒体库
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

    <n-modal v-model:show="showDiagnostics" preset="card" :title="diagnosticTitle" style="width: 640px; max-width: 95vw;">
      <n-spin :show="diagnosticsLoading">
        <div v-if="selectedDiagnostics" class="diagnostics-panel">
          <div class="diagnostics-summary">
            <n-tag :type="getDiagnosticTagType(selectedDiagnostics.severity)">
              {{ selectedDiagnostics.title }}
            </n-tag>
            <n-tag size="small">{{ selectedDiagnostics.category }}</n-tag>
            <n-tag v-if="selectedDiagnostics.retry_blocked" size="small" type="warning">
              {{ selectedDiagnostics.retry_blocked }}
            </n-tag>
          </div>
          <div class="diagnostics-detail">
            {{ selectedDiagnostics.detail }}
          </div>
          <div class="diagnostics-checks">
            <div
              v-for="[key, passed] in Object.entries(selectedDiagnostics.checks)"
              :key="key"
              class="diagnostics-check"
              :class="{ passed, failed: !passed }"
            >
              <span class="check-dot"></span>
              <span>{{ getCheckLabel(key) }}</span>
            </div>
          </div>
          <div class="diagnostics-actions">
            <n-button
              type="warning"
              size="small"
              :disabled="!selectedDiagnostics.can_retry"
              @click="retryFromDiagnostics"
            >
              <template #icon><n-icon><RefreshOutline /></n-icon></template>
              重试
            </n-button>
            <n-button size="small" type="error" @click="deleteFromDiagnostics">
              <template #icon><n-icon><TrashOutline /></n-icon></template>
              删除
            </n-button>
          </div>
        </div>
      </n-spin>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, h, onUnmounted } from 'vue'
import { NButton, NDataTable, NSelect, NSpace, NTag, NDropdown, NTooltip, NIcon, NCard, NCheckbox, NSpin, NEmpty, NPagination, NModal, useMessage, useDialog } from 'naive-ui'
import { InformationCircleOutline, RefreshOutline, SyncOutline, TrashOutline } from '@vicons/ionicons5'
import { downloadApi, mediaLibraryApi, type Download, type DownloadDiagnostics } from '@/api'

const message = useMessage()
const dialog = useDialog()
const loading = ref(false)
const downloads = ref<Download[]>([])
const statusFilter = ref('')
const selectedRowKeys = ref<number[]>([])
const showDiagnostics = ref(false)
const diagnosticsLoading = ref(false)
const selectedDiagnostics = ref<DownloadDiagnostics | null>(null)
const selectedDiagnosticDownload = ref<Download | null>(null)
const diagnosticTitle = computed(() => {
  if (!selectedDiagnosticDownload.value) return '任务诊断'
  return `任务诊断 #${selectedDiagnosticDownload.value.id}`
})

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
    stalled: { type: 'warning', text: '下载停滞' },
    completed: { type: 'success', text: '已完成' },
    failed: { type: 'error', text: '失败' }
  }
  return statusMap[status] || { type: 'info', text: status }
}

const getDiagnosticTagType = (severity: DownloadDiagnostics['severity']) => {
  const map: Record<DownloadDiagnostics['severity'], 'success' | 'info' | 'warning' | 'error'> = {
    success: 'success',
    info: 'info',
    warning: 'warning',
    error: 'error'
  }
  return map[severity] || 'info'
}

const getMediaLibraryStatusConfig = (status?: string) => {
  const statusMap: Record<string, { type: 'success' | 'warning' | 'error' | 'info', text: string }> = {
    success: { type: 'success', text: '已入库刷新' },
    failed: { type: 'error', text: '入库异常' },
    disabled: { type: 'info', text: '未启用入库' },
    pending: { type: 'warning', text: '待刷新' }
  }
  return statusMap[status || 'pending'] || { type: 'info', text: '未刷新' }
}

const getMediaLibraryTag = (row: Download) => {
  const config = getMediaLibraryStatusConfig(row.media_library_refresh_status)
  const detail = row.media_library_refresh_error || row.media_library_path || '暂无媒体库刷新记录'
  return h(
    NTooltip,
    { trigger: 'hover' },
    {
      trigger: () => h(NTag, { type: config.type, size: 'small' }, { default: () => config.text }),
      default: () => detail
    }
  )
}

const getMetadataStatusConfig = (row: Download) => {
  const sub = row.subscription
  if (!sub?.bangumi_id) {
    return { type: 'warning' as const, text: '未匹配元数据' }
  }
  if (sub.bangumi_cover_local || sub.bangumi_cover) {
    return { type: 'success' as const, text: '封面已匹配' }
  }
  return { type: 'info' as const, text: '元数据已匹配' }
}

const getMetadataTag = (row: Download) => {
  const config = getMetadataStatusConfig(row)
  const sub = row.subscription
  const detail = sub?.bangumi_id
    ? `Bangumi ID: ${sub.bangumi_id}${sub.bangumi_cover_local || sub.bangumi_cover ? '，已有封面' : '，暂无封面'}`
    : '订阅尚未匹配 Bangumi 元数据'
  return h(
    NTooltip,
    { trigger: 'hover' },
    {
      trigger: () => h(NTag, { type: config.type, size: 'small' }, { default: () => config.text }),
      default: () => detail
    }
  )
}

const checkLabels: Record<string, string> = {
  has_torrent_url: '种子链接',
  has_torrent_hash: '任务 Hash',
  has_file_path: '文件路径',
  has_error: '错误记录',
  retry_available: '重试额度',
  qbittorrent_task_found: '下载器任务'
}

const getCheckLabel = (key: string) => checkLabels[key] || key

const statusOptions = [
  { label: '全部', value: '' },
  { label: '等待中', value: 'pending' },
  { label: '下载中', value: 'downloading' },
  { label: '下载停滞', value: 'stalled' },
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
    title: '媒体库',
    key: 'media_library_refresh_status',
    width: 130,
    render: (row: Download) => getMediaLibraryTag(row)
  },
  {
    title: '元数据',
    key: 'metadata',
    width: 130,
    render: (row: Download) => getMetadataTag(row)
  },
  {
    title: '媒体库路径',
    key: 'media_library_path',
    ellipsis: { tooltip: true },
    render: (row: Download) => row.media_library_path || row.renamed_path || row.file_path || '-'
  },
  {
    title: '操作',
    key: 'actions',
    width: 210,
    render: (row: Download) => {
      const buttons: Array<ReturnType<typeof h>> = []
      if (row.status === 'failed' || row.status === 'stalled') {
        buttons.push(
          h(
            NTooltip,
            { trigger: 'hover' },
            {
              trigger: () => h(
                NButton,
                { size: 'small', circle: true, secondary: true, onClick: () => openDiagnostics(row) },
                { icon: () => h(NIcon, null, { default: () => h(InformationCircleOutline) }) }
              ),
              default: () => '查看诊断'
            }
          )
        )
      }
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
      if (row.status === 'completed') {
        buttons.push(
          h(
            NTooltip,
            { trigger: 'hover' },
            {
              trigger: () => h(
                NButton,
                { size: 'small', circle: true, secondary: true, onClick: () => handleMediaRefresh(row.id) },
                { icon: () => h(NIcon, null, { default: () => h(SyncOutline) }) }
              ),
              default: () => '刷新媒体库'
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

const handleMediaRefresh = async (id: number) => {
  try {
    const res: any = await mediaLibraryApi.refreshDownload(id)
    const result = res.data
    if (result?.status === 'success') {
      message.success('媒体库刷新已触发')
    } else if (result?.status === 'disabled') {
      message.info(result.message || '媒体库刷新未启用')
    } else {
      message.warning(result?.message || '媒体库刷新已处理')
    }
    loadDownloads()
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '媒体库刷新失败'
    message.error(errorMsg)
    loadDownloads()
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

const openDiagnostics = async (download: Download) => {
  selectedDiagnosticDownload.value = download
  selectedDiagnostics.value = null
  showDiagnostics.value = true
  diagnosticsLoading.value = true
  try {
    const response: any = await downloadApi.diagnostics(download.id)
    if (!response?.data?.checks) {
      throw new Error('诊断接口返回异常')
    }
    selectedDiagnostics.value = response.data
  } catch (error: any) {
    message.error(error.response?.data?.message || error.message || '加载诊断失败')
    showDiagnostics.value = false
  } finally {
    diagnosticsLoading.value = false
  }
}

const retryFromDiagnostics = async () => {
  if (!selectedDiagnosticDownload.value) return
  await handleRetry(selectedDiagnosticDownload.value.id)
  showDiagnostics.value = false
}

const deleteFromDiagnostics = () => {
  if (!selectedDiagnosticDownload.value) return
  const id = selectedDiagnosticDownload.value.id
  showDiagnostics.value = false
  handleDelete(id)
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
    pending: '等待中',
    stalled: '下载停滞'
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

.card-library-path {
  margin: 0 0 8px 26px;
  color: #6b7280;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.4;
  word-break: break-all;
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

.diagnostics-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.diagnostics-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.diagnostics-detail {
  padding: 12px;
  border: 1px solid #e6e6eb;
  border-radius: 8px;
  background: #fafafa;
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
}

.diagnostics-checks {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.diagnostics-check {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 32px;
  padding: 6px 8px;
  border: 1px solid #e6e6eb;
  border-radius: 8px;
  font-size: 13px;
}

.check-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #d03050;
}

.diagnostics-check.passed .check-dot {
  background: #18a058;
}

.diagnostics-check.failed {
  color: #d03050;
}

.diagnostics-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
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

  .diagnostics-checks {
    grid-template-columns: 1fr;
  }
}

/* 桌面端隐藏移动列表 */
@media (min-width: 769px) {
  .mobile-list {
    display: none !important;
  }
}
</style>
