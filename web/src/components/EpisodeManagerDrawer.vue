<template>
  <n-drawer
    :show="show"
    :width="'min(680px, 100vw)'"
    placement="right"
    content-class="episode-manager-drawer"
    @update:show="handleDrawerVisibility"
  >
    <n-drawer-content closable body-class="episode-manager-body">
      <template #header>
        <div class="drawer-title-block">
          <span class="drawer-kicker">剧集管理</span>
          <strong>{{ subscription?.name || '未选择订阅' }}</strong>
        </div>
      </template>

      <template v-if="subscription">
        <section class="episode-summary" aria-label="剧集概览">
          <div class="summary-primary">
            <span>连续进度</span>
            <strong class="numeric">{{ continuousProgress }} / {{ progressTotal }}</strong>
          </div>
          <div class="summary-stat">
            <span>已记录</span>
            <strong class="numeric">{{ episodes.length }}</strong>
          </div>
          <div class="summary-stat" :class="{ attention: pendingCandidateCount > 0 }">
            <span>待处理候选</span>
            <strong class="numeric">{{ pendingCandidateCount }}</strong>
          </div>
        </section>

        <n-alert
          v-if="loadError"
          type="error"
          :show-icon="true"
          class="drawer-alert"
          role="alert"
        >
          <div class="alert-row">
            <span>{{ loadError }}</span>
            <n-button class="error-retry-button" size="small" :loading="loading" @click="loadEpisodes">
              <template #icon><n-icon><ReloadOutlined /></n-icon></template>
              重试
            </n-button>
          </div>
        </n-alert>

        <n-alert
          v-if="operationNotice"
          :type="operationNotice.type"
          :show-icon="true"
          class="drawer-alert"
          role="status"
          closable
          @close="operationNotice = null"
        >
          {{ operationNotice.text }}
        </n-alert>

        <section class="episode-controls" aria-label="剧集筛选与添加">
          <n-tabs v-model:value="activeFilter" type="segment" size="small" class="episode-filter-tabs">
            <n-tab-pane name="all" :tab="`全部 ${episodes.length}`" />
            <n-tab-pane name="missing" :tab="`缺失 ${statusCount('missing')}`" />
            <n-tab-pane name="downloading" :tab="`下载中 ${statusCount('downloading')}`" />
            <n-tab-pane name="downloaded" :tab="`已下载 ${statusCount('downloaded')}`" />
            <n-tab-pane name="marked_downloaded" :tab="`已标记 ${statusCount('marked_downloaded')}`" />
            <n-tab-pane name="ignored" :tab="`已忽略 ${statusCount('ignored')}`" />
            <n-tab-pane name="candidate" :tab="`候选 ${candidateEpisodeCount}`" />
          </n-tabs>

          <div class="manual-episode-row">
            <label for="manual-episode-number">添加并选择集数</label>
            <div class="manual-episode-input">
              <n-input-number
                id="manual-episode-number"
                class="manual-episode-number"
                v-model:value="manualEpisodeNumber"
                :min="1"
                :max="10000"
                button-placement="both"
                placeholder="集数"
                @keyup.enter="addManualEpisode"
              />
              <n-button type="primary" secondary :disabled="manualEpisodeNumber == null" @click="addManualEpisode">
                <template #icon><n-icon><PlusOutlined /></n-icon></template>
                添加
              </n-button>
            </div>
          </div>
        </section>

        <section class="selection-toolbar" aria-label="批量操作">
          <n-checkbox
            :checked="allPageSelected"
            :indeterminate="somePageSelected"
            :disabled="currentPageEpisodes.length === 0"
            @update:checked="toggleCurrentPage"
          >
            选择当前页
          </n-checkbox>
          <span class="selection-count numeric">
            已选择 {{ selectedEpisodeNumbers.length }} / {{ MAX_EPISODE_SELECTION }} 集
          </span>
          <div class="batch-actions">
            <n-button
              size="small"
              :disabled="selectedEpisodeNumbers.length === 0 || Boolean(updatingStatus)"
              :loading="updatingStatus === 'marked_downloaded'"
              @click="applyStatus('marked_downloaded')"
            >
              <template #icon><n-icon><CheckCircleOutlined /></n-icon></template>
              标记已下载
            </n-button>
            <n-tooltip trigger="hover" :disabled="!selectionHasBlockedRestore">
              <template #trigger>
                <span class="tooltip-button-wrap">
                  <n-button
                    size="small"
                    :disabled="selectedEpisodeNumbers.length === 0 || Boolean(updatingStatus) || restorePlan.eligible.length === 0"
                    :loading="updatingStatus === 'missing'"
                    @click="applyStatus('missing')"
                  >
                    <template #icon><n-icon><RollbackOutlined /></n-icon></template>
                    恢复缺失
                  </n-button>
                </span>
              </template>
              活动下载任务对应的集数会跳过，请先处理下载任务
            </n-tooltip>
            <n-button
              size="small"
              :disabled="selectedEpisodeNumbers.length === 0 || Boolean(updatingStatus)"
              :loading="updatingStatus === 'ignored'"
              @click="applyStatus('ignored')"
            >
              <template #icon><n-icon><StopOutlined /></n-icon></template>
              忽略
            </n-button>
          </div>
        </section>

        <n-spin :show="loading" class="episode-grid-loading">
          <div v-if="visibleEpisodes.length > 0" class="episode-grid">
            <div
              v-for="item in currentPageEpisodes"
              :key="item.episode"
              class="episode-cell"
              :class="[
                `status-${item.status}`,
                { selected: isSelected(item.episode), local: isLocalEpisode(item.episode) }
              ]"
            >
              <n-checkbox
                class="episode-checkbox"
                :checked="isSelected(item.episode)"
                :aria-label="`${isSelected(item.episode) ? '取消选择' : '选择'}第 ${item.episode} 集`"
                @update:checked="checked => setEpisodeSelected(item.episode, checked)"
              />
              <button
                type="button"
                class="episode-cell-main"
                :aria-pressed="isSelected(item.episode)"
                :aria-label="`${isSelected(item.episode) ? '取消选择' : '选择'}第 ${item.episode} 集，${episodeCellStatus(item)}`"
                @click="toggleEpisode(item.episode)"
              >
                <strong class="episode-number numeric">{{ item.episode }}</strong>
                <span class="episode-status">{{ episodeCellStatus(item) }}</span>
              </button>
              <n-tooltip v-if="item.action_required_candidate_count > 0" trigger="hover">
                <template #trigger>
                  <button
                    type="button"
                    class="candidate-trigger"
                    :aria-label="`查看第 ${item.episode} 集的 ${item.action_required_candidate_count} 个资源候选`"
                    @click="openCandidates(item)"
                  >
                    候选 <span class="numeric">{{ item.action_required_candidate_count }}</span>
                  </button>
                </template>
                比较资源候选
              </n-tooltip>
            </div>
          </div>

          <div v-if="episodePageCount > 1" class="episode-pagination">
            <span class="page-status" role="status" aria-live="polite">
              第 {{ normalizedEpisodePage }} / {{ episodePageCount }} 页
            </span>
            <n-pagination
              :page="normalizedEpisodePage"
              :page-count="episodePageCount"
              :page-slot="5"
              @update:page="episodePage = $event"
            />
          </div>

          <n-empty
            v-if="visibleEpisodes.length === 0 && !loading && !loadError"
            :description="episodes.length === 0 ? '尚无剧集记录' : '当前筛选没有剧集'"
            class="episode-empty"
          >
            <template #extra>
              <span>可在上方输入集数并选择，再执行批量状态操作。</span>
            </template>
          </n-empty>
        </n-spin>
      </template>

      <n-empty v-else description="请先选择订阅" />
    </n-drawer-content>
  </n-drawer>

  <n-modal
    :show="candidateModalOpen"
    preset="card"
    class="candidate-modal"
    :style="candidateModalStyle"
    :mask-closable="!candidateActionLoading"
    :close-on-esc="!candidateActionLoading"
    @update:show="handleCandidateModalVisibility"
  >
    <template #header>
      <div class="candidate-modal-title">
        <span>第 <span class="numeric">{{ selectedEpisode?.episode }}</span> 集资源候选</span>
        <n-tag v-if="selectedCandidate" size="small" :type="candidateStatusType(selectedCandidate.status)">
          {{ candidateStatusLabel(selectedCandidate.status) }}
        </n-tag>
      </div>
    </template>

    <n-spin :show="candidateLoading">
      <n-alert v-if="candidateError" type="error" role="alert" class="candidate-error">
        <div class="alert-row">
          <span>{{ candidateError }}</span>
          <n-button class="error-retry-button" size="small" @click="reloadSelectedCandidates">重试</n-button>
        </div>
      </n-alert>

      <template v-else-if="selectedCandidate && candidateDifference">
        <div v-if="candidateOptions.length > 1" class="candidate-picker">
          <label for="candidate-resource-select">资源候选</label>
          <n-select
            id="candidate-resource-select"
            class="candidate-select"
            v-model:value="selectedCandidateId"
            :options="candidateOptions"
          />
        </div>

        <div v-if="candidateHasMore" class="candidate-load-more">
          <n-button
            :loading="candidateLoadingMore"
            :disabled="candidateLoadingMore"
            @click="loadMoreCandidates"
          >
            加载更多候选
          </n-button>
        </div>

        <div class="resource-comparison" role="table" aria-label="现有资源与候选资源比较">
          <div class="comparison-head" role="row">
            <span role="columnheader">字段</span>
            <strong role="columnheader">现有资源</strong>
            <strong role="columnheader">候选资源</strong>
          </div>
          <div
            v-for="row in comparisonRows"
            :key="row.key"
            class="comparison-row"
            :class="{ different: row.different }"
            role="row"
          >
            <span class="comparison-label" role="cell">{{ row.label }}</span>
            <component
              :is="row.linkCurrent ? 'a' : 'span'"
              role="cell"
              class="comparison-value"
              :href="row.linkCurrent || undefined"
              :target="row.linkCurrent ? '_blank' : undefined"
              :rel="row.linkCurrent ? 'noopener noreferrer' : undefined"
              :title="row.currentTitle"
            >
              {{ row.current }}
            </component>
            <component
              :is="row.linkCandidate ? 'a' : 'span'"
              role="cell"
              class="comparison-value candidate-value"
              :href="row.linkCandidate || undefined"
              :target="row.linkCandidate ? '_blank' : undefined"
              :rel="row.linkCandidate ? 'noopener noreferrer' : undefined"
              :title="row.candidateTitle"
            >
              {{ row.candidate }}
            </component>
          </div>
        </div>

        <div v-if="selectedCandidate.failure_reason" class="candidate-failure" role="alert">
          <strong>上次失败原因</strong>
          <span>{{ selectedCandidate.failure_reason }}</span>
        </div>

        <div v-if="replacementTask" class="replacement-progress" role="status" aria-live="polite">
          <div class="replacement-progress-head">
            <span>{{ replacementTask.message || candidateStageLabel(selectedCandidate.replacement_stage) }}</span>
            <strong class="numeric">{{ replacementTask.progress }}%</strong>
          </div>
          <n-progress
            :percentage="replacementTask.progress"
            :status="replacementTask.status === 'failed' ? 'error' : replacementTask.status === 'completed' ? 'success' : 'default'"
            :show-indicator="false"
          />
          <span v-if="replacementTask.error" class="task-error">{{ replacementTask.error }}</span>
        </div>
        <div
          v-else-if="selectedCandidate.status === 'replacing'"
          class="replacement-progress waiting"
          role="status"
          aria-live="polite"
        >
          <n-spin size="small" />
          <span>{{ candidateStageLabel(selectedCandidate.replacement_stage) }}，正在等待任务状态</span>
        </div>

      </template>

      <n-empty v-else-if="!candidateLoading" description="暂无资源候选" />
    </n-spin>

    <template v-if="selectedCandidate && !candidateLoading && !candidateError" #footer>
      <div class="candidate-actions">
        <n-button :disabled="Boolean(candidateActionLoading)" @click="closeCandidateModal">关闭</n-button>
        <n-button
          v-if="availableCandidateActions.includes('keep')"
          :loading="candidateActionLoading === 'keep'"
          :disabled="Boolean(candidateActionLoading)"
          @click="keepExisting"
        >
          保留现有资源
        </n-button>
        <n-button
          v-if="availableCandidateActions.includes('replace')"
          type="primary"
          :loading="candidateActionLoading === 'replace'"
          :disabled="Boolean(candidateActionLoading)"
          @click="confirmReplacement(false)"
        >
          使用新资源
        </n-button>
        <n-button
          v-if="availableCandidateActions.includes('retry_replace')"
          type="primary"
          :loading="candidateActionLoading === 'replace'"
          :disabled="Boolean(candidateActionLoading)"
          @click="confirmReplacement(true)"
        >
          重新替换
        </n-button>
        <n-button
          v-if="availableCandidateActions.includes('retry_cleanup')"
          type="warning"
          :loading="candidateActionLoading === 'cleanup'"
          :disabled="Boolean(candidateActionLoading) || replacementTaskRunning"
          @click="retryCleanup"
        >
          重试清理
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NCheckbox,
  NDrawer,
  NDrawerContent,
  NEmpty,
  NIcon,
  NInputNumber,
  NModal,
  NPagination,
  NProgress,
  NSelect,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  NTooltip,
  useDialog,
  useMessage
} from 'naive-ui'
import {
  CheckCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  RollbackOutlined,
  StopOutlined
} from '@vicons/antd'

