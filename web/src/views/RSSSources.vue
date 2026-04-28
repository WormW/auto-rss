<template>
  <div class="rss-sources-page">
    <div class="page-header">
      <h2>RSS 源管理</h2>
      <n-button type="primary" size="small" @click="showCreateModal = true">
        <template #icon><n-icon><AddOutline /></n-icon></template>
        <span class="btn-text">添加 RSS 源</span>
      </n-button>
    </div>

    <!-- 移动端卡片列表 -->
    <div class="mobile-list" v-if="isMobile">
      <n-spin :show="loading">
        <n-empty v-if="sources.length === 0 && !loading" description="暂无 RSS 源" />
        <div v-else class="source-cards">
          <n-card v-for="source in sources" :key="source.id" size="small" class="source-card">
            <div class="card-header">
              <span class="card-title">{{ source.name }}</span>
              <n-tag size="tiny" :type="source.enabled ? 'success' : 'default'">
                {{ source.enabled ? '启用' : '禁用' }}
              </n-tag>
            </div>
            <div class="card-url">{{ source.base_url }}</div>
            <div v-if="source.description" class="card-desc">{{ source.description }}</div>
            <div class="card-actions">
              <n-button text size="small" type="info" @click="handleViewAnimes(source)">
                <template #icon><n-icon><ListOutline /></n-icon></template>
                番剧列表
              </n-button>
              <n-button text size="small" type="error" @click="handleDelete(source.id)">
                <template #icon><n-icon><TrashOutline /></n-icon></template>
                删除
              </n-button>
            </div>
          </n-card>
        </div>
      </n-spin>
    </div>

    <!-- 桌面端表格 -->
    <n-data-table
      v-else
      :columns="columns"
      :data="sources"
      :loading="loading"
      :pagination="pagination"
    />

    <!-- 添加/编辑 RSS 源对话框 -->
    <n-modal v-model:show="showCreateModal" preset="dialog" title="添加 RSS 源">
      <n-form ref="formRef" :model="formData" :rules="formRules" label-placement="top">
        <n-form-item label="名称" path="name">
          <n-input v-model:value="formData.name" placeholder="如：Mikanani" />
        </n-form-item>
        <n-form-item label="RSS 地址" path="base_url">
          <n-input v-model:value="formData.base_url" placeholder="https://mikanime.tv/RSS/..." />
        </n-form-item>
        <n-form-item label="描述" path="description">
          <n-input
            v-model:value="formData.description"
            type="textarea"
            placeholder="可选"
          />
        </n-form-item>
        <n-form-item label="启用" path="enabled">
          <n-switch v-model:value="formData.enabled" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-space>
          <n-button @click="showCreateModal = false">取消</n-button>
          <n-button type="primary" @click="handleCreate">确定</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 查看番剧列表对话框 -->
    <n-modal
      v-model:show="showAnimesModal"
      preset="card"
      :title="`${currentSource?.name} - 番剧列表`"
      class="animes-modal"
      :segmented="{content: true, footer: 'soft'}"
    >
      <template #header-extra>
        <n-space>
          <n-button
            v-if="selectedAnimes.length > 0"
            type="primary"
            @click="handleBatchImport"
            :loading="batchImporting"
          >
            批量导入 ({{ selectedAnimes.length }})
          </n-button>
          <n-button
            @click="handleSelectAll"
            :disabled="animes.length === 0"
          >
            {{ selectedAnimes.length === animes.length ? '取消全选' : '全选' }}
          </n-button>
        </n-space>
      </template>

      <n-spin :show="animesLoading">
        <n-empty v-if="animes.length === 0 && !animesLoading" description="暂无番剧数据" />
        <n-list v-else bordered>
          <n-list-item v-for="anime in animes" :key="anime.title">
            <template #prefix>
              <n-checkbox
                :checked="isAnimeSelected(anime)"
                @update:checked="(checked) => toggleAnimeSelection(anime, checked)"
              />
            </template>
            <n-thing :title="anime.title">
              <template #header-extra>
                <n-tag type="info" size="small">{{ anime.fansub }}</n-tag>
              </template>
              <template #description>
                <n-space size="small">
                  <n-tag size="tiny" type="default">
                    <template #icon>
                      <n-icon :component="CalendarOutline" />
                    </template>
                    {{ anime.update_day || '未知' }}
                  </n-tag>
                  <n-tag size="tiny" type="default">
                    <template #icon>
                      <n-icon :component="FilmOutline" />
                    </template>
                    集数: {{ anime.episodes.length > 0 ? anime.episodes.join(', ') : '暂无' }}
                  </n-tag>
                </n-space>
              </template>
            </n-thing>
            <template #suffix>
              <n-button
                size="small"
                @click="handleSingleImport(anime)"
                :loading="importingAnimes.has(anime.title)"
              >
                单个导入
              </n-button>
            </template>
          </n-list-item>
        </n-list>
      </n-spin>

      <template #footer>
        <n-space justify="space-between">
          <n-text depth="3">
            已选择 {{ selectedAnimes.length }} / {{ animes.length }} 个番剧
          </n-text>
          <n-button @click="showAnimesModal = false">关闭</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 导入结果对话框 -->
    <n-modal
      v-model:show="showResultModal"
      preset="card"
      title="批量导入结果"
      class="result-modal"
      :segmented="{content: true}"
    >
      <n-alert v-if="importResults" :type="getResultAlertType()" style="margin-bottom: 16px">
        <template #header>
          导入完成: 成功 {{ importResults.success }} 个, 跳过 {{ importResults.skipped }} 个, 失败 {{ importResults.failed }} 个
        </template>
      </n-alert>

      <n-list bordered>
        <n-list-item v-for="(result, index) in importResults?.results || []" :key="index">
          <n-thing :title="result.title">
            <template #header-extra>
              <n-tag
                :type="result.success ? (result.skipped ? 'warning' : 'success') : 'error'"
                size="small"
              >
                {{ result.success ? (result.skipped ? '跳过' : '成功') : '失败' }}
              </n-tag>
            </template>
            <template #description>
              {{ result.message }}
            </template>
          </n-thing>
        </n-list-item>
      </n-list>

      <template #footer>
        <n-space justify="end">
          <n-button @click="handleCloseResultModal">关闭</n-button>
          <n-button type="primary" @click="handleGoToSubscriptions">
            查看订阅
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h, onUnmounted } from 'vue'
import {
  NButton,
  NDataTable,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSpace,
  NSwitch,
  NTag,
  NList,
  NListItem,
  NThing,
  NSpin,
  NCheckbox,
  NEmpty,
  NText,
  NAlert,
  NIcon,
  NTooltip,
  NCard,
  useMessage,
  useDialog
} from 'naive-ui'
import { CalendarOutline, FilmOutline, ListOutline, TrashOutline, AddOutline } from '@vicons/ionicons5'
import { rssSourceApi, subscriptionApi, type RSSSource, type RSSAnime } from '@/api'
import { useRouter } from 'vue-router'
import type { FormInst } from 'naive-ui'

