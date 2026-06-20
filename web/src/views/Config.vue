<template>
  <div class="config-page">
    <h2 class="page-title">系统配置</h2>

    <n-card title="qBittorrent 配置" class="config-card">
      <n-form :model="qbConfig" label-placement="top" class="config-form">
        <n-form-item label="主机地址">
          <n-input v-model:value="qbConfig.host" placeholder="http://localhost:8080" />
        </n-form-item>
        <n-form-item label="用户名">
          <n-input v-model:value="qbConfig.username" placeholder="admin" />
        </n-form-item>
        <n-form-item label="密码">
          <n-input
            v-model:value="qbConfig.password"
            type="password"
            show-password-on="click"
            placeholder="请输入密码"
          />
        </n-form-item>
        <n-form-item>
          <n-space>
            <n-button type="primary" @click="testConnection">测试连接</n-button>
            <n-button @click="saveQBConfig">保存配置</n-button>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card title="RSS 配置" class="config-card">
      <n-form :model="rssConfig" label-placement="top" class="config-form">
        <n-form-item label="检查间隔">
          <n-input-number
            v-model:value="rssConfig.interval"
            :min="5"
            :max="1440"
            placeholder="30"
          >
            <template #suffix>分钟</template>
          </n-input-number>
        </n-form-item>
        <n-form-item label="默认下载路径">
          <n-input v-model:value="rssConfig.downloadPath" placeholder="/downloads" />
        </n-form-item>
        <n-form-item>
          <n-button type="primary" @click="saveRSSConfig">保存配置</n-button>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card title="智能拉取策略" class="config-card">
      <n-form :model="smartFetchConfig" label-placement="top" class="config-form">
        <n-form-item label="启用智能拉取">
          <n-switch v-model:value="smartFetchConfig.enabled" />
        </n-form-item>
        <n-form-item label="更新日前窗口">
          <n-input-number
            v-model:value="smartFetchConfig.before_air_day"
            :min="0"
            :max="7"
            style="width: 100%;"
          >
            <template #suffix>天</template>
          </n-input-number>
        </n-form-item>
        <n-form-item label="更新日后窗口">
          <n-input-number
            v-model:value="smartFetchConfig.after_air_day"
            :min="0"
            :max="7"
            style="width: 100%;"
          >
            <template #suffix>天</template>
          </n-input-number>
        </n-form-item>
        <n-form-item label="跳过已完结">
          <n-switch v-model:value="smartFetchConfig.skip_completed" />
        </n-form-item>
        <n-form-item label="完结停止检查">
          <n-input-number
            v-model:value="smartFetchConfig.completed_stop_days"
            :min="0"
            :max="365"
            style="width: 100%;"
          >
            <template #suffix>天</template>
          </n-input-number>
        </n-form-item>
        <n-form-item label="本地完整性检查">
          <n-switch v-model:value="smartFetchConfig.check_local_complete" />
        </n-form-item>
        <n-form-item>
          <n-button type="primary" :loading="smartFetchSaving" @click="saveSmartFetchConfig">保存配置</n-button>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card title="系统设置" class="config-card">
      <n-form :model="systemConfig" label-placement="top" class="config-form">
        <n-form-item label="日志级别">
          <n-select
            v-model:value="systemConfig.logLevel"
            :options="logLevelOptions"
            placeholder="选择日志级别"
          />
        </n-form-item>
        <n-form-item label="自动重命名">
          <n-switch v-model:value="systemConfig.autoRename" />
        </n-form-item>
        <n-form-item label="代理地址">
          <n-input
            v-model:value="systemConfig.proxy"
            placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:7890"
          />
        </n-form-item>
        <n-form-item>
          <n-button type="primary" @click="saveSystemConfig">保存配置</n-button>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card title="文件整理配置" class="config-card">
      <n-form :model="fileOrganizerConfig" label-placement="top" class="config-form">
        <n-form-item label="启用文件整理">
          <n-switch v-model:value="fileOrganizerConfig.enabled" />
        </n-form-item>
        <n-form-item label="整理目录">
          <n-input
            v-model:value="fileOrganizerConfig.dir"
            placeholder="/downloads"
            :disabled="!fileOrganizerConfig.enabled"
          />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px">
              监控此目录的文件变化，自动整理和重命名文件
            </n-text>
          </template>
        </n-form-item>
        <n-form-item>
          <n-button type="primary" @click="saveFileOrganizerConfig">保存配置</n-button>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card title="媒体库联动" class="config-card">
      <n-form :model="mediaLibraryConfig" label-placement="top" class="config-form">
        <n-form-item label="启用媒体库刷新">
          <n-switch v-model:value="mediaLibraryConfig.enabled" />
        </n-form-item>
        <n-form-item label="媒体库类型">
          <n-select
            v-model:value="mediaLibraryConfig.provider"
            :options="mediaLibraryProviderOptions"
            :disabled="!mediaLibraryConfig.enabled"
          />
        </n-form-item>
        <n-form-item label="服务地址">
          <n-input
            v-model:value="mediaLibraryConfig.base_url"
            placeholder="http://jellyfin:8096"
            :disabled="!mediaLibraryConfig.enabled"
          />
        </n-form-item>
        <n-form-item :label="mediaLibraryConfig.tokenConfigured ? '访问令牌（已保存）' : '访问令牌'">
          <n-input
            v-model:value="mediaLibraryConfig.token"
            type="password"
            show-password-on="click"
            :placeholder="mediaLibraryConfig.tokenConfigured ? '留空则继续使用已保存令牌' : '请输入 API Token'"
            :disabled="!mediaLibraryConfig.enabled"
          />
        </n-form-item>
        <n-form-item v-if="mediaLibraryConfig.provider === 'plex'" label="Plex Section ID">
          <n-input
            v-model:value="mediaLibraryConfig.section_id"
            placeholder="例如 1"
            :disabled="!mediaLibraryConfig.enabled"
          />
        </n-form-item>
        <n-form-item v-else label="媒体库标识（可选）">
          <n-input
            v-model:value="mediaLibraryConfig.library_id"
            placeholder="留空刷新全部媒体库"
            :disabled="!mediaLibraryConfig.enabled"
          />
        </n-form-item>
        <n-form-item label="整理完成后自动刷新">
          <n-switch
            v-model:value="mediaLibraryConfig.refresh_on_import"
            :disabled="!mediaLibraryConfig.enabled"
          />
        </n-form-item>
        <n-form-item label="路径映射">
          <div class="path-mapping-list">
            <div
              v-for="(mapping, index) in mediaLibraryConfig.path_mappings"
              :key="index"
              class="path-mapping-row"
            >
              <n-input
                v-model:value="mapping.from"
                placeholder="下载路径，如 /downloads"
                :disabled="!mediaLibraryConfig.enabled"
              />
              <span class="mapping-arrow">→</span>
              <n-input
                v-model:value="mapping.to"
                placeholder="媒体库路径，如 /media/anime"
                :disabled="!mediaLibraryConfig.enabled"
              />
              <n-button
                circle
                secondary
                size="small"
                :disabled="!mediaLibraryConfig.enabled || mediaLibraryConfig.path_mappings.length <= 1"
                @click="removePathMapping(index)"
              >
                <template #icon><n-icon><TrashOutline /></n-icon></template>
              </n-button>
            </div>
            <n-button
              secondary
              size="small"
              :disabled="!mediaLibraryConfig.enabled"
              @click="addPathMapping"
            >
              添加映射
            </n-button>
          </div>
          <template #feedback>
            <n-text depth="3" style="font-size: 12px">
              当 Auto-RSS 和媒体库容器看到的路径不同时配置映射，例如 /downloads → /media/anime。
            </n-text>
          </template>
        </n-form-item>
        <n-form-item>
          <n-space>
            <n-button type="primary" @click="saveMediaLibraryConfig">保存配置</n-button>
            <n-button @click="testMediaLibraryConnection">测试连接</n-button>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card title="文件重命名规则" class="config-card">
      <n-form label-placement="top" class="config-form">
        <n-form-item label="模板预设">
          <n-select
            v-model:value="selectedPreset"
            :options="presetOptions"
            placeholder="选择预设模板"
            @update:value="handlePresetChange"
          />
        </n-form-item>
        <n-form-item label="自定义模板">
          <n-input
            v-model:value="renameTemplate"
            type="textarea"
            placeholder="${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
            :autosize="{
              minRows: 2,
              maxRows: 4
            }"
            @update:value="handleTemplateChange"
          />
        </n-form-item>
        <n-form-item label="可用变量">
          <n-space style="flex-wrap: wrap">
            <n-tag
              v-for="(desc, variable) in templateVariables"
              :key="variable"
              type="info"
              size="small"
              style="cursor: pointer"
              @click="insertVariable(variable)"
            >
              {{ variable }} - {{ desc }}
            </n-tag>
          </n-space>
        </n-form-item>
        <n-form-item label="预览效果">
          <n-alert v-if="previewPath" type="success" style="margin-bottom: 12px">
            <div style="font-family: monospace">{{ previewPath }}</div>
            <template #header>
              预览结果
            </template>
          </n-alert>
          <n-text depth="3" style="font-size: 12px">
            示例：{{ previewSample }}
          </n-text>
        </n-form-item>
        <n-form-item>
          <n-space>
            <n-button type="primary" @click="saveRenameTemplate">保存模板</n-button>
            <n-button @click="previewRename">实时预览</n-button>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card title="操作" class="config-card">
      <n-space wrap>
        <n-button type="info" @click="handleManualRefresh">手动刷新 RSS</n-button>
        <n-button type="success" @click="handleTriggerFileOrganizer">手动整理文件</n-button>
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NButton,
  NSpace,
  NSwitch,
  NSelect,
  NTag,
  NAlert,
  NText,
  NIcon,
  useMessage
} from 'naive-ui'
import { TrashOutline } from '@vicons/ionicons5'
import {
  configApi,
  rssApi,
  fileOrganizerApi,
  mediaLibraryApi,
  type MediaLibraryConfig,
  type SmartFetchConfig
} from '@/api'