import {
  episodeApi,
  taskApi,
  type CandidateStatus,
  type EditableEpisodeStatus,
  type EpisodeResourceCandidate,
  type EpisodeStatus,
  type ReplacementStage,
  type Subscription,
  type SubscriptionEpisode,
  type Task
} from '@/api'
import {
  appendEpisodeSelection,
  appendUniqueById,
  candidateAvailableActions,
  continuousOwnedEpisode,
  describeCandidateDifference,
  episodeStatusLabel,
  filterEpisodes,
  normalizedValuesDiffer,
  paginateItems,
  planEpisodeStatusUpdate,
  safeExternalURL,
  takeLookaheadPage,
  type EpisodeFilter
} from '@/utils/episode-ledger'

interface OperationNotice {
  type: 'success' | 'warning' | 'error' | 'info'
  text: string
}

interface DisplayEpisode extends SubscriptionEpisode {
  localOnly?: boolean
}

interface ComparisonRow {
  key: string
  label: string
  current: string
  candidate: string
  different: boolean
  linkCurrent?: string
  linkCandidate?: string
  currentTitle?: string
  candidateTitle?: string
}

interface CandidateScope {
  subscriptionId: number
  episode: number
  candidateId: number
  candidatePageOffset: number
}

const EPISODE_PAGE_SIZE = 120
const MAX_EPISODE_SELECTION = 500
const CANDIDATE_PAGE_SIZE = 100
const CANDIDATE_REQUEST_LIMIT = CANDIDATE_PAGE_SIZE + 1
const TASK_MISSING_REFRESH_THRESHOLD = 3
const TASK_MISSING_FAILURE_THRESHOLD = 6

