<template>
  <div class="onboarding-page">
    <section class="wizard-shell">
      <header class="wizard-header">
        <div>
          <n-tag type="success" size="small">首次启动</n-tag>
          <h1>完成 Auto-RSS 最小配置</h1>
          <p>按顺序连接下载器、确认下载目录、导入第一批订阅，完成后直接进入订阅列表。</p>
        </div>
        <n-space>
          <n-button quaternary @click="handleSkip" :loading="skipping">稍后继续</n-button>
          <n-button secondary @click="refreshStatus" :loading="loading">刷新状态</n-button>
        </n-space>
      </header>

      <n-alert v-if="statusError" type="warning" class="status-alert">
        {{ statusError }}
      </n-alert>

      <div class="status-grid">
        <div
          v-for="step in status?.steps || []"
          :key="step.key"
          class="status-tile"
          :class="{ complete: step.complete }"
        >
          <n-icon size="20">
            <CheckmarkCircleOutline v-if="step.complete" />
            <AlertCircleOutline v-else />
          </n-icon>
          <div>
            <strong>{{ step.label }}</strong>
            <span>{{ step.message }}</span>
          </div>
        </div>
      </div>

      <div class="wizard-body">
        <aside class="step-rail">
          <button
            v-for="(step, index) in steps"
            :key="step.key"
            class="rail-item"
            :class="{ active: currentStep === index, done: isStepDone(step.key) }"
            type="button"
            @click="currentStep = index"
          >
            <span class="rail-index">{{ index + 1 }}</span>
            <span>{{ step.title }}</span>
          </button>
        </aside>

        <main class="step-panel">
          <section v-if="activeKey === 'qbittorrent'" class="step-content">
            <div class="step-title">
              <n-icon><ServerOutline /></n-icon>
              <div>
                <h2>qBittorrent 连接</h2>
                <p>保存前先测试连接，避免订阅导入后无法创建下载任务。</p>
              </div>
            </div>
            <n-form :model="qbForm" label-placement="top" class="form-grid">
              <n-form-item label="主机地址">
                <n-input v-model:value="qbForm.host" placeholder="http://localhost:8080" />
              </n-form-item>
              <n-form-item label="用户名">
                <n-input v-model:value="qbForm.username" placeholder="admin" />
              </n-form-item>
              <n-form-item label="密码">
                <n-input
                  v-model:value="qbForm.password"
                  type="password"
                  show-password-on="click"
                  placeholder="请输入 qBittorrent 密码"
                />
              </n-form-item>
            </n-form>
            <n-space>
              <n-button type="primary" @click="testAndSaveQB" :loading="testingQB">
                <template #icon><n-icon><LinkOutline /></n-icon></template>
                测试并保存
              </n-button>
              <n-button @click="saveQBOnly" :loading="savingQB">仅保存</n-button>
            </n-space>
          </section>

          <section v-else-if="activeKey === 'download_path'" class="step-content">
            <div class="step-title">
              <n-icon><FolderOpenOutline /></n-icon>
              <div>
                <h2>下载目录</h2>
                <p>这里会作为订阅下载的根目录，向导会校验目录是否存在并可写。</p>
              </div>
            </div>
            <n-form label-placement="top">
              <n-form-item label="默认下载路径">
                <n-input v-model:value="downloadPath" placeholder="/downloads" />
              </n-form-item>
            </n-form>
            <n-alert v-if="pathValidation" :type="pathValidation.writable ? 'success' : 'warning'" class="inline-alert">
              {{ pathValidationMessage }}
            </n-alert>
            <n-space>
              <n-button type="primary" @click="validateAndSavePath" :loading="validatingPath">
                <template #icon><n-icon><CheckmarkDoneOutline /></n-icon></template>
                校验并保存
              </n-button>
              <n-button @click="savePathOnly" :loading="savingPath">仅保存</n-button>
            </n-space>
          </section>

          <section v-else-if="activeKey === 'rss_source'" class="step-content">
            <div class="step-title">
              <n-icon><RadioOutline /></n-icon>
              <div>
                <h2>添加 RSS 源</h2>
                <p>添加后会立即拉取番剧列表，用于选择第一批订阅。</p>
              </div>
            </div>
            <n-form :model="rssForm" label-placement="top" class="form-grid">
              <n-form-item label="名称">
                <n-input v-model:value="rssForm.name" placeholder="Mikanani" />
              </n-form-item>
              <n-form-item label="RSS 地址">
                <n-input v-model:value="rssForm.base_url" placeholder="https://mikanime.tv/RSS/..." />
              </n-form-item>
              <n-form-item label="描述">
                <n-input v-model:value="rssForm.description" placeholder="可选" />
              </n-form-item>
            </n-form>
            <n-space>
              <n-button type="primary" @click="createSourceAndFetch" :loading="creatingSource">
                <template #icon><n-icon><AddCircleOutline /></n-icon></template>
                添加并拉取
              </n-button>
              <n-button @click="loadExistingSources" :loading="loadingSources">使用已有 RSS 源</n-button>
            </n-space>
            <n-divider />
            <n-select
              v-if="rssSources.length > 0"
              v-model:value="selectedSourceId"
              :options="sourceOptions"
              placeholder="选择已有 RSS 源"
              @update:value="fetchAnimesForSelected"
            />
          </section>

          <section v-else-if="activeKey === 'subscription'" class="step-content">
            <div class="step-title">
              <n-icon><AlbumsOutline /></n-icon>
              <div>
                <h2>导入第一批订阅</h2>
                <p>选择一个番剧导入，导入任务完成后可继续预览重命名。</p>
              </div>
            </div>

            <n-spin :show="loadingAnimes || importingSubscription || pollingImport">
              <n-empty v-if="animes.length === 0" description="还没有可导入的番剧">
                <template #extra>
                  <n-button @click="goStep('rss_source')">返回 RSS 源</n-button>
                </template>
              </n-empty>
              <div v-else class="anime-list">
                <button
                  v-for="anime in animes"
                  :key="anime.title"
                  type="button"
                  class="anime-row"
                  :class="{ selected: selectedAnime?.title === anime.title }"
                  @click="selectedAnime = anime"
                >
                  <span>
                    <strong>{{ anime.title }}</strong>
                    <small>{{ anime.fansub || '未知字幕组' }} · {{ anime.episodes?.length || 0 }} 集</small>
                  </span>
                  <n-tag size="small">{{ anime.source_name }}</n-tag>
                </button>
              </div>
            </n-spin>

            <n-alert v-if="importTaskMessage" type="info" class="inline-alert">
              {{ importTaskMessage }}
            </n-alert>
            <n-space>
              <n-button
                type="primary"
                :disabled="!selectedAnime"
                @click="importSelectedAnime"
                :loading="importingSubscription || pollingImport"
              >
                <template #icon><n-icon><CloudDownloadOutline /></n-icon></template>
                导入选中订阅
              </n-button>
              <n-button @click="refreshTasks" :loading="pollingImport">刷新任务</n-button>
            </n-space>
          </section>

          <section v-else-if="activeKey === 'rename'" class="step-content">
            <div class="step-title">
              <n-icon><CreateOutline /></n-icon>
              <div>
                <h2>预览重命名</h2>
                <p>选择模板并查看示例输出，后续下载完成后会按此规则整理。</p>
              </div>
            </div>
            <n-form label-placement="top">
              <n-form-item label="模板预设">
                <n-select
                  v-model:value="selectedPreset"
                  :options="presetOptions"
                  placeholder="选择预设模板"
                  @update:value="applyPreset"
                />
              </n-form-item>
              <n-form-item label="重命名模板">
                <n-input
                  v-model:value="renameTemplate"
                  type="textarea"
                  :autosize="{ minRows: 2, maxRows: 4 }"
                  placeholder="${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
                />
              </n-form-item>
            </n-form>
            <n-alert v-if="renamePreview" type="success" class="inline-alert">
              <code>{{ renamePreview }}</code>
            </n-alert>
            <n-space>
              <n-button @click="previewRename" :loading="previewingRename">预览</n-button>
              <n-button type="primary" @click="saveRename" :loading="savingRename">保存模板</n-button>
            </n-space>
          </section>

          <section v-else class="step-content">
            <div class="step-title">
              <n-icon><NotificationsOutline /></n-icon>
              <div>
                <h2>通知配置</h2>
                <p>通知是可选项，可配置 Webhook 或 Telegram，也可以直接完成向导。</p>
              </div>
            </div>
            <n-switch v-model:value="configureNotification">
              <template #checked>配置通知</template>
              <template #unchecked>跳过通知</template>
            </n-switch>

            <n-form v-if="configureNotification" label-placement="top" class="notification-form">
              <n-form-item label="渠道">
                <n-select v-model:value="notificationForm.channel" :options="notificationChannelOptions" />
              </n-form-item>
              <template v-if="notificationForm.channel === 'telegram'">
                <n-form-item label="Bot Token">
                  <n-input v-model:value="telegramConfig.bot_token" type="password" show-password-on="click" />
                </n-form-item>
                <n-form-item label="Chat ID">
                  <n-input v-model:value="telegramConfig.chat_id" />
                </n-form-item>
              </template>
              <template v-else>
                <n-form-item label="Webhook URL">
                  <n-input v-model:value="webhookConfig.url" placeholder="http://localhost:8080/webhook" />
                </n-form-item>
                <n-form-item label="请求体模板">
                  <n-input
                    v-model:value="webhookConfig.body_template"
                    type="textarea"
                    :autosize="{ minRows: 4, maxRows: 6 }"
                  />
                </n-form-item>
              </template>
              <n-space>
                <n-button @click="testNotification" :loading="testingNotification">测试</n-button>
                <n-button type="primary" @click="saveNotification" :loading="savingNotification">保存通知</n-button>
              </n-space>
            </n-form>

            <n-result
              :status="canFinishRequiredSetup ? 'success' : 'warning'"
              :title="canFinishRequiredSetup ? '配置可以收尾' : '还有必需配置未完成'"
              :description="finishDescription"
            >
              <template #footer>
                <n-space justify="center">
                  <n-button @click="goStep('rename')">返回检查</n-button>
                  <n-button
                    type="primary"
                    @click="finishOnboarding"
                    :loading="finishing"
                    :disabled="!canFinishRequiredSetup"
                  >
                    完成并进入订阅
                  </n-button>
                </n-space>
              </template>
            </n-result>
          </section>

          <footer class="step-footer">
            <n-button :disabled="currentStep === 0" @click="currentStep--">上一步</n-button>
            <n-button v-if="currentStep < steps.length - 1" type="primary" @click="currentStep++">
              下一步
            </n-button>
          </footer>
        </main>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NDivider,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NResult,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  useMessage
} from 'naive-ui'
import {
  AddCircleOutline,
  AlbumsOutline,
  AlertCircleOutline,
  CheckmarkCircleOutline,
  CheckmarkDoneOutline,
  CloudDownloadOutline,
  CreateOutline,
  FolderOpenOutline,
  LinkOutline,
  NotificationsOutline,
  RadioOutline,
  ServerOutline
} from '@vicons/ionicons5'
import {
  configApi,
  notificationApi,
  onboardingApi,
  subscriptionApi,
  taskApi,
  type OnboardingDownloadPathStatus,
  type OnboardingStatus,
  type RSSAnime,
  type RSSSource,
  rssSourceApi
} from '@/api'