const message = useMessage()

const qbConfig = ref({
  host: 'http://localhost:8080',
  username: 'admin',
  password: ''
})

const rssConfig = ref({
  interval: 30,
  downloadPath: '/downloads'
})

const smartFetchConfig = ref<SmartFetchConfig>({
  enabled: true,
  before_air_day: 1,
  after_air_day: 2,
  skip_completed: false,
  completed_stop_days: 30,
  check_local_complete: true
})
const smartFetchSaving = ref(false)

const systemConfig = ref({
  logLevel: 'info',
  autoRename: true,
  proxy: ''
})

const fileOrganizerConfig = ref({
  enabled: false,
  dir: ''
})

const mediaLibraryConfig = ref<MediaLibraryConfig & { tokenConfigured?: boolean }>({
  enabled: false,
  provider: 'jellyfin',
  base_url: '',
  token: '',
  tokenConfigured: false,
  library_id: '',
  section_id: '',
  refresh_on_import: true,
  path_mappings: [{ from: '/downloads', to: '/media/anime' }]
})

const logLevelOptions = [
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' }
]

const mediaLibraryProviderOptions = [
  { label: 'Jellyfin', value: 'jellyfin' },
  { label: 'Emby', value: 'emby' },
  { label: 'Plex', value: 'plex' }
]