const props = defineProps<{
  show: boolean
  subscription: Subscription | null
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  changed: []
}>()

const dialog = useDialog()
const message = useMessage()

const episodes = ref<SubscriptionEpisode[]>([])
const loading = ref(false)
const loadError = ref('')
const operationNotice = ref<OperationNotice | null>(null)
const activeFilter = ref<EpisodeFilter>('all')
const episodePage = ref(1)
const selectedEpisodeNumbers = ref<number[]>([])
const localEpisodeNumbers = ref<number[]>([])
const manualEpisodeNumber = ref<number | null>(null)
const updatingStatus = ref<EditableEpisodeStatus | ''>('')

const candidateModalOpen = ref(false)
const candidateLoading = ref(false)
const candidateLoadingMore = ref(false)
const candidateError = ref('')
const candidateHasMore = ref(false)
const candidateOffset = ref(0)
const candidates = ref<EpisodeResourceCandidate[]>([])
const selectedEpisode = ref<SubscriptionEpisode | null>(null)
const selectedCandidateId = ref<number | null>(null)
const candidateActionLoading = ref<'keep' | 'replace' | 'cleanup' | ''>('')
const replacementTask = ref<Task | null>(null)
const activeTaskId = ref('')
let episodeRequestGeneration = 0
let episodeMutationGeneration = 0
let candidateRequestGeneration = 0
let taskPollGeneration = 0
let taskMissingPollCount = 0
let taskPollTimer: ReturnType<typeof setTimeout> | null = null
let taskPollScopeKey = ''

const candidateModalStyle = {
  width: 'min(920px, calc(100vw - 24px))',
  maxHeight: 'calc(100vh - 16px)',
  display: 'flex',
  flexDirection: 'column'
}

const continuousProgress = computed(() => continuousOwnedEpisode(episodes.value))
const progressTotal = computed(() => {
  const configured = props.subscription?.total_episodes || 0
  const observed = episodes.value.reduce((max, item) => Math.max(max, item.episode), 0)
  return configured > 0 ? configured : observed || '?'
})
const pendingCandidateCount = computed(() => episodes.value.reduce(
  (total, item) => total + item.action_required_candidate_count,
  0
))
const candidateEpisodeCount = computed(() => episodes.value.filter(
  item => item.action_required_candidate_count > 0
).length)
const episodeNumberSet = computed(() => new Set(episodes.value.map(item => item.episode)))
const selectedEpisodeSet = computed(() => new Set(selectedEpisodeNumbers.value))
const localEpisodeSet = computed(() => new Set(localEpisodeNumbers.value))

const localEpisodes = computed<DisplayEpisode[]>(() => localEpisodeNumbers.value
  .filter(number => !episodeNumberSet.value.has(number))
  .map(number => ({
    id: -number,
    subscription_id: props.subscription?.id || 0,
    episode: number,
    status: 'missing',
    active_download_id: null,
    active_torrent_hash: '',
    active_torrent_url: '',
    active_title: '',
    status_source: 'user',
    downloaded_at: null,
    created_at: '',
    updated_at: '',
    action_required_candidate_count: 0,
    localOnly: true
  })))

const visibleEpisodes = computed<DisplayEpisode[]>(() => {
  const filtered = filterEpisodes(episodes.value, activeFilter.value)
  const includeLocal = activeFilter.value === 'all' || activeFilter.value === 'missing'
  return [...filtered, ...(includeLocal ? localEpisodes.value : [])]
    .sort((left, right) => left.episode - right.episode)
})

const paginatedEpisodes = computed(() => paginateItems(visibleEpisodes.value, episodePage.value, EPISODE_PAGE_SIZE))
const currentPageEpisodes = computed(() => paginatedEpisodes.value.items)
const normalizedEpisodePage = computed(() => paginatedEpisodes.value.page)
const episodePageCount = computed(() => paginatedEpisodes.value.pageCount)
const currentPageEpisodeNumbers = computed(() => currentPageEpisodes.value.map(item => item.episode))
const allPageSelected = computed(() => currentPageEpisodeNumbers.value.length > 0 && currentPageEpisodeNumbers.value.every(isSelected))
const somePageSelected = computed(() => !allPageSelected.value && currentPageEpisodeNumbers.value.some(isSelected))
const restorePlan = computed(() => planEpisodeStatusUpdate(
  episodes.value,
  selectedEpisodeNumbers.value,
  'missing'
))
const selectionHasBlockedRestore = computed(() => restorePlan.value.blocked.length > 0)

const candidateOptions = computed(() => candidates.value.map((candidate, index) => ({
  label: `候选 ${index + 1} · ${candidateStatusLabel(candidate.status)} · ${candidate.title || candidate.resource_key}`,
  value: candidate.id
})))
const selectedCandidate = computed(() => candidates.value.find(
  candidate => candidate.id === selectedCandidateId.value
) || null)
const candidateDifference = computed(() => {
  if (!selectedEpisode.value || !selectedCandidate.value) return null
  return describeCandidateDifference(selectedEpisode.value, selectedCandidate.value)
})
const availableCandidateActions = computed(() => selectedCandidate.value
  ? candidateAvailableActions(selectedCandidate.value.status)
  : [])
const replacementTaskRunning = computed(() => (
  replacementTask.value?.status === 'pending' || replacementTask.value?.status === 'running'
))
const selectedCandidateScopeKey = computed(() => {
  if (!props.subscription || !selectedEpisode.value || selectedCandidateId.value == null) return ''
  return `${props.subscription.id}:${selectedEpisode.value.episode}:${selectedCandidateId.value}`
})

const comparisonRows = computed<ComparisonRow[]>(() => {
  const current = selectedEpisode.value
  const candidate = selectedCandidate.value
  const difference = candidateDifference.value
  if (!current || !candidate || !difference) return []

  const currentFansub = currentMetadataValue(props.subscription?.fansub)
  const currentLanguage = currentMetadataValue(props.subscription?.language)
  const currentTime = current.downloaded_at ? `下载于 ${formatDateTime(current.downloaded_at)}` : '未记录'
  const candidateTime = candidate.pub_time ? `发布于 ${formatDateTime(candidate.pub_time)}` : '未记录'
  const currentHash = summarizeHash(current.active_torrent_hash)
  const candidateHash = summarizeHash(candidate.torrent_hash)
  const currentURL = displayValue(current.active_torrent_url)
  const candidateURL = displayValue(candidate.torrent_url)

  return [
    {
      key: 'title',
      label: '标题',
      current: difference.title.current,
      candidate: difference.title.candidate,
      different: difference.title.different
    },
    {
      key: 'fansub',
      label: '字幕组',
      current: currentFansub,
      candidate: difference.fansub,
      different: normalizedValuesDiffer(props.subscription?.fansub, candidate.fansub)
    },
    {
      key: 'language',
      label: '语言',
      current: currentLanguage,
      candidate: difference.language,
      different: normalizedValuesDiffer(props.subscription?.language, candidate.language)
    },
    {
      key: 'time',
      label: '时间',
      current: currentTime,
      candidate: candidateTime,
      different: currentTime !== candidateTime
    },
    {
      key: 'hash',
      label: 'Hash',
      current: currentHash,
      candidate: candidateHash,
      different: difference.hash.different,
      currentTitle: current.active_torrent_hash || undefined,
      candidateTitle: candidate.torrent_hash || undefined
    },
    {
      key: 'url',
      label: 'URL',
      current: currentURL,
      candidate: candidateURL,
      different: difference.url.different,
      linkCurrent: safeExternalURL(current.active_torrent_url),
      linkCandidate: safeExternalURL(candidate.torrent_url)
    }
  ]
})