const router = useRouter()
const message = useMessage()

const steps = [
  { key: 'qbittorrent', title: '下载器' },
  { key: 'download_path', title: '目录' },
  { key: 'rss_source', title: 'RSS 源' },
  { key: 'subscription', title: '订阅' },
  { key: 'rename', title: '重命名' },
  { key: 'notification', title: '通知' }
]

const currentStep = ref(0)
const activeKey = computed(() => steps[currentStep.value]?.key || 'qbittorrent')
const status = ref<OnboardingStatus | null>(null)
const loading = ref(false)
const statusError = ref('')
const skipping = ref(false)
const finishing = ref(false)

const qbForm = ref({
  host: 'http://localhost:8080',
  username: 'admin',
  password: ''
})
const testingQB = ref(false)
const savingQB = ref(false)

const downloadPath = ref('/downloads')
const pathValidation = ref<OnboardingDownloadPathStatus | null>(null)
const validatingPath = ref(false)
const savingPath = ref(false)
const pathValidationMessage = computed(() => {
  const validation = pathValidation.value
  if (!validation) return ''
  if (validation.writable) return `${validation.path} 可用`
  return validation.error || '目录校验未通过'
})

const rssForm = ref({
  name: 'Mikanani',
  base_url: '',
  description: ''
})
const rssSources = ref<RSSSource[]>([])
const selectedSourceId = ref<number | null>(null)
const creatingSource = ref(false)
const loadingSources = ref(false)
const loadingAnimes = ref(false)
const animes = ref<RSSAnime[]>([])
const selectedAnime = ref<RSSAnime | null>(null)
const sourceOptions = computed(() =>
  rssSources.value.map(source => ({
    label: `${source.name} · ${source.base_url}`,
    value: source.id
  }))
)

