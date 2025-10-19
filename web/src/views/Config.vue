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

    <n-card title="操作" style="margin-top: 16px">
      <n-space>
        <n-button type="info" @click="handleManualRefresh">手动刷新 RSS</n-button>
        <n-button type="warning" @click="handleClearCache">清理缓存</n-button>
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
  useMessage
} from 'naive-ui'
import { configApi, rssApi } from '@/api'

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

const logLevelOptions = [
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' }
]

const loadConfig = async () => {
  try {
    const res: any = await configApi.getAll()
    const configs = Array.isArray(res.data) ? res.data : []

    configs.forEach((config: any) => {
      if (!config.key || !config.value) return

      switch (config.key) {
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
      }
    })
  } catch (error) {
    message.error('加载配置失败')
  }
}

const saveQBConfig = async () => {
  try {
    await configApi.update('qb_host', qbConfig.value.host)
    await configApi.update('qb_username', qbConfig.value.username)
    if (qbConfig.value.password) {
      await configApi.update('qb_password', qbConfig.value.password)
    }
    message.success('qBittorrent 配置保存成功')
  } catch (error) {
    message.error('保存配置失败')
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

const testConnection = async () => {
  // TODO: 实现连接测试
  message.info('连接测试功能待实现')
}

const handleManualRefresh = async () => {
  try {
    await rssApi.refresh()
    message.success('RSS 刷新已触发')
  } catch (error) {
    message.error('RSS 刷新失败')
  }
}

const handleClearCache = () => {
  // TODO: 实现缓存清理
  message.info('缓存清理功能待实现')
}

onMounted(() => {
  loadConfig()
})
</script>