watch(
  () => [props.show, props.subscription?.id] as const,
  ([show, subscriptionId], previous) => {
    if (!show || !subscriptionId) {
      invalidateEpisodeRequests()
      invalidateEpisodeMutation()
      closeCandidateModal()
      return
    }
    if (!previous || previous[1] !== subscriptionId) {
      resetDrawerState()
    }
    void loadEpisodes()
  },
  { immediate: true }
)

watch(activeFilter, () => {
  episodePage.value = 1
})

watch(() => visibleEpisodes.value.length, () => {
  episodePage.value = paginateItems(visibleEpisodes.value, episodePage.value, EPISODE_PAGE_SIZE).page
})

watch(selectedCandidateScopeKey, scopeKey => {
  candidateActionLoading.value = ''
  replacementTask.value = null
  activeTaskId.value = ''
  stopTaskPolling()
  if (scopeKey && selectedCandidate.value?.status === 'replacing') {
    startTaskPolling()
  }
})

watch(() => selectedCandidate.value?.status, status => {
  if (status === 'replacing' && selectedCandidateScopeKey.value && !replacementTaskRunning.value) {
    startTaskPolling()
  } else if (status && status !== 'replacing' && !activeTaskId.value && !replacementTaskRunning.value) {
    stopTaskPolling()
  }
})

onBeforeUnmount(() => {
  invalidateEpisodeRequests()
  invalidateEpisodeMutation()
  closeCandidateModal()
})

function resetDrawerState() {
  invalidateEpisodeRequests()
  invalidateEpisodeMutation()
  episodes.value = []
  loadError.value = ''
  operationNotice.value = null
  activeFilter.value = 'all'
  episodePage.value = 1
  selectedEpisodeNumbers.value = []
  localEpisodeNumbers.value = []
  manualEpisodeNumber.value = null
  closeCandidateModal()
}

function handleDrawerVisibility(value: boolean) {
  if (!value) {
    invalidateEpisodeRequests()
    invalidateEpisodeMutation()
    closeCandidateModal()
  }
  emit('update:show', value)
}

async function loadEpisodes() {
  const subscriptionId = props.subscription?.id
  if (!subscriptionId || !props.show) return
  const generation = ++episodeRequestGeneration
  loading.value = true
  loadError.value = ''
  try {
    const response = await episodeApi.list(subscriptionId)
    if (!episodeRequestIsCurrent(generation, subscriptionId)) return
    episodes.value = response.data || []
    localEpisodeNumbers.value = localEpisodeNumbers.value.filter(number => (
      !episodeNumberSet.value.has(number)
    ))
    if (selectedEpisode.value) {
      selectedEpisode.value = episodes.value.find(
        item => item.episode === selectedEpisode.value?.episode
      ) || selectedEpisode.value
    }
  } catch (error: any) {
    if (!episodeRequestIsCurrent(generation, subscriptionId)) return
    loadError.value = apiErrorMessage(error, '加载剧集记录失败')
  } finally {
    if (episodeRequestIsCurrent(generation, subscriptionId)) loading.value = false
  }
}

function invalidateEpisodeRequests() {
  episodeRequestGeneration++
  loading.value = false
}

function episodeRequestIsCurrent(generation: number, subscriptionId: number) {
  return generation === episodeRequestGeneration &&
    props.show && props.subscription?.id === subscriptionId
}

function invalidateEpisodeMutation() {
  episodeMutationGeneration++
  updatingStatus.value = ''
}

function episodeMutationIsCurrent(generation: number, subscriptionId: number) {
  return generation === episodeMutationGeneration &&
    props.show && props.subscription?.id === subscriptionId
}

function statusCount(status: EpisodeStatus) {
  return episodes.value.filter(item => item.status === status).length
}

function episodeCellStatus(item: DisplayEpisode) {
  return item.localOnly ? '待创建' : episodeStatusLabel(item.status)
}

function isLocalEpisode(episodeNumber: number) {
  return localEpisodeSet.value.has(episodeNumber)
}

function isSelected(episodeNumber: number) {
  return selectedEpisodeSet.value.has(episodeNumber)
}

function setEpisodeSelected(episodeNumber: number, checked: boolean): boolean {
  if (checked) {
    const selection = appendEpisodeSelection(
      selectedEpisodeNumbers.value,
      [episodeNumber],
      MAX_EPISODE_SELECTION
    )
    selectedEpisodeNumbers.value = selection.selected
    if (selection.rejected.length > 0) {
      showSelectionLimitWarning(selection.rejected.length)
      return false
    }
  } else {
    selectedEpisodeNumbers.value = selectedEpisodeNumbers.value.filter(number => number !== episodeNumber)
  }
  return true
}

function toggleEpisode(episodeNumber: number) {
  setEpisodeSelected(episodeNumber, !isSelected(episodeNumber))
}

function toggleCurrentPage(checked: boolean) {
  const visible = new Set(currentPageEpisodeNumbers.value)
  if (checked) {
    const selection = appendEpisodeSelection(
      selectedEpisodeNumbers.value,
      currentPageEpisodeNumbers.value,
      MAX_EPISODE_SELECTION
    )
    selectedEpisodeNumbers.value = selection.selected
    if (selection.rejected.length > 0) showSelectionLimitWarning(selection.rejected.length)
  } else {
    selectedEpisodeNumbers.value = selectedEpisodeNumbers.value.filter(number => !visible.has(number))
  }
}

function showSelectionLimitWarning(rejectedCount: number) {
  operationNotice.value = {
    type: 'warning',
    text: `单次最多选择 ${MAX_EPISODE_SELECTION} 集，已保留前 ${MAX_EPISODE_SELECTION} 集，另有 ${rejectedCount} 集未选择。`
  }
}

function addManualEpisode() {
  const value = manualEpisodeNumber.value
  if (!Number.isInteger(value) || value == null || value < 1 || value > 10000) {
    operationNotice.value = { type: 'warning', text: '集数必须是 1 到 10000 之间的整数。' }
    return
  }
  if (!setEpisodeSelected(value, true)) return
  if (!episodeNumberSet.value.has(value) && !localEpisodeSet.value.has(value)) {
    localEpisodeNumbers.value = [...localEpisodeNumbers.value, value].sort((a, b) => a - b)
  }
  activeFilter.value = 'all'
  const visibleIndex = visibleEpisodes.value.findIndex(item => item.episode === value)
  episodePage.value = visibleIndex >= 0 ? Math.floor(visibleIndex / EPISODE_PAGE_SIZE) + 1 : 1
  operationNotice.value = { type: 'info', text: `已选择第 ${value} 集，可继续执行批量状态操作。` }
  manualEpisodeNumber.value = null
}