// 重命名模板相关
const renameTemplate = ref('${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}')
const selectedPreset = ref('media_library')
const presetOptions = ref<Array<{ label: string; value: string }>>([])
const templateVariables = ref<Record<string, string>>({})
const previewPath = ref('')
const previewSample = ref('葬送的芙莉莲 S01E03')

// 加载重命名模板预设
const loadRenamePresets = async () => {
  try {
    const res: any = await configApi.getRenamePresets()
    if (res.code === 0 && res.data) {
      // 构建预设选项
      const presets = res.data.presets || {}
    presetOptions.value = Object.entries(presets).map(([key]) => ({
      label: getPresetLabel(key),
      value: key
    }))

      // 保存变量说明
      templateVariables.value = res.data.variables || {}
    }
  } catch (error) {
    console.error('加载重命名模板预设失败:', error)
  }
}

// 获取预设的中文标签
const getPresetLabel = (key: string): string => {
  const labels: Record<string, string> = {
    'media_library': '媒体库标准格式 (Plex/Jellyfin)',
    'media_library_fansub': '媒体库 + 字幕组',
    'media_library_full': '媒体库完整信息',
    'simple': '简洁格式',
    'fansub_style': '字幕组风格',
    'detailed': '详细信息格式'
  }
  return labels[key] || key
}