const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const formRef = ref<FormInst | null>(null)
const loading = ref(false)
const sources = ref<RSSSource[]>([])
const showCreateModal = ref(false)
const showAnimesModal = ref(false)
const showResultModal = ref(false)
const animesLoading = ref(false)
const batchImporting = ref(false)
const animes = ref<RSSAnime[]>([])
const selectedAnimes = ref<RSSAnime[]>([])
const importingAnimes = ref<Set<string>>(new Set())
const currentSource = ref<RSSSource | null>(null)
const importResults = ref<any>(null)

// Mobile detection
const isMobile = ref(false)
const checkMobile = () => {
  isMobile.value = window.innerWidth < 768
}
onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
  loadSources()
})
onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})

const formData = ref({
  name: '',
  base_url: '',
  description: '',
  enabled: true
})

const formRules = {
  name: [
    { required: true, message: '请输入 RSS 源名称', trigger: 'blur' }
  ],
  base_url: [
    { required: true, message: '请输入 RSS 地址', trigger: 'blur' },
    {
      validator: (_rule: any, value: string) => {
        if (!value) return true
        // 自动补全协议后验证
        const url = value.startsWith('http://') || value.startsWith('https://')
          ? value
          : 'https://' + value
        try {
          new URL(url)
          return true
        } catch {
          return new Error('请输入有效的 URL 地址')
        }
      },
      trigger: 'blur'
    }
  ]
}