async function applyStatus(status: EditableEpisodeStatus) {
  const subscriptionId = props.subscription?.id
  if (!subscriptionId || updatingStatus.value) return
  if (selectedEpisodeNumbers.value.length > MAX_EPISODE_SELECTION) {
    showSelectionLimitWarning(selectedEpisodeNumbers.value.length - MAX_EPISODE_SELECTION)
    return
  }
  const plan = planEpisodeStatusUpdate(episodes.value, selectedEpisodeNumbers.value, status)
  if (plan.eligible.length === 0) {
    if (plan.blocked.length > 0) {
      operationNotice.value = {
        type: 'warning',
        text: `第 ${plan.blocked.join('、')} 集仍有活动下载任务，请先处理下载任务。`
      }
    } else {
      operationNotice.value = { type: 'info', text: '所选剧集已经是目标状态。' }
    }
    return
  }

  const generation = ++episodeMutationGeneration
  updatingStatus.value = status
  operationNotice.value = null
  try {
    await episodeApi.updateStatus(subscriptionId, plan.eligible, status)
    if (!episodeMutationIsCurrent(generation, subscriptionId)) return
    selectedEpisodeNumbers.value = plan.blocked
    const skipped = plan.blocked.length + plan.unchanged.length
    operationNotice.value = skipped > 0
      ? {
          type: 'warning',
          text: `已更新 ${plan.eligible.length} 集，另有 ${skipped} 集未变更${plan.blocked.length ? '（活动下载中的集数已跳过）' : ''}。`
        }
      : { type: 'success', text: `已更新 ${plan.eligible.length} 集。` }
    emit('changed')
    await loadEpisodes()
  } catch (error: any) {
    if (episodeMutationIsCurrent(generation, subscriptionId)) {
      operationNotice.value = { type: 'error', text: apiErrorMessage(error, '批量更新失败') }
    }
  } finally {
    if (episodeMutationIsCurrent(generation, subscriptionId)) updatingStatus.value = ''
  }
}

async function openCandidates(episode: SubscriptionEpisode) {
  selectedEpisode.value = episode
  candidateModalOpen.value = true
  await loadCandidates(episode.episode, false)
}

async function loadCandidates(episodeNumber: number, append: boolean) {
  const subscriptionId = props.subscription?.id
  if (!subscriptionId || !props.show || !candidateModalOpen.value || selectedEpisode.value?.episode !== episodeNumber) return
  if (append && (candidateLoadingMore.value || !candidateHasMore.value)) return
  const generation = ++candidateRequestGeneration
  const offset = append ? candidateOffset.value : 0
  if (append) {
    candidateLoadingMore.value = true
  } else {
    candidateLoading.value = true
    candidateError.value = ''
    candidateOffset.value = 0
    candidates.value = []
  }
  try {
    const response = await episodeApi.listCandidates(subscriptionId, episodeNumber, {
      limit: CANDIDATE_REQUEST_LIMIT,
      offset
    })
    if (!candidateRequestIsCurrent(generation, subscriptionId, episodeNumber)) return
    const page = takeLookaheadPage(response.data || [], CANDIDATE_PAGE_SIZE)
    candidates.value = append ? appendUniqueById(candidates.value, page.items) : page.items
    candidateOffset.value = append
      ? candidateOffset.value + page.items.length
      : page.items.length
    candidateHasMore.value = page.hasMore
    const actionable = candidates.value.find(candidate => candidateAvailableActions(candidate.status).length > 0)
    const selectionExists = candidates.value.some(candidate => candidate.id === selectedCandidateId.value)
    if (!selectionExists) {
      selectedCandidateId.value = actionable?.id || candidates.value[0]?.id || null
    }
  } catch (error: any) {
    if (!candidateRequestIsCurrent(generation, subscriptionId, episodeNumber)) return
    if (append) {
      message.error(apiErrorMessage(error, '加载更多资源候选失败'))
    } else {
      candidates.value = []
      selectedCandidateId.value = null
      candidateHasMore.value = false
      candidateError.value = apiErrorMessage(error, '加载资源候选失败')
    }
  } finally {
    if (candidateRequestIsCurrent(generation, subscriptionId, episodeNumber)) {
      candidateLoading.value = false
      candidateLoadingMore.value = false
    }
  }
}

function reloadSelectedCandidates() {
  if (selectedEpisode.value) {
    void loadCandidates(selectedEpisode.value.episode, false)
  }
}

function loadMoreCandidates() {
  if (selectedEpisode.value) {
    void loadCandidates(selectedEpisode.value.episode, true)
  }
}

function candidateRequestIsCurrent(generation: number, subscriptionId: number, episodeNumber: number) {
  return generation === candidateRequestGeneration &&
    props.show && props.subscription?.id === subscriptionId && candidateModalOpen.value &&
    selectedEpisode.value?.episode === episodeNumber
}

function handleCandidateModalVisibility(value: boolean) {
  if (!value) closeCandidateModal()
  else candidateModalOpen.value = true
}

function closeCandidateModal() {
  candidateRequestGeneration++
  candidateModalOpen.value = false
  candidateLoading.value = false
  candidateLoadingMore.value = false
  candidateError.value = ''
  candidateHasMore.value = false
  candidateOffset.value = 0
  candidates.value = []
  selectedCandidateId.value = null
  selectedEpisode.value = null
  candidateActionLoading.value = ''
  replacementTask.value = null
  activeTaskId.value = ''
  stopTaskPolling()
}

async function keepExisting() {
  const scope = candidateScope()
  if (!scope) return
  candidateActionLoading.value = 'keep'
  try {
    await episodeApi.keepExisting(scope.subscriptionId, scope.episode, scope.candidateId)
    if (!candidateScopeIsCurrent(scope)) return
    message.success('已保留现有资源')
    emit('changed')
    await Promise.all([loadEpisodes(), loadCandidates(scope.episode, false)])
  } catch (error: any) {
    if (candidateScopeIsCurrent(scope)) message.error(apiErrorMessage(error, '保留现有资源失败'))
  } finally {
    if (candidateScopeIsCurrent(scope)) candidateActionLoading.value = ''
  }
}

function confirmReplacement(retry: boolean) {
  const episode = selectedEpisode.value
  if (!episode) return
  const tracked = episode.active_download_id != null
  const cleanupText = tracked
    ? '新资源下载和整理成功后，系统才会移除旧任务与旧文件。'
    : '系统会采用新资源，但无法保证清理未受跟踪的旧文件。'
  dialog.warning({
    title: retry ? '确认重新替换' : '确认使用新资源',
    content: `${cleanupText} 替换期间请勿手动移动相关文件。`,
    positiveText: retry ? '重新替换' : '使用新资源',
    negativeText: '取消',
    onPositiveClick: async () => {
      const started = await startReplacement()
      return started ? undefined : false
    }
  })
}

