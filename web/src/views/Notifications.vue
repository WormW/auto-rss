<template>
  <div class="notifications-page">
    <h2 class="page-title">通知管理</h2>

    <!-- WebSocket 状态 -->
    <n-card class="status-card" size="small">
      <n-space align="center">
        <n-tag :type="wsStatus.enabled ? 'success' : 'default'">
          WebSocket {{ wsStatus.enabled ? '已连接' : '未连接' }}
        </n-tag>
        <n-text v-if="wsStatus.enabled" depth="3">
          在线客户端: {{ wsStatus.connected_clients }}
        </n-text>
      </n-space>
    </n-card>

    <!-- 通知渠道列表 -->
    <n-card title="通知渠道" class="config-card">
      <n-space vertical :size="16">
        <n-alert type="info" :show-icon="false">
          配置通知渠道后，Auto-RSS 将在下载完成、失败、磁盘预警等事件发生时发送通知。
        </n-alert>

        <n-list>
          <n-list-item v-for="channel in channels" :key="channel.channel">
            <n-thing>
              <template #header>
                <n-space align="center">
                  <n-tag :type="channel.enabled ? 'success' : 'default'">
                    {{ channel.enabled ? '已启用' : '已禁用' }}
                  </n-tag>
                  <span>{{ getChannelDisplayName(channel.channel) }}</span>
                </n-space>
              </template>
              <template #header-extra>
                <n-space>
                  <n-button size="small" @click="handleEdit(channel)">编辑</n-button>
                  <n-button size="small" type="error" @click="handleDelete(channel.channel)">删除</n-button>
                </n-space>
              </template>
              <template #description>
                <n-text depth="3" style="font-size: 12px;">
                  最后更新: {{ formatTime(channel.updated_at) }}
                </n-text>
              </template>
            </n-thing>
          </n-list-item>
        </n-list>

        <n-empty v-if="channels.length === 0" description="暂无通知渠道配置" />

        <n-button type="primary" @click="showAddDialog" block>添加通知渠道</n-button>
      </n-space>
    </n-card>

    <!-- 通知历史 -->
    <n-card title="通知历史" class="history-card">
      <n-space vertical :size="16">
        <n-space>
          <n-select
            v-model:value="filterStatus"
            :options="statusOptions"
            placeholder="状态筛选"
            clearable
            style="width: 120px"
          />
          <n-button @click="loadNotifications">刷新</n-button>
        </n-space>

        <n-table :data="notifications" size="small">
          <thead>
            <tr>
              <th>时间</th>
              <th>渠道</th>
              <th>标题</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in notifications" :key="item.id">
              <td>{{ formatTime(item.created_at) }}</td>
              <td>
                <n-tag size="small">{{ item.type }}</n-tag>
              </td>
              <td>
                <n-ellipsis style="max-width: 300px">{{ item.title }}</n-ellipsis>
              </td>
              <td>
                <n-tag :type="getStatusType(item.status)" size="small">
                  {{ item.status }}
                </n-tag>
              </td>
            </tr>
          </tbody>
        </n-table>

        <n-pagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :item-count="total"
          :page-sizes="[10, 20, 50]"
          show-size-picker
          @update:page="loadNotifications"
          @update:page-size="loadNotifications"
        />
      </n-space>
    </n-card>

    <!-- 添加/编辑通知渠道对话框 -->
    <n-modal v-model:show="showModal" :title="editingChannel ? '编辑渠道' : '添加渠道'">
      <n-card style="width: 600px; max-width: 90vw;">
        <n-form :model="formData" label-placement="left" label-width="100px">
          <n-form-item label="渠道类型">
            <n-select
              v-model:value="formData.channel"
              :options="channelTypeOptions"
              :disabled="!!editingChannel"
              placeholder="选择渠道类型"
            />
          </n-form-item>

          <n-form-item label="启用">
            <n-switch v-model:value="formData.enabled" />
          </n-form-item>

          <!-- Telegram 配置 -->
          <template v-if="formData.channel === 'telegram'">
            <n-form-item label="Bot Token">
              <n-input
                v-model:value="telegramConfig.bot_token"
                type="password"
                placeholder="从 @BotFather 获取"
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="Chat ID">
              <n-input
                v-model:value="telegramConfig.chat_id"
                placeholder="用户或群组 ID"
              />
            </n-form-item>
          </template>

          <!-- Webhook 配置 -->
          <template v-if="formData.channel?.startsWith('webhook')">
            <n-form-item label="名称">
              <n-input v-model:value="webhookConfig.name" placeholder="如: nanobot、openclaw" />
            </n-form-item>
            <n-form-item label="Webhook URL">
              <n-input v-model:value="webhookConfig.url" placeholder="http://localhost:8080/webhook" />
            </n-form-item>
            <n-form-item label="HTTP 方法">
              <n-select v-model:value="webhookConfig.method" :options="httpMethodOptions" />
            </n-form-item>
            <n-form-item label="模板预设">
              <n-select
                v-model:value="selectedTemplate"
                :options="templateOptions"
                placeholder="选择预设模板"
                @update:value="applyTemplate"
              />
            </n-form-item>
            <n-form-item label="请求体模板">
              <n-input
                v-model:value="webhookConfig.body_template"
                type="textarea"
                :autosize="{ minRows: 5, maxRows: 10 }"
                placeholder="JSON 模板，支持 {{.Title}} 等变量"
              />
            </n-form-item>
            <n-form-item label="Headers">
              <n-input
                v-model:value="headersText"
                type="textarea"
                :autosize="{ minRows: 2, maxRows: 4 }"
                placeholder="Content-Type: application/json\nAuthorization: Bearer xxx"
              />
            </n-form-item>
            <n-form-item label="密钥 (可选)">
              <n-input
                v-model:value="webhookConfig.secret"
                type="password"
                placeholder="用于 HMAC 签名"
                show-password-on="click"
              />
            </n-form-item>
            <n-form-item label="启用重试">
              <n-switch v-model:value="webhookConfig.retry_enabled" />
            </n-form-item>
          </template>
        </n-form>

        <template #footer>
          <n-space justify="end">
            <n-button @click="showModal = false">取消</n-button>
            <n-button @click="handleTest" :loading="testing">测试</n-button>
            <n-button type="primary" @click="handleSave" :loading="saving">保存</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import {
  NCard,
  NSpace,
  NButton,
  NTag,
  NText,
  NList,
  NListItem,
  NThing,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSwitch,
  NTable,
  NPagination,
  NAlert,
  NEllipsis,
  NEmpty,
  useMessage,
  useDialog
} from 'naive-ui'
import { notificationApi, type NotificationSetting, type WebhookTemplate } from '@/api'