const pagination = ref({
  page: 1,
  pageSize: 20,
  itemCount: 0,
  onChange: (page: number) => {
    pagination.value.page = page
    loadSources()
  }
})

const columns = [
  { title: 'ID', key: 'id', width: 60 },
  { title: '名称', key: 'name', width: 150 },
  { title: 'RSS 地址', key: 'base_url', ellipsis: { tooltip: true } },
  { title: '描述', key: 'description', width: 200, ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'enabled',
    width: 100,
    render: (row: RSSSource) => {
      return h(
        NTag,
        { type: row.enabled ? 'success' : 'default', size: 'small' },
        { default: () => (row.enabled ? '启用' : '禁用') }
      )
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 120,
    render: (row: RSSSource) => {
      return h(NSpace, null, {
        default: () => [
          h(
            NTooltip,
            { trigger: 'hover' },
            {
              trigger: () => h(
                NButton,
                { size: 'small', circle: true, secondary: true, type: 'info', onClick: () => handleViewAnimes(row) },
                { icon: () => h(NIcon, null, { default: () => h(ListOutline) }) }
              ),
              default: () => '查看番剧列表'
            }
          ),
          h(
            NTooltip,
            { trigger: 'hover' },
            {
              trigger: () => h(
                NButton,
                { size: 'small', circle: true, secondary: true, type: 'error', onClick: () => handleDelete(row.id) },
                { icon: () => h(NIcon, null, { default: () => h(TrashOutline) }) }
              ),
              default: () => '删除此源'
            }
          )
        ]
      })
    }
  }
]

const loadSources = async () => {
  loading.value = true
  try {
    const res: any = await rssSourceApi.list(pagination.value.page, pagination.value.pageSize)
    sources.value = res.data?.list || []
    pagination.value.itemCount = res.data?.total || 0
  } catch (error) {
    message.error('加载 RSS 源列表失败')
    sources.value = []
  } finally {
    loading.value = false
  }
}

const handleCreate = async () => {
  try {
    // 验证表单
    await formRef.value?.validate()

    // 自动补全 URL 协议
    let baseUrl = formData.value.base_url.trim()
    if (!baseUrl.startsWith('http://') && !baseUrl.startsWith('https://')) {
      baseUrl = 'https://' + baseUrl
    }

    // 提交数据
    await rssSourceApi.create({
      ...formData.value,
      base_url: baseUrl
    })

    message.success('添加成功')
    showCreateModal.value = false
    formData.value = {
      name: '',
      base_url: '',
      description: '',
      enabled: true
    }
    loadSources()
  } catch (error: any) {
    // 如果是表单验证错误，不显示消息（表单会自动显示）
    if (error?.message) {
      return
    }
    message.error('添加失败')
  }
}

const handleDelete = async (id: number) => {
  dialog.warning({
    title: '确认删除',
    content: '确定要删除这个 RSS 源吗？此操作不可恢复。',
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await rssSourceApi.delete(id)
        message.success('删除成功')
        loadSources()
      } catch (error) {
        message.error('删除失败')
      }
    }
  })
}

const handleViewAnimes = async (source: RSSSource) => {
  currentSource.value = source
  selectedAnimes.value = []
  showAnimesModal.value = true
  animesLoading.value = true

  try {
    const res: any = await rssSourceApi.fetchAnimes(source.id)
    animes.value = res.data || []
  } catch (error) {
    message.error('获取番剧列表失败')
    animes.value = []
  } finally {
    animesLoading.value = false
  }
}

const isAnimeSelected = (anime: RSSAnime) => {
  return selectedAnimes.value.some(a => a.title === anime.title)
}