// 处理预设选择
const handlePresetChange = async (value: string) => {
  try {
    const res: any = await configApi.getRenamePresets()
    if (res.code === 0 && res.data) {
      const presets = res.data.presets || {}
      if (presets[value]) {
        renameTemplate.value = presets[value]
        await previewRename()
      }
    }
  } catch (error) {
    message.error('切换预设失败')
  }
}

// 插入变量
const insertVariable = (variable: string) => {
  renameTemplate.value += variable
  previewRename()
}

// 模板变化时实时预览
const handleTemplateChange = () => {
  // 可以添加防抖逻辑
  previewRename()
}

// 预览重命名效果
const previewRename = async () => {
  if (!renameTemplate.value) {
    previewPath.value = ''
    return
  }

  try {
    const res: any = await configApi.previewRenameTemplate(renameTemplate.value)
    if (res.code === 0 && res.data) {
      previewPath.value = res.data.preview
      // 更新示例数据显示
      if (res.data.sample) {
        const s = res.data.sample
        previewSample.value = `${s.title} S${String(s.season).padStart(2, '0')}E${String(s.episode).padStart(2, '0')}`
      }
    }
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '预览失败'
    message.warning(errorMsg)
    previewPath.value = ''
  }
}

// 保存重命名模板
const saveRenameTemplate = async () => {
  if (!renameTemplate.value) {
    message.warning('请输入重命名模板')
    return
  }

  try {
    await configApi.saveRenameTemplate(renameTemplate.value)
    message.success('重命名模板保存成功')
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '保存模板失败'
    message.error(errorMsg)
  }
}

// 加载当前重命名模板
const loadRenameTemplate = async () => {
  try {
    const res: any = await configApi.getRenameTemplate()
    if (res.code === 0 && res.data) {
      renameTemplate.value = res.data.template
      await previewRename()
    }
  } catch (error) {
    console.error('加载重命名模板失败:', error)
  }
}

const loadConfig = async () => {
  try {
    const res: any = await configApi.getAll()
    const configs = Array.isArray(res.data) ? res.data : []

    configs.forEach((config: any) => {
      if (!config.key || !config.value) return

      switch (config.key) {
        case 'qbittorrent_host':
          qbConfig.value.host = config.value
          break
        case 'qbittorrent_username':
          qbConfig.value.username = config.value
          break
        case 'qbittorrent_password':
          qbConfig.value.password = config.value
          break
        case 'qb_host':
          qbConfig.value.host = config.value
          break
        case 'qb_username':
          qbConfig.value.username = config.value
          break
        case 'rss_interval':
          rssConfig.value.interval = parseInt(config.value) || 30
          break
        case 'download_path':
          rssConfig.value.downloadPath = config.value
          break
        case 'smart_fetch.enabled':
          smartFetchConfig.value.enabled = config.value === 'true'
          break
        case 'smart_fetch.before_air_day':
          smartFetchConfig.value.before_air_day = parseInt(config.value) || 1
          break
        case 'smart_fetch.after_air_day':
          smartFetchConfig.value.after_air_day = parseInt(config.value) || 2
          break
        case 'smart_fetch.skip_completed':
          smartFetchConfig.value.skip_completed = config.value === 'true'
          break
        case 'smart_fetch.completed_stop_days':
          smartFetchConfig.value.completed_stop_days = parseInt(config.value) || 0
          break
        case 'smart_fetch.check_local_complete':
          smartFetchConfig.value.check_local_complete = config.value !== 'false'
          break
        case 'system_proxy':
          systemConfig.value.proxy = config.value
          break
        case 'log_level':
          systemConfig.value.logLevel = config.value
          break
        case 'auto_rename':
          systemConfig.value.autoRename = config.value === 'true'
          break
        case 'file_organizer_enabled':
          fileOrganizerConfig.value.enabled = config.value === 'true'
          break
        case 'file_organizer_dir':
          fileOrganizerConfig.value.dir = config.value
          break
      }
    })
  } catch (error) {
    message.error('加载配置失败')
  }
}