async function startReplacement() {
  const scope = candidateScope()
  if (!scope) return false
  candidateActionLoading.value = 'replace'
  try {
    const response = await episodeApi.replace(scope.subscriptionId, scope.episode, scope.candidateId)
    if (!candidateScopeIsCurrent(scope)) return false
    activeTaskId.value = response.data.task_id
    replacementTask.value = {
      id: response.data.task_id,
      type: 'replacement',
      status: response.data.status,
      subscription_id: scope.subscriptionId,
      name: '替换剧集资源',
      progress: 0,
      message: '任务已启动'
    }
    message.success('替换任务已启动')
    updateSelectedCandidateTaskState('replacing', 'queued')
    startTaskPolling(scope)
    return true
  } catch (error: any) {
    if (candidateScopeIsCurrent(scope)) message.error(apiErrorMessage(error, '启动替换任务失败'))
    return false
  } finally {
    if (candidateScopeIsCurrent(scope)) candidateActionLoading.value = ''
  }
}

async function retryCleanup() {
  const scope = candidateScope()
  if (!scope) return
  candidateActionLoading.value = 'cleanup'
  try {
    const response = await episodeApi.retryCleanup(scope.subscriptionId, scope.episode, scope.candidateId)
    if (!candidateScopeIsCurrent(scope)) return
    activeTaskId.value = response.data.task_id
    replacementTask.value = {
      id: response.data.task_id,
      type: 'replacement',
      status: response.data.status,
      subscription_id: scope.subscriptionId,
      name: '重试替换清理',
      progress: 0,
      message: '清理任务已启动'
    }
    message.success('清理任务已启动')
    updateSelectedCandidateTaskState('accepted_cleanup_failed', 'cleanup_queued')
    startTaskPolling(scope)
  } catch (error: any) {
    if (candidateScopeIsCurrent(scope)) message.error(apiErrorMessage(error, '启动清理重试失败'))
  } finally {
    if (candidateScopeIsCurrent(scope)) candidateActionLoading.value = ''
  }
}

function updateSelectedCandidateTaskState(status: CandidateStatus, stage: ReplacementStage) {
  const candidateId = selectedCandidateId.value
  candidates.value = candidates.value.map(candidate => candidate.id === candidateId
    ? { ...candidate, status, replacement_stage: stage }
    : candidate)
}

async function refreshCandidatePage(scope: CandidateScope): Promise<boolean> {
  const generation = ++candidateRequestGeneration
  candidateLoading.value = false
  candidateLoadingMore.value = false
  try {
    const response = await episodeApi.listCandidates(scope.subscriptionId, scope.episode, {
      limit: CANDIDATE_REQUEST_LIMIT,
      offset: scope.candidatePageOffset
    })
    if (!candidateRequestIsCurrent(generation, scope.subscriptionId, scope.episode) ||
      !candidateScopeIsCurrent(scope)) return false
    const page = takeLookaheadPage(response.data || [], CANDIDATE_PAGE_SIZE)
    candidates.value = appendUniqueById(candidates.value, page.items)
    if (scope.candidatePageOffset + page.items.length >= candidateOffset.value) {
      candidateOffset.value = Math.max(
        candidateOffset.value,
        scope.candidatePageOffset + page.items.length
      )
      candidateHasMore.value = page.hasMore
    }
    return true
  } catch (error: any) {
    if (candidateScopeIsCurrent(scope)) {
      message.error(apiErrorMessage(error, '刷新资源候选状态失败'))
    }
    return false
  }
}

function candidateScope(): CandidateScope | null {
  if (!props.subscription || !selectedEpisode.value || !selectedCandidate.value) return null
  const candidateIndex = candidates.value.findIndex(candidate => candidate.id === selectedCandidate.value?.id)
  return {
    subscriptionId: props.subscription.id,
    episode: selectedEpisode.value.episode,
    candidateId: selectedCandidate.value.id,
    candidatePageOffset: Math.floor(Math.max(0, candidateIndex) / CANDIDATE_PAGE_SIZE) * CANDIDATE_PAGE_SIZE
  }
}

function candidateScopeIsCurrent(scope: CandidateScope) {
  return props.show && candidateModalOpen.value && props.subscription?.id === scope.subscriptionId &&
    selectedEpisode.value?.episode === scope.episode && selectedCandidateId.value === scope.candidateId
}

function startTaskPolling(scope: CandidateScope | null = candidateScope()) {
  if (!scope) return
  if (taskPollScopeKey === selectedCandidateScopeKey.value) return
  stopTaskPolling()
  taskPollScopeKey = selectedCandidateScopeKey.value
  const generation = taskPollGeneration
  void pollReplacementTask(generation, scope)
}

function stopTaskPolling() {
  taskPollGeneration++
  taskPollScopeKey = ''
  taskMissingPollCount = 0
  if (taskPollTimer) {
    clearTimeout(taskPollTimer)
    taskPollTimer = null
  }
}

async function pollReplacementTask(
  generation: number,
  scope: CandidateScope
) {
  if (!taskPollIsCurrent(generation, scope)) return
  try {
    const [currentResponse, historyResponse] = await Promise.all([
      taskApi.getCurrent(),
      taskApi.getHistory()
    ])
    const current = (currentResponse as any).data?.current as Task | null
    const history = ((historyResponse as any).data || []) as Task[]
    if (!taskPollIsCurrent(generation, scope)) return
    const taskId = activeTaskId.value
    const task = taskId
      ? [current, ...history].find(item => item?.id === taskId) || null
      : current?.type === 'replacement' && current.subscription_id === scope.subscriptionId
        ? current
        : null

    if (task && !activeTaskId.value) activeTaskId.value = task.id
    if (task) {
      taskMissingPollCount = 0
      replacementTask.value = task
    }

    if (task && ['completed', 'failed', 'cancelled'].includes(task.status)) {
      await finishTaskPolling(task, scope)
      return
    }

    if (!task && taskId) {
      taskMissingPollCount++
      if (taskMissingPollCount === TASK_MISSING_REFRESH_THRESHOLD ||
        taskMissingPollCount >= TASK_MISSING_FAILURE_THRESHOLD) {
        const handled = await reconcileMissingTask(generation, scope)
        if (handled) return
      }
    } else if (!task && selectedCandidate.value && !candidateTaskIsRunning(selectedCandidate.value)) {
      replacementTask.value = null
      activeTaskId.value = ''
      stopTaskPolling()
      return
    }
  } catch (error) {
    if (!taskPollIsCurrent(generation, scope)) return
    console.error('Failed to poll replacement task:', error)
  }

  if (taskPollIsCurrent(generation, scope)) {
    taskPollTimer = setTimeout(() => {
      taskPollTimer = null
      void pollReplacementTask(generation, scope)
    }, 2000)
  }
}

function taskPollIsCurrent(
  generation: number,
  scope: CandidateScope
) {
  return generation === taskPollGeneration && candidateScopeIsCurrent(scope)
}

async function finishTaskPolling(task: Task, scope: CandidateScope) {
  stopTaskPolling()
  if (task.status === 'completed') message.success('资源候选任务已完成')
  else if (task.status === 'failed') message.error(task.error || '资源候选任务失败')
  else message.warning('资源候选任务已取消')
  emit('changed')
  await Promise.all([loadEpisodes(), refreshCandidatePage(scope)])
}