const toggleAnimeSelection = (anime: RSSAnime, checked: boolean) => {
  if (checked) {
    if (!isAnimeSelected(anime)) {
      selectedAnimes.value.push(anime)
    }
  } else {
    selectedAnimes.value = selectedAnimes.value.filter(a => a.title !== anime.title)
  }
}

const handleSelectAll = () => {
  if (selectedAnimes.value.length === animes.value.length) {
    selectedAnimes.value = []
  } else {
    selectedAnimes.value = [...animes.value]
  }
}

const handleSingleImport = async (anime: RSSAnime) => {
  importingAnimes.value.add(anime.title)
  try {
    const res: any = await subscriptionApi.batchImportFromRSS([{
      title: anime.title,
      fansub: anime.fansub,
      rss_url: anime.rss_url,
      season: anime.season || 1,
      source_id: anime.source_id,
      source_name: anime.source_name
    }])

    if (res.code === 0) {
      message.success(`${anime.title} 导入任务已提交，可在右上角任务管理中查看进度`)
    } else {
      message.error(res.message || '导入失败')
    }
  } catch (error: any) {
    message.error('导入失败: ' + (error.response?.data?.message || error.message || '未知错误'))
  } finally {
    importingAnimes.value.delete(anime.title)
  }
}

const handleBatchImport = async () => {
  if (selectedAnimes.value.length === 0) {
    message.warning('请先选择要导入的番剧')
    return
  }

  batchImporting.value = true
  try {
    const items = selectedAnimes.value.map(anime => ({
      title: anime.title,
      fansub: anime.fansub,
      rss_url: anime.rss_url,
      season: anime.season || 1,
      source_id: anime.source_id,
      source_name: anime.source_name
    }))

    const res: any = await subscriptionApi.batchImportFromRSS(items)

    if (res.code === 0) {
      message.success(`批量导入任务已提交（共 ${items.length} 个），可在右上角任务管理中查看进度`)
      showAnimesModal.value = false
      selectedAnimes.value = []
    } else {
      message.error('批量导入失败: ' + (res.message || '未知错误'))
    }
  } catch (error: any) {
    message.error('批量导入失败: ' + (error.response?.data?.message || error.message || '未知错误'))
  } finally {
    batchImporting.value = false
  }
}

const getResultAlertType = () => {
  if (!importResults.value) return 'info'
  if (importResults.value.failed === 0) return 'success'
  if (importResults.value.success === 0) return 'error'
  return 'warning'
}

const handleCloseResultModal = () => {
  showResultModal.value = false
  importResults.value = null
}

const handleGoToSubscriptions = () => {
  showResultModal.value = false
  importResults.value = null
  router.push({ name: 'subscriptions' })
}
</script>

<style scoped>
.rss-sources-page {
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

/* 移动端列表 */
.mobile-list {
  display: none;
}

.source-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.source-card {
  border-radius: 8px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.card-title {
  font-size: 15px;
  font-weight: 600;
}

.card-url {
  font-size: 12px;
  color: var(--n-text-color-3);
  word-break: break-all;
  margin-bottom: 4px;
}

.card-desc {
  font-size: 13px;
  color: var(--n-text-color-2);
  margin-bottom: 8px;
}

.card-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--n-border-color);
}

/* Modal 响应式 */
.animes-modal {
  width: 90%;
  max-width: 1400px;
}

.result-modal {
  width: 80%;
  max-width: 1000px;
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

  .btn-text {
    display: none;
  }

  .mobile-list {
    display: block;
  }

  .n-data-table {
    display: none;
  }

  .animes-modal,
  .result-modal {
    width: 95vw !important;
    max-width: none !important;
    margin: 8px;
  }

  .animes-modal :deep(.n-card__content),
  .result-modal :deep(.n-card__content) {
    padding: 12px !important;
  }

  /* 番剧列表移动端优化 */
  .animes-modal :deep(.n-list-item) {
    padding: 12px 8px;
  }

  .animes-modal :deep(.n-thing-header) {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}

/* 桌面端隐藏移动列表 */
@media (min-width: 769px) {
  .mobile-list {
    display: none !important;
  }
}
</style>