const importingSubscription = ref(false)
const pollingImport = ref(false)
const importTaskId = ref('')
const importTaskMessage = ref('')

const renameTemplate = ref('${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}')
const selectedPreset = ref('media_library')
const presetOptions = ref<Array<{ label: string; value: string }>>([])
const presets = ref<Record<string, string>>({})
const renamePreview = ref('')
const previewingRename = ref(false)
const savingRename = ref(false)

const configureNotification = ref(false)
const notificationForm = ref({
  channel: 'webhook',
  enabled: true
})
const notificationChannelOptions = [
  { label: 'Webhook', value: 'webhook' },
  { label: 'Telegram', value: 'telegram' }
]
const webhookConfig = ref({
  name: 'auto-rss',
  url: '',
  method: 'POST',
  headers: { 'Content-Type': 'application/json' } as Record<string, string>,
  body_template: '{"event":"{{.Event}}","title":"{{.Title}}","message":"{{.Message}}"}',
  retry_enabled: true,
  timeout_sec: 30
})
const telegramConfig = ref({
  bot_token: '',
  chat_id: ''
})
const testingNotification = ref(false)
const savingNotification = ref(false)

const isStepDone = (key: string) => {
  if (key === 'notification') {
    return !configureNotification.value || (status.value?.notification_count || 0) > 0
  }
  if (key === 'rename') {
    return status.value?.rename_template.configured || !!renamePreview.value
  }
  return status.value?.steps.some(step => step.key === key && step.complete) || false
}