const loadSmartFetchConfig = async () => {
  try {
    const res: any = await configApi.getSmartFetch()
    if (res.code === 0 && res.data) {
      smartFetchConfig.value = {
        ...smartFetchConfig.value,
        ...res.data
      }
    }
  } catch (error) {
    console.error('加载智能拉取配置失败:', error)
  }
}

const loadMediaLibraryConfig = async () => {
  try {
    const res: any = await mediaLibraryApi.getConfig()
    if (res.code === 0 && res.data) {
      mediaLibraryConfig.value = {
        enabled: Boolean(res.data.enabled),
        provider: res.data.provider || 'jellyfin',
        base_url: res.data.base_url || '',
        token: '',
        tokenConfigured: Boolean(res.data.token_configured),
        library_id: res.data.library_id || '',
        section_id: res.data.section_id || '',
        refresh_on_import: res.data.refresh_on_import !== false,
        path_mappings: Array.isArray(res.data.path_mappings) && res.data.path_mappings.length > 0
          ? res.data.path_mappings
          : [{ from: '/downloads', to: '/media/anime' }]
      }
    }
  } catch (error) {
    message.error('加载媒体库配置失败')
  }
}

const addPathMapping = () => {
  mediaLibraryConfig.value.path_mappings.push({ from: '', to: '' })
}

const removePathMapping = (index: number) => {
  mediaLibraryConfig.value.path_mappings.splice(index, 1)
  if (mediaLibraryConfig.value.path_mappings.length === 0) {
    addPathMapping()
  }
}

const buildMediaLibraryPayload = (): MediaLibraryConfig => ({
  enabled: mediaLibraryConfig.value.enabled,
  provider: mediaLibraryConfig.value.provider,
  base_url: mediaLibraryConfig.value.base_url,
  token: mediaLibraryConfig.value.token || undefined,
  library_id: mediaLibraryConfig.value.library_id || undefined,
  section_id: mediaLibraryConfig.value.section_id || undefined,
  refresh_on_import: mediaLibraryConfig.value.refresh_on_import,
  path_mappings: mediaLibraryConfig.value.path_mappings
    .map((mapping) => ({
      from: mapping.from.trim(),
      to: mapping.to.trim()
    }))
    .filter((mapping) => mapping.from || mapping.to)
})

const saveMediaLibraryConfig = async () => {
  try {
    const res: any = await mediaLibraryApi.saveConfig(buildMediaLibraryPayload())
    if (res.data) {
      mediaLibraryConfig.value.token = ''
      mediaLibraryConfig.value.tokenConfigured = Boolean(res.data.token_configured)
    }
    message.success('媒体库配置保存成功')
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '保存媒体库配置失败'
    message.error(errorMsg)
  }
}

const testMediaLibraryConnection = async () => {
  if (!mediaLibraryConfig.value.enabled) {
    message.warning('请先启用媒体库刷新')
    return
  }
  try {
    message.loading('正在测试媒体库连接...', { duration: 0 })
    await mediaLibraryApi.testConnection({
      ...buildMediaLibraryPayload(),
      enabled: true
    })
    message.destroyAll()
    message.success('媒体库连接测试成功')
  } catch (error: any) {
    message.destroyAll()
    const errorMsg = error?.response?.data?.message || '媒体库连接测试失败'
    message.error(errorMsg)
  }
}

const saveQBConfig = async () => {
  if (!qbConfig.value.host || !qbConfig.value.username || !qbConfig.value.password) {
    message.warning('请填写完整的 qBittorrent 配置信息')
    return
  }

  try {
    await configApi.saveQBittorrent(
      qbConfig.value.host,
      qbConfig.value.username,
      qbConfig.value.password
    )
    message.success('qBittorrent 配置保存成功')
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '保存配置失败'
    message.error(errorMsg)
  }
}

const saveRSSConfig = async () => {
  try {
    await configApi.update('rss_interval', rssConfig.value.interval.toString())
    await configApi.update('download_path', rssConfig.value.downloadPath)
    message.success('RSS 配置保存成功')
  } catch (error) {
    message.error('保存配置失败')
  }
}

