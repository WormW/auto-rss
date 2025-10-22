<template>
  <div>
    <h2>系统配置</h2>

    <n-card title="qBittorrent 配置" style="margin-top: 16px">
      <n-form :model="qbConfig" label-placement="left" label-width="120">
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

    <n-card title="RSS 配置" style="margin-top: 16px">
      <n-form :model="rssConfig" label-placement="left" label-width="120">
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

    <n-card title="系统设置" style="margin-top: 16px">
      <n-form :model="systemConfig" label-placement="left" label-width="120">
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

    <n-card title="文件整理配置" style="margin-top: 16px">
      <n-form :model="fileOrganizerConfig" label-placement="left" label-width="120">
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

    <n-card title="文件重命名规则" style="margin-top: 16px">
      <n-form label-placement="left" label-width="120">
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

    <n-card title="操作" style="margin-top: 16px">
      <n-space>
        <n-button type="info" @click="handleManualRefresh">手动刷新 RSS</n-button>
        <n-button type="success" @click="handleTriggerFileOrganizer">手动整理文件</n-button>
        <n-button type="warning" @click="handleClearCache">清理缓存</n-button>
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
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
  useMessage
} from 'naive-ui'
import { configApi, rssApi, fileOrganizerApi } from '@/api'

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

const systemConfig = ref({
  logLevel: 'info',
  autoRename: true,
  proxy: ''
})

const fileOrganizerConfig = ref({
  enabled: false,
  dir: ''
})

const logLevelOptions = [
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' }
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
      presetOptions.value = Object.entries(presets).map(([key, value]) => ({
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
    message.loading('正在测试连接...', { duration: 0, key: 'test-qb' })
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

const handleClearCache = () => {
  // TODO: 实现缓存清理
  message.info('缓存清理功能待实现')
}

onMounted(() => {
  loadConfig()
  loadRenamePresets()
  loadRenameTemplate()
})
</script>