const missingRequiredSteps = computed(() => status.value?.steps.filter(step => !step.complete) || [])
const canFinishRequiredSetup = computed(() => missingRequiredSteps.value.length === 0)
const finishDescription = computed(() => {
  if (canFinishRequiredSetup.value) {
    return '完成后不会再次自动弹出向导，可从系统配置继续调整高级选项。'
  }
  return `请先完成：${missingRequiredSteps.value.map(step => step.label).join('、')}`
})

const goStep = (key: string) => {
  const index = steps.findIndex(step => step.key === key)
  if (index >= 0) currentStep.value = index
}

const extractData = <T>(response: any): T => response.data as T

const refreshStatus = async () => {
  loading.value = true
  statusError.value = ''
  try {
    const res = await onboardingApi.status()
    status.value = extractData<OnboardingStatus>(res)
    hydrateFormsFromStatus()
  } catch (error: any) {
    statusError.value = error?.response?.data?.message || '无法读取向导状态'
  } finally {
    loading.value = false
  }
}

const hydrateFormsFromStatus = () => {
  if (!status.value) return
  if (status.value.qbittorrent.host) qbForm.value.host = status.value.qbittorrent.host
  if (status.value.qbittorrent.username) qbForm.value.username = status.value.qbittorrent.username
  if (status.value.download_path.path) downloadPath.value = status.value.download_path.path
  if (status.value.rename_template.template) renameTemplate.value = status.value.rename_template.template
}