const saveSmartFetchConfig = async () => {
  smartFetchSaving.value = true
  try {
    await configApi.updateSmartFetch(smartFetchConfig.value)
    message.success('智能拉取配置保存成功')
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '保存配置失败'
    message.error(errorMsg)
  } finally {
    smartFetchSaving.value = false
  }
}

const saveSystemConfig = async () => {
  try {
    await configApi.update('log_level', systemConfig.value.logLevel)
    await configApi.update('auto_rename', systemConfig.value.autoRename.toString())
    await configApi.update('system_proxy', systemConfig.value.proxy)
    message.success('系统配置保存成功')
  } catch (error) {
    message.error('保存配置失败')
  }
}

const saveFileOrganizerConfig = async () => {
  if (fileOrganizerConfig.value.enabled && !fileOrganizerConfig.value.dir) {
    message.warning('启用文件整理时，整理目录不能为空')
    return
  }

  try {
    await configApi.update('file_organizer_enabled', fileOrganizerConfig.value.enabled.toString())
    await configApi.update('file_organizer_dir', fileOrganizerConfig.value.dir)

    // 重新加载文件整理配置
    try {
      await fileOrganizerApi.reloadConfig()
      message.success('文件整理配置保存并应用成功')
    } catch (reloadError: any) {
      const reloadMsg = reloadError?.response?.data?.message || '配置已保存，但重新加载失败'
      message.warning(reloadMsg)
    }
  } catch (error) {
    message.error('保存配置失败')
  }
}

const testConnection = async () => {
  if (!qbConfig.value.host || !qbConfig.value.username || !qbConfig.value.password) {
    message.warning('请填写完整的 qBittorrent 配置信息')
    return
  }

  try {
    message.loading('正在测试连接...', { duration: 0 })
    await configApi.testQBittorrent(
      qbConfig.value.host,
      qbConfig.value.username,
      qbConfig.value.password
    )
    message.destroyAll()
    message.success('qBittorrent 连接测试成功')
  } catch (error: any) {
    message.destroyAll()
    const errorMsg = error?.response?.data?.message || '连接测试失败'
    message.error(errorMsg)
  }
}

const handleManualRefresh = async () => {
  try {
    await rssApi.refresh()
    message.success('RSS 刷新已触发')
  } catch (error) {
    message.error('RSS 刷新失败')
  }
}

const handleTriggerFileOrganizer = async () => {
  try {
    await fileOrganizerApi.triggerScan()
    message.success('文件整理任务已触发')
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '触发文件整理失败'
    message.error(errorMsg)
  }
}

onMounted(() => {
  loadConfig()
  loadSmartFetchConfig()
  loadMediaLibraryConfig()
  loadRenamePresets()
  loadRenameTemplate()
})
</script>

<style scoped>
.config-page {
  max-width: 100%;
}

.page-title {
  margin: 0 0 16px 0;
  font-size: 20px;
}

.config-card {
  margin-bottom: 16px;
}

.config-form {
  max-width: 600px;
}

.path-mapping-list {
  display: flex;
  width: 100%;
  flex-direction: column;
  gap: 8px;
}

.path-mapping-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
}

.mapping-arrow {
  color: #8a8f98;
  font-size: 14px;
}

/* 移动端响应式 */
@media (max-width: 768px) {
  .page-title {
    font-size: 18px;
  }

  .config-card :deep(.n-card__content) {
    padding: 12px;
  }

  .config-form {
    max-width: 100%;
  }

  .config-form :deep(.n-form-item) {
    margin-bottom: 12px;
  }

  .config-form :deep(.n-input-number) {
    width: 100% !important;
  }

  .config-form :deep(.n-select) {
    width: 100% !important;
  }

  /* 可用变量标签换行显示 */
  .config-form :deep(.n-space) {
    gap: 6px !important;
  }

  .config-form :deep(.n-tag) {
    font-size: 11px;
  }

  /* 操作按钮组 */
  .config-card .n-space {
    width: 100%;
  }

  .config-card .n-space .n-button {
    flex: 1;
    min-width: 0;
  }
}
</style>