async function reconcileMissingTask(generation: number, scope: CandidateScope): Promise<boolean> {
  await refreshCandidatePage(scope)
  if (!taskPollIsCurrent(generation, scope)) return true

  const candidate = selectedCandidate.value
  if (candidate && !candidateTaskIsRunning(candidate)) {
    const completed = candidate.status === 'accepted'
    if (replacementTask.value) {
      replacementTask.value = {
        ...replacementTask.value,
        status: completed ? 'completed' : 'failed',
        message: completed ? '候选状态已确认完成' : '候选状态已确认终止',
        error: completed ? undefined : candidate.failure_reason || '资源候选任务未完成'
      }
    }
    stopTaskPolling()
    if (completed) message.success('资源候选任务已完成')
    else message.error(candidate.failure_reason || '资源候选任务未完成')
    emit('changed')
    await loadEpisodes()
    return true
  }

  if (taskMissingPollCount >= TASK_MISSING_FAILURE_THRESHOLD) {
    if (replacementTask.value) {
      replacementTask.value = {
        ...replacementTask.value,
        status: 'failed',
        message: '无法确认任务状态',
        error: '任务状态连续缺失，请稍后重新打开候选详情确认。'
      }
    }
    stopTaskPolling()
    message.error('连续多次无法确认资源候选任务状态，请稍后重试')
    emit('changed')
    await loadEpisodes()
    return true
  }

  return false
}

function candidateTaskIsRunning(candidate: EpisodeResourceCandidate): boolean {
  return candidate.status === 'replacing' || (
    candidate.status === 'accepted_cleanup_failed' &&
    ['cleanup_queued', 'cleanup_active', 'cleaning'].includes(candidate.replacement_stage)
  )
}

function candidateStatusLabel(status: CandidateStatus) {
  const labels: Record<CandidateStatus, string> = {
    pending: '待处理',
    kept_existing: '已保留现有资源',
    replacing: '替换中',
    accepted: '已采用新资源',
    accepted_cleanup_failed: '新资源已采用，清理失败',
    failed: '替换失败'
  }
  return labels[status]
}

function candidateStatusType(status: CandidateStatus): 'default' | 'success' | 'warning' | 'error' | 'info' {
  switch (status) {
    case 'pending': return 'warning'
    case 'replacing': return 'info'
    case 'accepted': return 'success'
    case 'failed':
    case 'accepted_cleanup_failed': return 'error'
    default: return 'default'
  }
}

function candidateStageLabel(stage: ReplacementStage) {
  const labels: Partial<Record<ReplacementStage, string>> = {
    queued: '等待替换',
    downloading: '正在下载新资源',
    download_cleanup: '正在清理新下载任务',
    terminal_cleanup: '正在完成下载清理',
    detaching: '正在解绑旧资源',
    staged: '新资源已暂存',
    old_backed_up: '旧资源已备份',
    promoted: '正在切换资源',
    switched: '资源已切换',
    cleanup_queued: '等待清理旧资源',
    cleanup_active: '正在认领清理任务',
    cleaning: '正在清理旧资源',
    done: '处理完成'
  }
  return labels[stage] || '替换处理中'
}

function displayValue(value?: string | null) {
  return value?.trim() || '未记录'
}

function currentMetadataValue(value?: string | null) {
  return value?.trim() ? `订阅设置：${value.trim()}` : '未记录'
}