const loadConfig = async () => {
  try {
    const res: any = await configApi.getAll()
    const configs = Array.isArray(res.data) ? res.data : []
    for (const item of configs) {
      if (!item.key) continue
      if (item.key === 'qbittorrent_password' || item.key === 'qb_password') qbForm.value.password = item.value || ''
      if (item.key === 'download_path' && item.value) downloadPath.value = item.value
      if (item.key === 'rename_template' && item.value) renameTemplate.value = item.value
    }
  } catch {
    // Status endpoint still provides enough defaults for the wizard.
  }
}

const testAndSaveQB = async () => {
  if (!qbForm.value.host || !qbForm.value.username || !qbForm.value.password) {
    message.warning('请填写完整的 qBittorrent 配置')
    return
  }
  testingQB.value = true
  try {
    await configApi.testQBittorrent(qbForm.value.host, qbForm.value.username, qbForm.value.password)
    await configApi.saveQBittorrent(qbForm.value.host, qbForm.value.username, qbForm.value.password)
    message.success('qBittorrent 连接成功并已保存')
    await refreshStatus()
    currentStep.value = Math.max(currentStep.value, 1)
  } catch (error: any) {
    message.error(error?.response?.data?.message || '连接测试失败')
  } finally {
    testingQB.value = false
  }
}

const saveQBOnly = async () => {
  if (!qbForm.value.host || !qbForm.value.username || !qbForm.value.password) {
    message.warning('请填写完整的 qBittorrent 配置')
    return
  }
  savingQB.value = true
  try {
    await configApi.saveQBittorrent(qbForm.value.host, qbForm.value.username, qbForm.value.password)
    message.success('qBittorrent 配置已保存')
    await refreshStatus()
    currentStep.value = Math.max(currentStep.value, 1)
  } catch (error: any) {
    message.error(error?.response?.data?.message || '保存失败')
  } finally {
    savingQB.value = false
  }
}

const validateAndSavePath = async () => {
  if (!downloadPath.value.trim()) {
    message.warning('请输入下载目录')
    return
  }
  validatingPath.value = true
  try {
    const res = await onboardingApi.validateDownloadPath(downloadPath.value)
    pathValidation.value = extractData<OnboardingDownloadPathStatus>(res)
    await configApi.update('download_path', pathValidation.value.path)
    downloadPath.value = pathValidation.value.path
    message.success('下载目录已校验并保存')
    await refreshStatus()
    currentStep.value = Math.max(currentStep.value, 2)
  } catch (error: any) {
    pathValidation.value = error?.response?.data?.data || null
    message.error(error?.response?.data?.message || '目录不可用')
  } finally {
    validatingPath.value = false
  }
}

const savePathOnly = async () => {
  if (!downloadPath.value.trim()) {
    message.warning('请输入下载目录')
    return
  }
  savingPath.value = true
  try {
    await configApi.update('download_path', downloadPath.value.trim())
    message.success('下载目录已保存')
    await refreshStatus()
    currentStep.value = Math.max(currentStep.value, 2)
  } catch {
    message.error('保存下载目录失败')
  } finally {
    savingPath.value = false
  }
}

const normalizeUrl = (url: string) => {
  const trimmed = url.trim()
  if (!trimmed) return ''
  if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) return trimmed
  return `https://${trimmed}`
}

