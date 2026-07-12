<template>
  <section class="feed-editor" aria-labelledby="feed-editor-title">
    <div class="feed-editor-header">
      <div class="feed-editor-heading">
        <div class="feed-editor-title-row">
          <h3 id="feed-editor-title">RSS feeds</h3>
          <n-tag size="tiny" type="info">平等源</n-tag>
        </div>
        <p>所有 feed 平等，先到先得</p>
      </div>
      <n-button v-if="!readonly" size="small" type="primary" @click="openNewFeed">
        <template #icon><n-icon><PlusOutlined /></n-icon></template>
        添加 feed
      </n-button>
    </div>

    <n-data-table
      v-if="modelValue.length"
      class="feed-table"
      size="small"
      :columns="columns"
      :data="modelValue"
      :row-key="feedRowKey"
      :scroll-x="980"
      :bordered="false"
    />
    <n-empty v-else size="small" description="暂无 RSS feed" class="feed-empty" />

    <n-modal
      v-model:show="showEditor"
      preset="card"
      class="feed-config-modal"
      :title="editingIndex >= 0 ? '编辑 feed' : '添加 feed'"
      :bordered="false"
      :mask-closable="!previewLoading"
    >
      <n-form label-placement="top" :show-feedback="false">
        <div class="feed-form-grid">
          <n-form-item label="名称" required>
            <n-input v-model:value="editorDraft.name" placeholder="例如：字幕组 A" :disabled="readonly" />
          </n-form-item>
          <n-form-item label="字幕组">
            <n-input v-model:value="editorDraft.fansub" placeholder="可选" :disabled="readonly" />
          </n-form-item>
          <n-form-item label="RSS URL" required class="feed-url-field">
            <n-input
              v-model:value="editorDraft.rss_url"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 3 }"
              placeholder="https://example.com/feed.xml"
              :disabled="readonly"
            />
          </n-form-item>
          <n-form-item label="集数偏移" required>
            <n-input-number
              v-model:value="editorDraft.episode_offset"
              :min="0"
              :precision="0"
              button-placement="both"
              class="feed-offset-input"
              :disabled="readonly"
            />
          </n-form-item>
          <n-form-item label="启用">
            <n-switch v-model:value="editorDraft.enabled" :disabled="readonly" />
          </n-form-item>
        </div>
      </n-form>

      <n-alert v-if="draftError" type="error" :show-icon="false" class="feed-draft-alert">
        {{ draftError }}
      </n-alert>

      <div v-if="previewResult || previewLoading" class="mapping-preview">
        <div class="mapping-preview-header">
          <span>集数映射预览</span>
          <n-tag v-if="previewResult" size="tiny" :type="previewResult.valid_items ? 'success' : 'warning'">
            有效 {{ previewResult.valid_items }} / {{ previewResult.parsed_items }}
          </n-tag>
        </div>
        <n-spin :show="previewLoading">
          <n-alert
            v-if="previewResult?.warning === 'empty_feed'"
            type="warning"
            :show-icon="false"
          >
            RSS 当前没有条目，保存后将等待首次发布
          </n-alert>
          <n-data-table
            v-else-if="previewResult"
            size="small"
            :columns="previewColumns"
            :data="previewResult.items"
            :row-key="previewRowKey"
            :scroll-x="720"
            :max-height="260"
            :bordered="false"
          />
        </n-spin>
      </div>

      <template #footer>
        <div class="feed-modal-actions">
          <n-button @click="showEditor = false">取消</n-button>
          <div class="feed-modal-actions-primary">
            <n-button :loading="previewLoading" @click="previewDraft">
              <template #icon><n-icon><EyeOutlined /></n-icon></template>
              预览映射
            </n-button>
            <n-button v-if="!readonly" type="primary" :disabled="!canApplyDraft" @click="applyDraft">
              保存到列表
            </n-button>
          </div>
        </div>
      </template>
    </n-modal>
  </section>
</template>

<script setup lang="tsx">
import { computed, h, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NDataTable,
  NEllipsis,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NSpin,
  NTag,
  NTooltip,
  NSwitch,
  type DataTableColumns
} from 'naive-ui'
import {
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined
} from '@vicons/antd'

import {
  subscriptionFeedApi,
  type SubscriptionFeed,
  type SubscriptionFeedInput,
  type SubscriptionFeedPreview,
  type SubscriptionFeedPreviewItem
} from '@/api/subscription-feed'
import {
  hasDuplicateFeedURLs,
  normalizeFeedURLForComparison
} from '@/utils/subscription-feeds'