const message = useMessage()
const dialog = useDialog()

// WebSocket 状态
const wsStatus = ref({ enabled: false, connected_clients: 0 })

// 渠道列表
const channels = ref<NotificationSetting[]>([])

// 通知历史
const notifications = ref<any[]>([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filterStatus = ref('')

// 对话框状态
const showModal = ref(false)
const editingChannel = ref('')
const saving = ref(false)
const testing = ref(false)

// 表单数据
const formData = ref({
  channel: '',
  enabled: true,
  config: {}
})

// Telegram 配置
const telegramConfig = ref({
  bot_token: '',
  chat_id: ''
})

// Webhook 配置
const webhookConfig = ref({
  name: '',
  url: '',
  method: 'POST',
  headers: {} as Record<string, string>,
  body_template: '',
  secret: '',
  retry_enabled: true,
  timeout_sec: 30
})

// 模板选择
const selectedTemplate = ref('')
const webhookTemplates = ref<WebhookTemplate[]>([])

// 选项
const channelTypeOptions = [
  { label: 'Telegram', value: 'telegram' },
  { label: 'Webhook (通用)', value: 'webhook' },
  { label: 'Webhook - Nanobot', value: 'webhook.nanobot' },
  { label: 'Webhook - OpenClaw', value: 'webhook.openclaw' },
  { label: 'Webhook - Discord', value: 'webhook.discord' },
  { label: 'Webhook - Slack', value: 'webhook.slack' }
]

const httpMethodOptions = [
  { label: 'POST', value: 'POST' },
  { label: 'PUT', value: 'PUT' },
  { label: 'PATCH', value: 'PATCH' }
]

const statusOptions = [
  { label: '全部', value: '' },
  { label: '已发送', value: 'sent' },
  { label: '失败', value: 'failed' }
]

const templateOptions = computed(() => [
  { label: '不使用预设', value: '' },
  ...webhookTemplates.value.map(t => ({ label: t.label, value: t.name }))
])

// Headers 文本（用于编辑）
const headersText = computed({
  get: () => {
    return Object.entries(webhookConfig.value.headers)
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n')
  },
  set: (val: string) => {
    const headers: Record<string, string> = {}
    val.split('\n').forEach(line => {
      const idx = line.indexOf(':')
      if (idx > 0) {
        headers[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
      }
    })
    webhookConfig.value.headers = headers
  }
})

// 获取渠道显示名称
const getChannelDisplayName = (channel: string) => {
  const map: Record<string, string> = {
    'telegram': 'Telegram',
    'webhook': 'Webhook',
    'webhook.nanobot': 'Nanobot',
    'webhook.openclaw': 'OpenClaw',
    'webhook.discord': 'Discord',
    'webhook.slack': 'Slack'
  }
  return map[channel] || channel
}

// 状态标签类型
const getStatusType = (status: string) => {
  const map: Record<string, any> = {
    'sent': 'success',
    'failed': 'error',
    'pending': 'warning'
  }
  return map[status] || 'default'
}

// 格式化时间
const formatTime = (time: string) => {
  if (!time) return '-'
  const date = new Date(time)
  return date.toLocaleString()
}

// 加载渠道列表
const loadChannels = async () => {
  try {
    const res: any = await notificationApi.getSettings()
    channels.value = res.data || []
  } catch (error: any) {
    message.error(error.message || '加载渠道列表失败')
  }
}

// 加载 WebSocket 状态
const loadWSStatus = async () => {
  try {
    const res: any = await notificationApi.getWebSocketStatus()
    wsStatus.value = res.data
  } catch (error) {
    // 忽略错误
  }
}

// 加载通知历史
const loadNotifications = async () => {
  try {
    const res: any = await notificationApi.listNotifications(
      page.value,
      pageSize.value,
      filterStatus.value || undefined
    )
    notifications.value = res.data?.list || []
    total.value = res.data?.total || 0
  } catch (error: any) {
    message.error(error.message || '加载通知历史失败')
  }
}

// 加载 Webhook 模板
const loadWebhookTemplates = async () => {
  try {
    const res: any = await notificationApi.getWebhookTemplates()
    webhookTemplates.value = res.data || []
  } catch (error) {
    // 忽略错误
  }
}

// 应用模板
const applyTemplate = (templateName: string) => {
  const template = webhookTemplates.value.find(t => t.name === templateName)
  if (template) {
    webhookConfig.value.body_template = template.template
  }
}

// 显示添加对话框
const showAddDialog = () => {
  editingChannel.value = ''
  formData.value = {
    channel: 'webhook',
    enabled: true,
    config: {}
  }
  telegramConfig.value = { bot_token: '', chat_id: '' }
  webhookConfig.value = {
    name: '',
    url: '',
    method: 'POST',
    headers: {},
    body_template: '',
    secret: '',
    retry_enabled: true,
    timeout_sec: 30
  }
  selectedTemplate.value = ''
  showModal.value = true
}

// 编辑渠道
const handleEdit = (channel: NotificationSetting) => {
  editingChannel.value = channel.channel
  formData.value = {
    channel: channel.channel,
    enabled: channel.enabled,
    config: {}
  }

  try {
    const config = JSON.parse(channel.config)
    if (channel.channel === 'telegram') {
      telegramConfig.value = {
        bot_token: config.bot_token || '',
        chat_id: config.chat_id || ''
      }
    } else if (channel.channel.startsWith('webhook')) {
      webhookConfig.value = {
        name: config.name || '',
        url: config.url || '',
        method: config.method || 'POST',
        headers: config.headers || {},
        body_template: config.body_template || '',
        secret: config.secret || '',
        retry_enabled: config.retry_enabled !== false,
        timeout_sec: config.timeout_sec || 30
      }
    }
  } catch (e) {
    // 解析失败使用默认值
  }

  showModal.value = true
}

// 删除渠道
const handleDelete = (channel: string) => {
  dialog.warning({
    title: '确认删除',
    content: `确定要删除 ${getChannelDisplayName(channel)} 配置吗？`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await notificationApi.deleteSetting(channel)
        message.success('删除成功')
        loadChannels()
      } catch (error: any) {
        message.error(error.message || '删除失败')
      }
    }
  })
}

// 测试配置
const handleTest = async () => {
  testing.value = true
  try {
    let config: any
    if (formData.value.channel === 'telegram') {
      config = telegramConfig.value
    } else {
      config = webhookConfig.value
    }

    await notificationApi.testChannel({
      channel: formData.value.channel,
      config
    })
    message.success('测试消息已发送，请检查接收端')
  } catch (error: any) {
    message.error(error.message || '测试失败')
  } finally {
    testing.value = false
  }
}

// 保存配置
const handleSave = async () => {
  saving.value = true
  try {
    let config: any
    if (formData.value.channel === 'telegram') {
      config = telegramConfig.value
    } else {
      config = webhookConfig.value
    }

    await notificationApi.updateSetting({
      channel: formData.value.channel,
      enabled: formData.value.enabled,
      config
    })
    message.success('保存成功')
    showModal.value = false
    loadChannels()
  } catch (error: any) {
    message.error(error.message || '保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadChannels()
  loadWSStatus()
  loadNotifications()
  loadWebhookTemplates()
})
</script>

<style scoped>
.notifications-page {
  max-width: 1200px;
  margin: 0 auto;
}

.page-title {
  margin-bottom: 20px;
}

.status-card {
  margin-bottom: 20px;
}

.config-card,
.history-card {
  margin-bottom: 20px;
}
</style>