const createSourceAndFetch = async () => {
  if (!rssForm.value.name.trim() || !rssForm.value.base_url.trim()) {
    message.warning('请填写 RSS 源名称和地址')
    return
  }
  creatingSource.value = true
  try {
    const res: any = await rssSourceApi.create({
      name: rssForm.value.name.trim(),
      base_url: normalizeUrl(rssForm.value.base_url),
      description: rssForm.value.description,
      enabled: true
    })
    const source = res.data as RSSSource
    selectedSourceId.value = source.id
    await loadExistingSources()
    selectedSourceId.value = source.id
    await fetchAnimesForSelected(source.id)
    await refreshStatus()
    message.success('RSS 源已添加')
    currentStep.value = Math.max(currentStep.value, 3)
  } catch (error: any) {
    message.error(error?.response?.data?.message || error?.response?.data?.error || '添加 RSS 源失败')
  } finally {
    creatingSource.value = false
  }
}

const loadExistingSources = async () => {
  loadingSources.value = true
  try {
    const res: any = await rssSourceApi.list(1, 50, true)
    rssSources.value = res.data?.list || []
    if (!selectedSourceId.value && rssSources.value.length > 0) {
      selectedSourceId.value = rssSources.value[0].id
    }
    if (selectedSourceId.value) {
      await fetchAnimesForSelected(selectedSourceId.value)
    }
  } catch {
    message.error('加载 RSS 源失败')
  } finally {
    loadingSources.value = false
  }
}

const fetchAnimesForSelected = async (value?: number | null) => {
  const id = Number(value || selectedSourceId.value)
  if (!id) return
  selectedSourceId.value = id
  loadingAnimes.value = true
  selectedAnime.value = null
  try {
    const res: any = await rssSourceApi.fetchAnimes(id)
    animes.value = res.data || []
    selectedAnime.value = animes.value[0] || null
    if (animes.value.length === 0) {
      message.warning('该 RSS 源暂未解析到番剧')
    }
  } catch (error: any) {
    animes.value = []
    message.error(error?.response?.data?.message || error?.response?.data?.error || '获取番剧列表失败')
  } finally {
    loadingAnimes.value = false
  }
}

const importSelectedAnime = async () => {
  if (!selectedAnime.value) return
  importingSubscription.value = true
  importTaskMessage.value = ''
  try {
    const anime = selectedAnime.value
    const res: any = await subscriptionApi.batchImportFromRSS([{
      title: anime.title,
      fansub: anime.fansub,
      rss_url: anime.rss_url,
      season: anime.season || 1,
      source_id: anime.source_id,
      source_name: anime.source_name
    }])
    importTaskId.value = res.data?.task_id || ''
    importTaskMessage.value = '导入任务已提交'
    await waitForImportTask()
  } catch (error: any) {
    message.error(error?.response?.data?.message || '导入订阅失败')
  } finally {
    importingSubscription.value = false
  }
}

const refreshTasks = async () => {
  const [currentRes, historyRes]: any[] = await Promise.all([
    taskApi.getCurrent(),
    taskApi.getHistory()
  ])
  const current = currentRes.data?.current
  const history = historyRes.data || []
  const task = current?.id === importTaskId.value
    ? current
    : history.find((item: any) => item.id === importTaskId.value)
  if (!task) return null

  importTaskMessage.value = `${task.name}: ${task.message || task.status}`
  if (task.status === 'completed') {
    message.success('订阅导入完成')
    await refreshStatus()
    currentStep.value = Math.max(currentStep.value, 4)
  } else if (task.status === 'failed') {
    message.error(task.error || '订阅导入失败')
  } else if (task.status === 'cancelled') {
    message.warning('导入任务已取消')
  }
  return task
}

const waitForImportTask = async () => {
  if (!importTaskId.value) return
  pollingImport.value = true
  try {
    for (let i = 0; i < 60; i++) {
      await new Promise(resolve => setTimeout(resolve, 1000))
      const task = await refreshTasks()
      if (task && ['completed', 'failed', 'cancelled'].includes(task.status)) {
        return
      }
    }
    message.info('导入仍在后台执行，可稍后在任务管理中查看')
  } finally {
    pollingImport.value = false
  }
}

const loadRenamePresets = async () => {
  try {
    const res: any = await configApi.getRenamePresets()
    presets.value = res.data?.presets || {}
    presetOptions.value = Object.keys(presets.value).map(key => ({
      label: presetLabel(key),
      value: key
    }))
  } catch {
    // Keep the default template usable.
  }
}