function formatDateTime(value?: string | null) {
  if (!value) return '未记录'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

function summarizeHash(value?: string | null) {
  const hash = value?.trim() || ''
  if (!hash) return '未记录'
  if (hash.length <= 24) return hash
  return `${hash.slice(0, 12)}…${hash.slice(-8)}`
}

function apiErrorMessage(error: any, fallback: string) {
  if (error?.response?.status === 409) {
    return error.response.data?.message || '状态已变化，请刷新后重试'
  }
  return error?.response?.data?.message || error?.message || fallback
}
</script>

<style scoped>
.drawer-title-block {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.drawer-title-block strong {
  max-width: 540px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 16px;
  letter-spacing: 0;
}

.drawer-kicker {
  color: #5c6573;
  font-size: 11px;
  font-weight: 600;
}

.episode-summary {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) repeat(2, minmax(96px, 1fr));
  background: #f7f8fa;
  box-shadow: inset 0 0 0 1px rgba(31, 37, 46, 0.07);
  border-radius: 8px;
  overflow: hidden;
}

.summary-primary,
.summary-stat {
  min-height: 72px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
}

.summary-stat {
  border-left: 1px solid rgba(31, 37, 46, 0.08);
}

.summary-primary span,
.summary-stat span {
  color: #666f7c;
  font-size: 12px;
}

.summary-primary strong {
  color: #20252d;
  font-size: 22px;
}

.summary-stat strong {
  font-size: 18px;
}

.summary-stat.attention {
  background: #fff4e5;
  color: #9a4e00;
}

.numeric {
  font-variant-numeric: tabular-nums;
}

.drawer-alert {
  margin-top: 12px;
}

.alert-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.episode-controls {
  margin-top: 16px;
}

.episode-filter-tabs :deep(.n-tabs-nav-scroll-content) {
  min-width: max-content;
}

.episode-filter-tabs :deep(.n-tabs-tab) {
  flex: 0 0 auto;
  min-width: 78px;
  min-height: 40px;
}

.manual-episode-row {
  margin-top: 14px;
  display: grid;
  grid-template-columns: 132px minmax(0, 1fr);
  align-items: center;
  gap: 12px;
}

.manual-episode-row > label,
.candidate-picker label {
  color: #4e5662;
  font-size: 13px;
  font-weight: 600;
}

.manual-episode-input {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

.manual-episode-input > :deep(.n-button) {
  min-height: 40px;
}

.manual-episode-number :deep(.n-input) {
  min-height: 40px;
}

.manual-episode-number :deep(.n-input__prefix > .n-button),
.manual-episode-number :deep(.n-input__suffix > .n-button) {
  min-width: 40px;
  min-height: 40px;
}

.selection-toolbar {
  margin: 16px 0 12px;
  padding: 10px 0;
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 8px 14px;
  border-top: 1px solid rgba(31, 37, 46, 0.09);
  border-bottom: 1px solid rgba(31, 37, 46, 0.09);
}

.selection-toolbar :deep(.n-checkbox) {
  min-height: 40px;
  align-items: center;
}

.error-retry-button {
  min-width: 40px;
  min-height: 40px;
}

.selection-count {
  color: #6b7280;
  font-size: 12px;
}

.batch-actions {
  grid-column: 1 / -1;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.batch-actions :deep(.n-button) {
  min-height: 40px;
}

.tooltip-button-wrap {
  display: inline-flex;
}

.episode-grid-loading {
  min-height: 220px;
}

.episode-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 10px;
  align-items: start;
}

.episode-pagination {
  margin-top: 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.page-status {
  color: #6b7280;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.episode-pagination :deep(.n-pagination-item) {
  min-width: 40px;
  min-height: 40px;
}

.episode-cell {
  position: relative;
  min-width: 0;
  aspect-ratio: 1;
  overflow: hidden;
  border-radius: 7px;
  background: #f7f8fa;
  box-shadow: inset 0 0 0 1px rgba(31, 37, 46, 0.12);
  transition-property: box-shadow, transform, background-color;
  transition-duration: 160ms;
  transition-timing-function: cubic-bezier(0.2, 0, 0, 1);
}

.episode-cell:hover {
  transform: translateY(-1px);
  box-shadow: inset 0 0 0 1px rgba(31, 37, 46, 0.2), 0 4px 12px rgba(31, 37, 46, 0.08);
}

.episode-cell.selected {
  box-shadow: inset 0 0 0 2px #1769aa, 0 3px 10px rgba(23, 105, 170, 0.16);
}

.episode-cell.status-missing {
  background: #fff0f0;
  box-shadow: inset 4px 0 0 #c74343, inset 0 0 0 1px rgba(128, 34, 34, 0.14);
}

.episode-cell.status-downloading {
  background: #fff7df;
  box-shadow: inset 4px 0 0 #b66a00, inset 0 0 0 1px rgba(130, 80, 0, 0.14);
}

.episode-cell.status-downloaded {
  background: #edf8f1;
  box-shadow: inset 4px 0 0 #277d4a, inset 0 0 0 1px rgba(31, 104, 61, 0.14);
}

.episode-cell.status-marked_downloaded {
  background: #edf5fc;
  box-shadow: inset 4px 0 0 #1769aa, inset 0 0 0 1px rgba(23, 105, 170, 0.14);
}

.episode-cell.status-ignored {
  background: #f1f2f4;
  box-shadow: inset 4px 0 0 #69717d, inset 0 0 0 1px rgba(63, 70, 80, 0.14);
}

.episode-cell.local {
  background-image: repeating-linear-gradient(
    -45deg,
    transparent,
    transparent 6px,
    rgba(31, 37, 46, 0.035) 6px,
    rgba(31, 37, 46, 0.035) 12px
  );
}

.episode-checkbox {
  position: absolute;
  top: 4px;
  left: 6px;
  z-index: 2;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.episode-cell-main {
  width: 100%;
  height: 100%;
  min-width: 40px;
  min-height: 40px;
  padding: 26px 8px 34px;
  border: 0;
  background: transparent;
  color: #222832;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
}

.episode-cell-main:active {
  transform: scale(0.96);
}

.episode-number {
  font-size: 20px;
  line-height: 1.1;
}

.episode-status {
  max-width: 100%;
  color: #58616e;
  font-size: 11px;
  line-height: 1.25;
  text-align: center;
  overflow-wrap: anywhere;
}

.candidate-trigger {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 3;
  width: 100%;
  min-height: 40px;
  padding: 6px 8px;
  border: 0;
  border-top: 1px solid rgba(151, 49, 78, 0.14);
  background: #fff1f4;
  color: #9b2548;
  font-size: 11px;
  font-weight: 700;
  cursor: pointer;
}

.candidate-trigger:active {
  transform: scale(0.96);
}

.episode-empty {
  min-height: 220px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.episode-empty :deep(.n-empty__extra) {
  max-width: 320px;
  color: #747c88;
  font-size: 12px;
  text-align: center;
  text-wrap: pretty;
}

.candidate-modal-title {
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

:global(.candidate-modal .n-card-header),
:global(.candidate-modal .n-card__footer) {
  flex: 0 0 auto;
}

:global(.candidate-modal .n-card__content) {
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.candidate-picker {
  margin-bottom: 16px;
  display: grid;
  grid-template-columns: 100px minmax(0, 1fr);
  align-items: center;
  gap: 12px;
}

.candidate-select :deep(.n-base-selection) {
  min-height: 40px;
  --n-height: 40px !important;
}

.candidate-load-more {
  margin-bottom: 12px;
  display: flex;
  justify-content: flex-end;
}

.candidate-load-more :deep(.n-button) {
  min-height: 40px;
}

.resource-comparison {
  overflow: hidden;
  border-radius: 8px;
  box-shadow: inset 0 0 0 1px rgba(31, 37, 46, 0.1);
}

.comparison-head,
.comparison-row {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr) minmax(0, 1fr);
  gap: 0;
}

.comparison-head {
  background: #f0f2f5;
  color: #434b57;
  font-size: 12px;
}

.comparison-head > *,
.comparison-row > * {
  min-width: 0;
  padding: 10px 12px;
}

.comparison-row + .comparison-row {
  border-top: 1px solid rgba(31, 37, 46, 0.08);
}

.comparison-row.different {
  background: #fffaf0;
}

.comparison-label {
  color: #5d6673;
  font-size: 12px;
  font-weight: 600;
}

.comparison-value {
  color: #303641;
  font-size: 12px;
  line-height: 1.55;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

a.comparison-value {
  color: #1769aa;
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 2px;
}

.candidate-value {
  border-left: 1px solid rgba(31, 37, 46, 0.07);
}

.candidate-failure,
.replacement-progress {
  margin-top: 14px;
  padding: 12px;
  border-radius: 7px;
}

.candidate-failure {
  display: flex;
  flex-direction: column;
  gap: 4px;
  background: #fff0f0;
  color: #8d2525;
  overflow-wrap: anywhere;
}

.replacement-progress {
  background: #eef5fb;
}

.replacement-progress.waiting {
  display: flex;
  align-items: center;
  gap: 10px;
  color: #3f5368;
}

.replacement-progress-head {
  margin-bottom: 8px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.task-error {
  display: block;
  margin-top: 8px;
  color: #a72d2d;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.candidate-error {
  margin-bottom: 12px;
}

.candidate-actions {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
}

.candidate-actions :deep(.n-button) {
  min-height: 40px;
}

@media (max-width: 560px) {
  .drawer-title-block strong {
    max-width: calc(100vw - 96px);
  }

  .episode-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .summary-primary {
    grid-column: 1 / -1;
    min-height: 62px;
  }

  .summary-stat {
    min-height: 58px;
    border-top: 1px solid rgba(31, 37, 46, 0.08);
  }

  .summary-stat:nth-child(2) {
    border-left: 0;
  }

  .manual-episode-row,
  .candidate-picker {
    grid-template-columns: 1fr;
    gap: 6px;
  }

  .selection-toolbar {
    grid-template-columns: 1fr;
  }

  .selection-count {
    grid-row: 2;
  }

  .episode-pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .episode-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }

  .comparison-head,
  .comparison-row {
    grid-template-columns: 72px minmax(0, 1fr);
  }

  .comparison-head > :last-child {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  .candidate-value {
    grid-column: 2;
    border-left: 0;
    border-top: 1px dashed rgba(31, 37, 46, 0.12);
  }

  .candidate-value::before {
    content: '候选：';
    color: #8d5b00;
    font-weight: 600;
  }

  .comparison-row > .comparison-value:not(.candidate-value)::before {
    content: '现有：';
    color: #68717e;
    font-weight: 600;
  }
}

@media (prefers-color-scheme: dark) {
  .drawer-kicker,
  .summary-primary span,
  .summary-stat span,
  .manual-episode-row > label,
  .candidate-picker label,
  .selection-count,
  .episode-status,
  .comparison-label {
    color: #aeb5bf;
  }

  .episode-summary,
  .episode-cell,
  .comparison-head {
    background: #23262b;
  }

  .summary-primary strong,
  .episode-cell-main,
  .comparison-value {
    color: #eceef1;
  }

  .summary-stat.attention,
  .comparison-row.different {
    background: #382c1d;
  }
}

@media (prefers-reduced-motion: reduce) {
  .episode-cell,
  .episode-cell-main,
  .candidate-trigger {
    transition-duration: 0.01ms;
  }

  .episode-cell:hover,
  .episode-cell-main:active,
  .candidate-trigger:active {
    transform: none;
  }
}
</style>