const props = defineProps<{
  subscriptionId?: number
  modelValue: SubscriptionFeedInput[]
  readonly?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: SubscriptionFeedInput[]]
  'validation-change': [valid: boolean]
}>()

type DisplayFeed = SubscriptionFeedInput & Partial<SubscriptionFeed>

const showEditor = ref(false)
const editingIndex = ref(-1)
const previewLoading = ref(false)
const previewResult = ref<SubscriptionFeedPreview | null>(null)
const draftError = ref('')
const validatedMappings = ref(new Set<string>())
const initializedFeedIDs = new Set<number>()
const editorDraft = ref<SubscriptionFeedInput>(emptyFeed())

function emptyFeed(): SubscriptionFeedInput {
  return {
    name: '',
    fansub: '',
    rss_url: '',
    episode_offset: 0,
    enabled: true
  }
}

function mappingSignature(feed: SubscriptionFeedInput): string {
  try {
    return `${normalizeFeedURLForComparison(feed.rss_url)}|${feed.episode_offset}`
  } catch {
    return `${feed.rss_url.trim()}|${feed.episode_offset}`
  }
}

function isValidFeedURL(raw: string): boolean {
  try {
    const parsed = new URL(raw.trim())
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function feedValidationError(feed: SubscriptionFeedInput): string {
  if (!feed.name.trim()) return '请输入 feed 名称'
  if (!isValidFeedURL(feed.rss_url)) return '请输入有效的 HTTP 或 HTTPS RSS URL'
  if (!Number.isInteger(feed.episode_offset) || feed.episode_offset < 0) {
    return '集数偏移必须是非负整数'
  }
  return ''
}

function mappingIsValidated(feed: SubscriptionFeedInput): boolean {
  return validatedMappings.value.has(mappingSignature(feed))
}

function validateAllFeeds(feeds: SubscriptionFeedInput[]): boolean {
  return feeds.every((feed) => !feedValidationError(feed) && mappingIsValidated(feed)) &&
    !hasDuplicateFeedURLs(feeds)
}

function publishValidation(feeds = props.modelValue) {
  emit('validation-change', validateAllFeeds(feeds))
}

watch(
  () => props.modelValue,
  (feeds) => {
    const next = new Set(validatedMappings.value)
    for (const feed of feeds) {
      if (feed.id && !initializedFeedIDs.has(feed.id)) {
        initializedFeedIDs.add(feed.id)
        next.add(mappingSignature(feed))
      }
    }
    validatedMappings.value = next
    publishValidation(feeds)
  },
  { deep: true, immediate: true }
)

const previewValid = computed(() => mappingIsValidated(editorDraft.value))
const canApplyDraft = computed(() => {
  return !feedValidationError(editorDraft.value) && previewValid.value
})

function openNewFeed() {
  editingIndex.value = -1
  editorDraft.value = emptyFeed()
  previewResult.value = null
  draftError.value = ''
  showEditor.value = true
}

function openEditFeed(feed: SubscriptionFeedInput, index: number) {
  editingIndex.value = index
  editorDraft.value = { ...feed }
  previewResult.value = null
  draftError.value = ''
  showEditor.value = true
}

function cleanInput(feed: SubscriptionFeedInput): SubscriptionFeedInput {
  return {
    ...(feed.id ? { id: feed.id } : {}),
    name: feed.name.trim(),
    fansub: feed.fansub?.trim() || '',
    rss_url: feed.rss_url.trim(),
    episode_offset: feed.episode_offset,
    enabled: feed.enabled
  }
}

async function previewDraft() {
  const error = feedValidationError(editorDraft.value)
  if (error) {
    draftError.value = error
    return
  }

  previewLoading.value = true
  draftError.value = ''
  try {
    const input = cleanInput(editorDraft.value)
    const response: any = await subscriptionFeedApi.preview(
      props.subscriptionId,
      input,
      input.id
    )
    const preview = response?.data as SubscriptionFeedPreview | undefined
    if (!preview || !Array.isArray(preview.items)) {
      throw new Error('预览接口返回异常')
    }
    previewResult.value = preview
    if (preview.warning === 'empty_feed' || preview.valid_items > 0) {
      const next = new Set(validatedMappings.value)
      next.add(mappingSignature(input))
      validatedMappings.value = next
      publishValidation()
    }
  } catch (error: any) {
    previewResult.value = null
    draftError.value = error.response?.data?.message || error.message || 'feed 预览失败'
  } finally {
    previewLoading.value = false
  }
}

function applyDraft() {
  if (props.readonly || !canApplyDraft.value) return
  const next = props.modelValue.map((feed) => ({ ...feed }))
  const input = cleanInput(editorDraft.value)
  if (editingIndex.value >= 0) {
    next[editingIndex.value] = { ...next[editingIndex.value], ...input }
  } else {
    next.push(input)
  }
  if (hasDuplicateFeedURLs(next)) {
    draftError.value = '同一订阅内不能添加重复的 feed URL'
    return
  }
  emit('update:modelValue', next)
  publishValidation(next)
  showEditor.value = false
}

function removeFeed(index: number) {
  const next = props.modelValue.filter((_, feedIndex) => feedIndex !== index)
  emit('update:modelValue', next)
  publishValidation(next)
}

function updateEnabled(index: number, enabled: boolean) {
  const next = props.modelValue.map((feed, feedIndex) =>
    feedIndex === index ? { ...feed, enabled } : { ...feed }
  )
  emit('update:modelValue', next)
  publishValidation(next)
}

function previewFeed(feed: SubscriptionFeedInput, index: number) {
  openEditFeed(feed, index)
  void previewDraft()
}

function feedRowKey(feed: SubscriptionFeedInput, index = 0) {
  return feed.id || `${mappingSignature(feed)}:${index}`
}

function previewRowKey(item: SubscriptionFeedPreviewItem, index = 0) {
  return `${item.title}:${item.original_episode}:${index}`
}

function formatFeedTime(value?: string) {
  if (!value) return '尚未成功'
  return new Date(value).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

const columns = computed<DataTableColumns<SubscriptionFeedInput>>(() => [
  {
    title: '启用',
    key: 'enabled',
    width: 64,
    fixed: 'left',
    render: (row, index) => h(NSwitch, {
      value: row.enabled,
      size: 'small',
      disabled: props.readonly,
      'onUpdate:value': (value: boolean) => updateEnabled(index, value)
    })
  },
  {
    title: '名称 / 字幕组',
    key: 'name',
    width: 150,
    render: (row) => h('div', { class: 'feed-name-cell' }, [
      h('strong', row.name || '未命名'),
      row.fansub ? h('span', row.fansub) : null
    ])
  },
  {
    title: 'RSS URL',
    key: 'rss_url',
    minWidth: 250,
    render: (row) => h(NEllipsis, { tooltip: { width: 420 } }, { default: () => row.rss_url })
  },
  {
    title: '偏移',
    key: 'episode_offset',
    width: 72,
    align: 'right',
    render: (row) => h('span', { class: 'feed-offset-value' }, row.episode_offset)
  },
  {
    title: '同步状态',
    key: 'baseline_pending',
    width: 104,
    render: (row) => {
      const feed = row as DisplayFeed
      if (!row.id) return h(NTag, { size: 'tiny', type: 'info' }, { default: () => '待创建' })
      return h(
        NTag,
        { size: 'tiny', type: feed.baseline_pending ? 'warning' : 'success' },
        { default: () => feed.baseline_pending ? '待基线' : '增量同步' }
      )
    }
  },
  {
    title: '最近结果',
    key: 'last_success_at',
    width: 150,
    render: (row) => {
      const feed = row as DisplayFeed
      return h('div', { class: 'feed-result-cell' }, [
        h('span', formatFeedTime(feed.last_success_at)),
        feed.last_error ? h(NEllipsis, { class: 'feed-row-error', tooltip: { width: 360 } }, { default: () => feed.last_error }) : null
      ])
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 132,
    fixed: 'right',
    render: (row, index) => h('div', { class: 'feed-row-actions' }, [
      h(NTooltip, null, {
        trigger: () => h(NButton, {
          text: true,
          class: 'feed-icon-button',
          'aria-label': '预览 feed',
          onClick: () => previewFeed(row, index)
        }, { icon: () => h(NIcon, null, { default: () => h(EyeOutlined) }) }),
        default: () => '预览映射'
      }),
      h(NTooltip, null, {
        trigger: () => h(NButton, {
          text: true,
          class: 'feed-icon-button',
          'aria-label': '编辑 feed',
          disabled: props.readonly,
          onClick: () => openEditFeed(row, index)
        }, { icon: () => h(NIcon, null, { default: () => h(EditOutlined) }) }),
        default: () => '编辑'
      }),
      h(NTooltip, null, {
        trigger: () => h(NButton, {
          text: true,
          type: 'error',
          class: 'feed-icon-button',
          'aria-label': '删除 feed',
          disabled: props.readonly,
          onClick: () => removeFeed(index)
        }, { icon: () => h(NIcon, null, { default: () => h(DeleteOutlined) }) }),
        default: () => '删除'
      })
    ])
  }
])

const previewColumns: DataTableColumns<SubscriptionFeedPreviewItem> = [
  {
    title: '标题',
    key: 'title',
    minWidth: 260,
    render: (row) => h(NEllipsis, { tooltip: { width: 480 } }, { default: () => row.title })
  },
  { title: '原始集数', key: 'original_episode', width: 88, align: 'right' },
  { title: '偏移', key: 'episode_offset', width: 72, align: 'right' },
  { title: '相对集数', key: 'relative_episode', width: 88, align: 'right' },
  {
    title: '状态',
    key: 'valid',
    width: 150,
    render: (row) => h('div', { class: 'mapping-status' }, [
      h(NTag, { size: 'tiny', type: row.valid ? 'success' : 'warning' }, {
        default: () => row.valid ? '有效' : '无效'
      }),
      !row.valid && row.invalid_reason ? h('span', row.invalid_reason) : null
    ])
  }
]
</script>

<style scoped>
.feed-editor {
  min-width: 0;
  padding: 14px;
  border-radius: 8px;
  background: #f8fafb;
  box-shadow:
    0 0 0 1px rgba(31, 41, 55, 0.07),
    0 1px 2px rgba(31, 41, 55, 0.04);
}

.feed-editor-header,
.feed-editor-title-row,
.feed-modal-actions,
.feed-modal-actions-primary,
.mapping-preview-header {
  display: flex;
  align-items: center;
}

.feed-editor-header,
.feed-modal-actions,
.mapping-preview-header {
  justify-content: space-between;
  gap: 12px;
}

.feed-editor-title-row {
  gap: 8px;
}

.feed-editor-heading h3 {
  margin: 0;
  font-size: 14px;
  line-height: 1.4;
  text-wrap: balance;
}

.feed-editor-heading p {
  margin: 3px 0 0;
  color: #7b8491;
  font-size: 12px;
  line-height: 1.4;
  text-wrap: pretty;
}

.feed-table {
  margin-top: 12px;
}

.feed-empty {
  padding: 24px 0 14px;
}

:deep(.feed-name-cell),
:deep(.feed-result-cell),
:deep(.mapping-status) {
  display: flex;
  min-width: 0;
  flex-direction: column;
}

:deep(.feed-name-cell strong) {
  overflow: hidden;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:deep(.feed-name-cell span),
:deep(.feed-result-cell span) {
  overflow: hidden;
  color: #7b8491;
  font-size: 11px;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:deep(.feed-offset-value),
:deep(.feed-result-cell),
:deep(.mapping-status) {
  font-variant-numeric: tabular-nums;
}

:deep(.feed-row-error) {
  color: #d03050 !important;
}

:deep(.feed-row-actions) {
  display: flex;
  align-items: center;
  gap: 2px;
}

:deep(.feed-icon-button) {
  min-width: 40px;
  min-height: 40px;
  transition-property: scale, color;
  transition-duration: 150ms;
  transition-timing-function: ease-out;
}

:deep(.feed-icon-button:active:not(:disabled)) {
  scale: 0.96;
}

.feed-form-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 14px;
}

.feed-url-field {
  grid-column: 1 / -1;
}

.feed-offset-input {
  width: 100%;
}

.feed-draft-alert,
.mapping-preview {
  margin-top: 14px;
}

.mapping-preview {
  padding-top: 14px;
  border-top: 1px solid #e5e9ef;
}

.mapping-preview-header {
  margin-bottom: 10px;
  font-size: 13px;
  font-weight: 600;
}

:deep(.mapping-status) {
  gap: 4px;
}

:deep(.mapping-status span) {
  color: #8a5a00;
  font-size: 11px;
  line-height: 1.35;
  white-space: normal;
}

.feed-modal-actions-primary {
  gap: 8px;
}

:global(.feed-config-modal) {
  width: min(760px, calc(100vw - 24px));
  max-height: calc(100vh - 32px);
}

@media (max-width: 640px) {
  .feed-editor {
    padding: 12px;
  }

  .feed-editor-header,
  .feed-modal-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .feed-form-grid {
    grid-template-columns: 1fr;
  }

  .feed-url-field {
    grid-column: auto;
  }

  .feed-modal-actions-primary {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
}
</style>