const presetLabel = (key: string) => {
  const labels: Record<string, string> = {
    media_library: '媒体库标准格式',
    media_library_fansub: '媒体库 + 字幕组',
    media_library_full: '媒体库完整信息',
    simple: '简洁格式',
    fansub_style: '字幕组风格',
    detailed: '详细信息格式'
  }
  return labels[key] || key
}

const applyPreset = (value: string) => {
  if (presets.value[value]) {
    renameTemplate.value = presets.value[value]
    previewRename()
  }
}

const previewRename = async () => {
  if (!renameTemplate.value.trim()) {
    message.warning('请输入重命名模板')
    return
  }
  previewingRename.value = true
  try {
    const res: any = await configApi.previewRenameTemplate(renameTemplate.value)
    renamePreview.value = res.data?.preview || ''
  } catch (error: any) {
    renamePreview.value = ''
    message.error(error?.response?.data?.message || '预览失败')
  } finally {
    previewingRename.value = false
  }
}

const saveRename = async () => {
  if (!renameTemplate.value.trim()) {
    message.warning('请输入重命名模板')
    return
  }
  savingRename.value = true
  try {
    await configApi.saveRenameTemplate(renameTemplate.value)
    message.success('重命名模板已保存')
    await refreshStatus()
    currentStep.value = Math.max(currentStep.value, 5)
  } catch (error: any) {
    message.error(error?.response?.data?.message || '保存模板失败')
  } finally {
    savingRename.value = false
  }
}

const currentNotificationConfig = () => {
  if (notificationForm.value.channel === 'telegram') {
    return telegramConfig.value
  }
  return webhookConfig.value
}

const testNotification = async () => {
  testingNotification.value = true
  try {
    await notificationApi.testChannel({
      channel: notificationForm.value.channel,
      config: currentNotificationConfig()
    })
    message.success('测试消息已发送')
  } catch (error: any) {
    message.error(error?.response?.data?.message || '测试失败')
  } finally {
    testingNotification.value = false
  }
}

const saveNotification = async () => {
  savingNotification.value = true
  try {
    await notificationApi.updateSetting({
      channel: notificationForm.value.channel,
      enabled: true,
      config: currentNotificationConfig()
    })
    message.success('通知配置已保存')
    await refreshStatus()
  } catch (error: any) {
    message.error(error?.response?.data?.message || '保存通知失败')
  } finally {
    savingNotification.value = false
  }
}

const handleSkip = async () => {
  skipping.value = true
  try {
    await onboardingApi.skip()
    message.info('已跳过向导，可稍后手动配置')
    await router.replace({ name: 'rss-sources' })
  } catch {
    message.error('保存跳过状态失败')
  } finally {
    skipping.value = false
  }
}

const finishOnboarding = async () => {
  if (!canFinishRequiredSetup.value) {
    message.warning(finishDescription.value)
    return
  }

  finishing.value = true
  try {
    await onboardingApi.complete()
    message.success('首次启动向导已完成')
    await router.replace({ name: 'subscriptions' })
  } catch (error: any) {
    const missing = error?.response?.data?.data?.missing
    if (Array.isArray(missing) && missing.length > 0) {
      message.error(`必需配置尚未完成：${missing.join(', ')}`)
      await refreshStatus()
      return
    }
    message.error(error?.response?.data?.message || '保存完成状态失败')
  } finally {
    finishing.value = false
  }
}

onMounted(async () => {
  await Promise.all([
    refreshStatus(),
    loadConfig(),
    loadRenamePresets()
  ])
  await previewRename()
  if ((status.value?.rss_source_count || 0) > 0) {
    await loadExistingSources()
  }
})
</script>

