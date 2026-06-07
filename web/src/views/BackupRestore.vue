<template>
  <div class="backup-page">
    <div class="page-heading">
      <div>
        <h2 class="page-title">备份恢复</h2>
        <p class="page-subtitle">导出完整配置包，预览差异后再恢复或迁移。</p>
      </div>
      <n-space>
        <n-tooltip>
          <template #trigger>
            <n-button :loading="exporting" type="primary" @click="exportBackup">
              <template #icon>
                <n-icon><DownloadOutline /></n-icon>
              </template>
              导出备份
            </n-button>
          </template>
          默认脱敏密码、Token 和通知密钥
        </n-tooltip>
      </n-space>
    </div>

    <n-alert type="info" class="top-alert">
      默认导出的敏感字段会以占位符保存。恢复时这些占位符会被跳过，不会覆盖本机已有密钥。
    </n-alert>

    <div class="summary-grid">
      <n-card title="备份内容" class="panel-card">
        <n-space vertical size="large">
          <n-checkbox v-model:checked="includeSensitive">
            导出时包含敏感字段
          </n-checkbox>
          <n-descriptions v-if="lastExportSummary" bordered :column="2" size="small">
            <n-descriptions-item label="订阅">{{ lastExportSummary.subscriptions }}</n-descriptions-item>
            <n-descriptions-item label="RSS 源">{{ lastExportSummary.rss_sources }}</n-descriptions-item>
            <n-descriptions-item label="分组">{{ lastExportSummary.groups }}</n-descriptions-item>
            <n-descriptions-item label="标签">{{ lastExportSummary.tags }}</n-descriptions-item>
            <n-descriptions-item label="系统配置">{{ lastExportSummary.configs }}</n-descriptions-item>
            <n-descriptions-item label="通知配置">{{ lastExportSummary.notification_settings }}</n-descriptions-item>
          </n-descriptions>
          <n-empty v-else description="尚未导出备份" size="small" />
        </n-space>
      </n-card>

      <n-card title="恢复策略" class="panel-card">
        <n-form label-placement="top">
          <n-form-item label="来源格式">
            <n-select v-model:value="sourceFormat" :options="sourceOptions" />
          </n-form-item>
          <n-form-item label="冲突处理">
            <n-radio-group v-model:value="strategy">
              <n-space vertical>
                <n-radio value="skip">跳过已有项目</n-radio>
                <n-radio value="merge">合并缺失字段</n-radio>
                <n-radio value="overwrite">覆盖已有项目</n-radio>
              </n-space>
            </n-radio-group>
          </n-form-item>
        </n-form>
      </n-card>
    </div>

    <n-card title="导入预览" class="panel-card">
      <n-space vertical size="large">
        <div class="import-toolbar">
          <n-upload
            :default-upload="false"
            :max="1"
            accept=".json,application/json"
            @change="handleFileChange"
            @remove="clearImportState"
          >
            <n-button>
              <template #icon>
                <n-icon><CloudUploadOutline /></n-icon>
              </template>
              选择备份文件
            </n-button>
          </n-upload>
          <n-space>
            <n-button :disabled="!importData" :loading="previewing" @click="previewImport">
              <template #icon>
                <n-icon><EyeOutline /></n-icon>
              </template>
              预览差异
            </n-button>
            <n-button
              type="primary"
              :disabled="!plan || importableCount === 0"
              :loading="importing"
              @click="applyImport"
            >
              <template #icon>
                <n-icon><CloudDownloadOutline /></n-icon>
              </template>
              执行导入
            </n-button>
          </n-space>
        </div>

        <n-alert v-if="fileName" type="default">
          当前文件：{{ fileName }}
        </n-alert>

        <div v-if="plan" class="plan-summary">
          <n-statistic label="新增" :value="plan.summary.create" />
          <n-statistic label="覆盖" :value="plan.summary.overwrite" />
          <n-statistic label="合并" :value="plan.summary.merge" />
          <n-statistic label="跳过" :value="plan.summary.skip" />
          <n-statistic label="脱敏跳过" :value="plan.summary.sensitive_skipped" />
        </div>

        <n-data-table
          v-if="plan"
          :columns="columns"
          :data="plan.items"
          :pagination="{ pageSize: 12 }"
          size="small"
          striped
        />
        <n-empty v-else description="选择文件并点击预览后显示新增、覆盖和跳过项目" />
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { computed, h, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NDataTable,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NRadio,
  NRadioGroup,
  NSelect,
  NSpace,
  NStatistic,
  NTag,
  NTooltip,
  NUpload,
  useDialog,
  useMessage,
  type DataTableColumns,
  type UploadFileInfo
} from 'naive-ui'
import {
  CloudDownloadOutline,
  CloudUploadOutline,
  DownloadOutline,
  EyeOutline
} from '@vicons/ionicons5'
import {
  backupApi,
  type BackupImportItem,
  type BackupImportPlan,
  type BackupPackage,
  type BackupPackageSummary
} from '@/api'

const message = useMessage()
const dialog = useDialog()

const includeSensitive = ref(false)
const exporting = ref(false)
const previewing = ref(false)
const importing = ref(false)
const sourceFormat = ref('auto')
const strategy = ref<'skip' | 'overwrite' | 'merge'>('skip')
const fileName = ref('')
const importData = ref<unknown | null>(null)
const plan = ref<BackupImportPlan | null>(null)
const lastExportSummary = ref<BackupPackageSummary | null>(null)