<style scoped>
.onboarding-page {
  min-height: 100vh;
  padding: 32px;
  background:
    linear-gradient(135deg, rgba(20, 184, 166, 0.12), rgba(59, 130, 246, 0.08)),
    var(--n-body-color, #f7f9fb);
}

.wizard-shell {
  max-width: 1180px;
  margin: 0 auto;
  background: var(--n-card-color, #fff);
  border: 1px solid var(--n-border-color, #e5e7eb);
  border-radius: 8px;
  overflow: hidden;
  box-shadow: 0 18px 45px rgba(15, 23, 42, 0.12);
}

.wizard-header {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  padding: 28px;
  border-bottom: 1px solid var(--n-border-color, #e5e7eb);
}

.wizard-header h1 {
  margin: 10px 0 8px;
  font-size: 28px;
  line-height: 1.2;
}

.wizard-header p,
.step-title p {
  margin: 0;
  color: var(--n-text-color-3, #6b7280);
}

.status-alert {
  margin: 16px 28px 0;
}

.status-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 10px;
  padding: 18px 28px;
  border-bottom: 1px solid var(--n-border-color, #e5e7eb);
}

.status-tile {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  min-height: 76px;
  padding: 12px;
  border: 1px solid var(--n-border-color, #e5e7eb);
  border-radius: 8px;
  background: rgba(148, 163, 184, 0.08);
}

.status-tile.complete {
  border-color: rgba(24, 160, 88, 0.4);
  background: rgba(24, 160, 88, 0.08);
}

.status-tile strong,
.status-tile span {
  display: block;
}

.status-tile span {
  margin-top: 4px;
  color: var(--n-text-color-3, #6b7280);
  font-size: 12px;
  line-height: 1.4;
}

.wizard-body {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  min-height: 560px;
}

.step-rail {
  padding: 20px;
  border-right: 1px solid var(--n-border-color, #e5e7eb);
  background: rgba(15, 23, 42, 0.03);
}

.rail-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  margin-bottom: 8px;
  padding: 8px;
  color: inherit;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 8px;
  cursor: pointer;
  text-align: left;
}

.rail-item.active {
  background: var(--n-card-color, #fff);
  border-color: rgba(24, 160, 88, 0.45);
}

.rail-item.done .rail-index {
  background: #18a058;
  color: #fff;
}

.rail-index {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 999px;
  background: rgba(148, 163, 184, 0.25);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.step-panel {
  padding: 28px;
}

.step-content {
  max-width: 760px;
}

.step-title {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 24px;
}

.step-title :deep(.n-icon) {
  margin-top: 4px;
  color: #18a058;
  font-size: 28px;
}

.step-title h2 {
  margin: 0 0 6px;
  font-size: 22px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}

.inline-alert {
  margin: 0 0 16px;
}

.anime-list {
  display: grid;
  gap: 10px;
  max-height: 360px;
  overflow: auto;
  margin-bottom: 16px;
}

.anime-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  width: 100%;
  min-height: 64px;
  padding: 12px;
  color: inherit;
  background: var(--n-card-color, #fff);
  border: 1px solid var(--n-border-color, #e5e7eb);
  border-radius: 8px;
  cursor: pointer;
  text-align: left;
}

.anime-row.selected {
  border-color: #18a058;
  box-shadow: inset 3px 0 0 #18a058;
}

.anime-row strong,
.anime-row small {
  display: block;
}

.anime-row small {
  margin-top: 4px;
  color: var(--n-text-color-3, #6b7280);
}

.notification-form {
  margin-top: 20px;
}

.step-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 32px;
}

@media (max-width: 900px) {
  .onboarding-page {
    padding: 12px;
  }

  .wizard-header {
    flex-direction: column;
    padding: 20px;
  }

  .status-grid {
    grid-template-columns: 1fr;
    padding: 14px 20px;
  }

  .wizard-body {
    grid-template-columns: 1fr;
  }

  .step-rail {
    display: flex;
    gap: 8px;
    overflow-x: auto;
    border-right: 0;
    border-bottom: 1px solid var(--n-border-color, #e5e7eb);
  }

  .rail-item {
    min-width: 126px;
    margin-bottom: 0;
  }

  .step-panel {
    padding: 20px;
  }

  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