const sourceOptions = [
  { label: '自动识别', value: 'auto' },
  { label: 'Auto-RSS 备份包', value: 'auto-rss' },
  { label: 'Auto_Bangumi 订阅规则', value: 'auto-bangumi' }
]

const resourceLabels: Record<string, string> = {
  config: '系统配置',
  rss_source: 'RSS 源',
  group: '分组',
  tag: '标签',
  subscription: '订阅',
  subscription_tag: '订阅标签',
  notification_setting: '通知配置'
}

const actionMeta: Record<string, { label: string; type: 'success' | 'warning' | 'info' | 'default' }> = {
  create: { label: '新增', type: 'success' },
  overwrite: { label: '覆盖', type: 'warning' },
  merge: { label: '合并', type: 'info' },
  skip: { label: '跳过', type: 'default' }
}

const importableCount = computed(() => {
  if (!plan.value) return 0
  return plan.value.summary.create + plan.value.summary.overwrite + plan.value.summary.merge
})

const columns: DataTableColumns<BackupImportItem> = [
  {
    title: '类型',
    key: 'resource',
    width: 110,
    render(row) {
      return resourceLabels[row.resource] || row.resource
    }
  },
  {
    title: '名称',
    key: 'name',
    ellipsis: { tooltip: true },
    render(row) {
      return row.name || row.key
    }
  },
  {
    title: '动作',
    key: 'action',
    width: 110,
    render(row) {
      const meta = actionMeta[row.action] || actionMeta.skip
      return h(NTag, { type: meta.type, size: 'small' }, { default: () => meta.label })
    }
  },
  {
    title: '原因',
    key: 'reason',
    ellipsis: { tooltip: true }
  },
  {
    title: '标记',
    key: 'flags',
    width: 130,
    render(row) {
      const tags: ReturnType<typeof h>[] = []
      if (row.conflict) {
        tags.push(h(NTag, { type: 'warning', size: 'small' }, { default: () => '冲突' }))
      }
      if (row.sensitive) {
        tags.push(h(NTag, { type: 'error', size: 'small' }, { default: () => '敏感' }))
      }
      return h(NSpace, { size: 4 }, { default: () => tags })
    }
  }
]

const exportBackup = async () => {
  exporting.value = true
  try {
    const res: any = await backupApi.export(includeSensitive.value)
    const pkg = res.data as BackupPackage
    lastExportSummary.value = pkg.summary
    downloadJSON(pkg, `auto-rss-backup-${formatTimestamp(new Date())}.json`)
    message.success('备份已导出')
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '导出备份失败'
    message.error(errorMsg)
  } finally {
    exporting.value = false
  }
}

const handleFileChange = async ({ file }: { file: UploadFileInfo }) => {
  if (!file.file) {
    clearImportState()
    return
  }

  try {
    const text = await file.file.text()
    importData.value = JSON.parse(text)
    fileName.value = file.name
    plan.value = null
  } catch {
    clearImportState()
    message.error('备份文件不是有效的 JSON')
  }
}

const clearImportState = () => {
  fileName.value = ''
  importData.value = null
  plan.value = null
}

const previewImport = async () => {
  if (!importData.value) {
    message.warning('请先选择备份文件')
    return
  }

  previewing.value = true
  try {
    const res: any = await backupApi.preview(importData.value, sourceFormat.value, strategy.value)
    plan.value = res.data as BackupImportPlan
    message.success('导入预览已生成')
  } catch (error: any) {
    const errorMsg = error?.response?.data?.message || '预览导入失败'
    message.error(errorMsg)
  } finally {
    previewing.value = false
  }
}

const applyImport = () => {
  if (!importData.value || !plan.value) {
    return
  }

  dialog.warning({
    title: '确认导入',
    content: `将新增 ${plan.value.summary.create} 项，覆盖 ${plan.value.summary.overwrite} 项，合并 ${plan.value.summary.merge} 项。`,
    positiveText: '执行导入',
    negativeText: '取消',
    onPositiveClick: async () => {
      importing.value = true
      try {
        const res: any = await backupApi.import(importData.value, sourceFormat.value, strategy.value)
        plan.value = res.data as BackupImportPlan
        message.success('导入完成')
      } catch (error: any) {
        const errorMsg = error?.response?.data?.message || '导入失败'
        message.error(errorMsg)
      } finally {
        importing.value = false
      }
    }
  })
}

const downloadJSON = (data: unknown, filename: string) => {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

const formatTimestamp = (date: Date) => {
  const pad = (value: number) => String(value).padStart(2, '0')
  return [
    date.getFullYear(),
    pad(date.getMonth() + 1),
    pad(date.getDate())
  ].join('') + '-' + [
    pad(date.getHours()),
    pad(date.getMinutes()),
    pad(date.getSeconds())
  ].join('')
}
</script>

<style scoped>
.backup-page {
  max-width: 1180px;
}

.page-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.page-title {
  margin: 0;
  font-size: 20px;
}

.page-subtitle {
  margin: 6px 0 0;
  color: var(--n-text-color-3);
}

.top-alert {
  margin-bottom: 16px;
}

.summary-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 360px);
  gap: 16px;
  margin-bottom: 16px;
}

.panel-card {
  border-radius: 8px;
}

.import-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.plan-summary {
  display: grid;
  grid-template-columns: repeat(5, minmax(110px, 1fr));
  gap: 12px;
}

@media (max-width: 900px) {
  .summary-grid,
  .plan-summary {
    grid-template-columns: 1fr;
  }

  .page-heading,
  .import-toolbar {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
