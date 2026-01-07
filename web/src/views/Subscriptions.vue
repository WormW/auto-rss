<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px">
      <h2>订阅管理</h2>
      <n-space>
        <n-button @click="showAnimeSearch">
          <template #icon>
            <n-icon><SearchOutlined /></n-icon>
          </template>
          搜索番剧
        </n-button>
        <n-button type="primary" @click="showAddDialog">
          添加订阅
        </n-button>
      </n-space>
    </n-space>

    <!-- 番剧搜索组件 -->
    <AnimeSearch ref="animeSearchRef" @subscribe="handleSearchSubscribe" />

    <!-- 订阅列表 - 按星期分组的卡片展示 -->
    <n-spin :show="loading">
      <div v-for="week in sortedWeekList" :key="week.day" style="margin-bottom: 24px">
        <div v-if="getActiveSubscriptionsByWeekday(week.day).length > 0">
          <h3 style="margin: 16px 0 12px 4px;">{{ week.label }}</h3>
          <div class="grid-container">
            <n-card
              v-for="sub in getActiveSubscriptionsByWeekday(week.day)"
              :key="sub.id"
              hoverable
              style="cursor: default;"
            >
              <div style="display: flex; gap: 12px;">
                <!-- 封面图 -->
                <div style="flex-shrink: 0;">
                  <img
                    v-if="sub.bangumi_cover_local"
                    :src="`http://localhost:7892/covers/${sub.bangumi_cover_local}`"
                    :alt="sub.name"
                    style="width: 92px; height: 130px; object-fit: cover; border-radius: 4px;"
                  />
                  <div
                    v-else
                    style="width: 92px; height: 130px; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); border-radius: 4px; display: flex; align-items: center; justify-content: center; color: white; font-size: 24px; font-weight: bold;"
                  >
                    {{ sub.name[0] }}
                  </div>
                </div>

                <!-- 内容区 -->
                <div style="flex: 1; min-width: 0; position: relative; padding-right: 80px;">
                  <!-- Bangumi评分 - 右上角 -->
                  <div v-if="sub.bangumi_score && sub.bangumi_score > 0" style="position: absolute; top: 0; right: 0; background: linear-gradient(135deg, #f6d365 0%, #fda085 100%); color: white; padding: 4px 10px; border-radius: 12px; font-size: 13px; font-weight: 600; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
                    ⭐ {{ sub.bangumi_score.toFixed(1) }}
                  </div>

                  <div style="padding-bottom: 50px;">
                    <!-- 标题 -->
                    <n-ellipsis style="max-width: 250px; font-weight: 500; font-size: 16px; margin-bottom: 8px;">
                      {{ sub.name }}
                    </n-ellipsis>

                    <!-- RSS 地址 -->
                    <n-ellipsis :line-clamp="2" style="font-size: 12px; color: var(--n-text-color-2); margin-bottom: 12px;">
                      {{ sub.rss_url }}
                    </n-ellipsis>

                    <!-- 标签组 -->
                    <n-space :size="4" :wrap="true" style="margin-bottom: 8px; max-width: 100%;">
                      <!-- Bangumi ID 标签 -->
                      <n-tooltip v-if="sub.bangumi_id" trigger="hover">
                        <template #trigger>
                          <n-tag
                            size="small"
                            style="cursor: pointer;"
                            @click="openBangumiPage(sub.bangumi_id)"
                          >
                            🔗 BGM:{{ sub.bangumi_id }}
                          </n-tag>
                        </template>
                        点击查看 Bangumi 页面
                      </n-tooltip>
                      <!-- 年份季度标签 -->
                      <n-tag v-if="sub.air_year" size="small" type="primary">
                        {{ getYearSeasonLabel(sub.air_year, sub.air_date) }}
                      </n-tag>
                      <n-tag size="small">第 {{ sub.season }} 季</n-tag>
                      <n-tag size="small" :type="sub.enabled ? 'success' : 'default'">
                        {{ sub.enabled ? '已启用' : '未启用' }}
                      </n-tag>
                      <n-tag size="small" type="info" v-if="sub.fansub">
                        {{ sub.fansub }}
                      </n-tag>
                      <n-tooltip trigger="hover" v-if="sub.current_episode || sub.total_episodes || sub.latest_episode">
                        <template #trigger>
                          <n-space :size="4">
                            <n-tag size="small" type="info" v-if="sub.latest_episode">
                              📺 更新至 {{ sub.latest_episode }} 集
                            </n-tag>
                            <n-tag
                              size="small"
                              :type="getCollectionTagType(sub)"
                            >
                              📂 收集 {{ sub.current_episode || 0 }} / {{ sub.total_episodes || '?' }}
                            </n-tag>
                          </n-space>
                        </template>
                        <div style="font-size: 13px; line-height: 1.6;">
                          <div v-if="sub.latest_episode">
                            <strong>📺 最新更新:</strong> 第 {{ sub.latest_episode }} 集
                          </div>
                          <div>
                            <strong>📂 已收集:</strong> {{ sub.current_episode || 0 }} 集
                          </div>
                          <div>
                            <strong>📊 总集数:</strong> {{ sub.total_episodes || '未知' }}
                          </div>
                          <div v-if="sub.downloading_count !== undefined && sub.downloading_count > 0" style="margin-top: 4px; color: #18a058;">
                            <strong>⬇️ 下载中:</strong> {{ sub.downloading_count }} 个资源
                          </div>
                          <div v-if="sub.latest_episode && sub.current_episode < sub.latest_episode" style="margin-top: 4px; color: #f0a020;">
                            ⚠️ 未收集最新 {{ sub.latest_episode - sub.current_episode }} 集
                          </div>
                          <div v-else-if="sub.latest_episode && sub.current_episode === sub.latest_episode" style="margin-top: 4px; color: #18a058;">
                            ✅ 已全部收集最新剧集
                          </div>
                        </div>
                      </n-tooltip>
                    </n-space>

                    <!-- 最后下载时间 -->
                    <div v-if="sub.last_download_at" style="font-size: 12px; color: var(--n-text-color-3);">
                      {{ formatTime(sub.last_download_at) }}
                    </div>
                  </div>

                  <!-- 操作区域 -->
                  <div style="position: absolute; right: 0; bottom: 0; display: flex; gap: 8px; align-items: flex-end;">
                    <!-- 控制按钮组 -->
                    <div style="display: flex; flex-direction: column; gap: 4px;">
                      <n-tooltip trigger="hover">
                        <template #trigger>
                          <n-button text @click="handleEnrichBangumi(sub.id)">
                            <template #icon>
                              <n-icon><ReloadOutlined /></n-icon>
                            </template>
                          </n-button>
                        </template>
                        补全番剧信息
                      </n-tooltip>
                      <n-tooltip trigger="hover">
                        <template #trigger>
                          <n-button text @click="handleCollectEpisodes(sub.id)">
                            <template #icon>
                              <n-icon><DownloadOutlined /></n-icon>
                            </template>
                          </n-button>
                        </template>
                        收集剧集
                      </n-tooltip>
                      <n-switch
                        :value="sub.enabled"
                        @update:value="(val) => handleToggle(sub.id, val)"
                        size="small"
                      />
                    </div>

                    <!-- 编辑删除按钮 -->
                    <div style="display: flex; flex-direction: column; gap: 4px;">
                      <n-button text @click="handleEdit(sub)">
                        <template #icon>
                          <n-icon><EditOutlined /></n-icon>
                        </template>
                      </n-button>
                      <n-button text type="error" @click="handleDelete(sub.id)">
                        <template #icon>
                          <n-icon><DeleteOutlined /></n-icon>
                        </template>
                      </n-button>
                    </div>
                  </div>
                </div>
              </div>
            </n-card>
          </div>
        </div>
      </div>

      <!-- 已完结番剧 -->
      <div v-if="completedSubscriptions.length > 0" style="margin-top: 32px;">
        <h3 style="margin: 16px 0 12px 4px;">已完结</h3>
        <div class="grid-container">
          <n-card
            v-for="sub in completedSubscriptions"
            :key="sub.id"
            hoverable
            style="cursor: default;"
          >
            <div style="display: flex; gap: 12px;">
              <!-- 封面图 -->
              <div style="flex-shrink: 0;">
                <img
                  v-if="sub.bangumi_cover_local"
                  :src="`http://localhost:7892/covers/${sub.bangumi_cover_local}`"
                  :alt="sub.name"
                  style="width: 92px; height: 130px; object-fit: cover; border-radius: 4px;"
                />
                <div
                  v-else
                  style="width: 92px; height: 130px; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); border-radius: 4px; display: flex; align-items: center; justify-content: center; color: white; font-size: 24px; font-weight: bold;"
                >
                  {{ sub.name[0] }}
                </div>
              </div>

              <!-- 内容区 -->
              <div style="flex: 1; min-width: 0; position: relative; padding-right: 80px;">
                <!-- Bangumi评分 - 右上角 -->
                <div v-if="sub.bangumi_score && sub.bangumi_score > 0" style="position: absolute; top: 0; right: 0; background: linear-gradient(135deg, #f6d365 0%, #fda085 100%); color: white; padding: 4px 10px; border-radius: 12px; font-size: 13px; font-weight: 600; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
                  ⭐ {{ sub.bangumi_score.toFixed(1) }}
                </div>

                <div style="padding-bottom: 50px;">
                  <!-- 标题 -->
                  <n-ellipsis style="max-width: 250px; font-weight: 500; font-size: 16px; margin-bottom: 8px;">
                    {{ sub.name }}
                  </n-ellipsis>

                  <!-- RSS 地址 -->
                  <n-ellipsis :line-clamp="2" style="font-size: 12px; color: var(--n-text-color-2); margin-bottom: 12px;">
                    {{ sub.rss_url }}
                  </n-ellipsis>

                  <!-- 标签组 -->
                  <n-space :size="4" :wrap="true" style="margin-bottom: 8px; max-width: 100%;">
                    <!-- 年份季度标签 -->
                    <n-tag v-if="sub.air_year" size="small" type="primary">
                      {{ getYearSeasonLabel(sub.air_year, sub.air_date) }}
                    </n-tag>
                    <n-tag size="small">第 {{ sub.season }} 季</n-tag>
                    <n-tag size="small" type="default">已完结</n-tag>
                    <n-tag size="small" type="info" v-if="sub.fansub">
                      {{ sub.fansub }}
                    </n-tag>
                    <n-tooltip trigger="hover" v-if="sub.current_episode || sub.total_episodes || sub.latest_episode">
                      <template #trigger>
                        <n-space :size="4">
                          <n-tag size="small" type="warning" v-if="sub.latest_episode">
                            📺 更新至 {{ sub.latest_episode }} 集
                          </n-tag>
                          <n-tag size="small" :type="sub.current_episode === sub.total_episodes && sub.total_episodes > 0 ? 'success' : 'info'">
                            📂 收集 {{ sub.current_episode || 0 }} / {{ sub.total_episodes || '?' }} 集
                          </n-tag>
                        </n-space>
                      </template>
                      <div>
                        <div v-if="sub.latest_episode">
                          <strong>📺 最新更新:</strong> 第 {{ sub.latest_episode }} 集
                        </div>
                        <div>
                          <strong>📂 已收集:</strong> {{ sub.current_episode || 0 }} 集
                        </div>
                        <div>
                          <strong>📊 总集数:</strong> {{ sub.total_episodes || '未知' }}
                        </div>
                        <div v-if="sub.current_episode < sub.total_episodes && sub.total_episodes > 0" style="margin-top: 4px; color: #d03050;">
                          ⚠️ 还缺 {{ sub.total_episodes - sub.current_episode }} 集
                        </div>
                      </div>
                    </n-tooltip>
                  </n-space>

                  <!-- 最后下载时间 -->
                  <div v-if="sub.last_download_at" style="font-size: 12px; color: var(--n-text-color-3);">
                    {{ formatTime(sub.last_download_at) }}
                  </div>
                </div>

                <!-- 操作区域 -->
                <div style="position: absolute; right: 0; bottom: 0; display: flex; gap: 8px; align-items: flex-end;">
                  <!-- 控制按钮组 -->
                  <div style="display: flex; flex-direction: column; gap: 4px;">
                    <n-tooltip trigger="hover">
                      <template #trigger>
                        <n-button text @click="handleEnrichBangumi(sub.id)">
                          <template #icon>
                            <n-icon><ReloadOutlined /></n-icon>
                          </template>
                        </n-button>
                      </template>
                      补全番剧信息
                    </n-tooltip>
                    <n-tooltip trigger="hover">
                      <template #trigger>
                        <n-button text @click="handleCollectEpisodes(sub.id)">
                          <template #icon>
                            <n-icon><DownloadOutlined /></n-icon>
                          </template>
                        </n-button>
                      </template>
                      收集剧集
                    </n-tooltip>
                    <n-switch
                      :value="sub.enabled"
                      @update:value="(val) => handleToggle(sub.id, val)"
                      size="small"
                    />
                  </div>

                  <!-- 编辑删除按钮 -->
                  <div style="display: flex; flex-direction: column; gap: 4px;">
                    <n-button text @click="handleEdit(sub)">
                      <template #icon>
                        <n-icon><EditOutlined /></n-icon>
                      </template>
                    </n-button>
                    <n-button text type="error" @click="handleDelete(sub.id)">
                      <template #icon>
                        <n-icon><DeleteOutlined /></n-icon>
                      </template>
                    </n-button>
                  </div>
                </div>
              </div>
            </div>
          </n-card>
        </div>
      </div>

      <!-- 空状态 -->
      <n-empty v-if="subscriptions.length === 0 && !loading" description="暂无订阅">
        <template #extra>
          <n-button size="small" @click="showAddDialog">添加第一个订阅</n-button>
        </template>
      </n-empty>
    </n-spin>

    <!-- 添加/编辑订阅对话框 - 两步式流程 -->
    <n-modal v-model:show="showModal" :mask-closable="!step2Loading">
      <n-card
        style="width: 600px;"
        :title="editingId ? '编辑订阅' : '添加订阅'"
        :bordered="false"
        size="huge"
        role="dialog"
        aria-modal="true"
      >
        <!-- 第一步: 选择RSS源或手动输入 -->
        <div v-if="showRssStep">
          <n-tabs v-model:value="activeTab" type="line">
            <!-- RSS源选择 -->
            <n-tab-pane name="rss_source" tab="从RSS源">
              <n-form label-width="80px">
                <n-form-item label="RSS 地址">
                  <n-input
                    v-model:value="formData.rss_url"
                    type="textarea"
                    :autosize="{ minRows: 2 }"
                    placeholder="https://mikanani.me/RSS/Bangumi?bangumiId=xxx"
                    :disabled="step2Loading"
                  />
                </n-form-item>
                <n-form-item>
                  <n-space justify="end" style="width: 100%;">
                    <n-button
                      text
                      type="primary"
                      @click="$router.push('/rss-sources')"
                      :disabled="step2Loading"
                    >
                      浏览 RSS 源
                    </n-button>
                  </n-space>
                </n-form-item>
              </n-form>
            </n-tab-pane>

            <!-- 手动输入 -->
            <n-tab-pane name="manual" tab="手动输入">
              <n-form label-width="80px">
                <n-form-item label="番剧名称">
                  <n-input
                    v-model:value="formData.name"
                    placeholder="请输入番剧名称"
                    :disabled="step2Loading"
                  />
                </n-form-item>
                <n-form-item label="RSS 地址">
                  <n-input
                    v-model:value="formData.rss_url"
                    type="textarea"
                    :autosize="{ minRows: 2 }"
                    placeholder="https://example.com/feed.xml"
                    :disabled="step2Loading"
                  />
                </n-form-item>
              </n-form>
            </n-tab-pane>
          </n-tabs>

          <n-space justify="end" style="margin-top: 16px;">
            <n-button @click="showModal = false" :disabled="step2Loading">取消</n-button>
            <n-button type="primary" @click="handleGetRssData" :loading="step2Loading">
              下一步
            </n-button>
          </n-space>
        </div>

        <!-- 第二步: 详细配置 -->
        <div v-else>
          <div style="max-height: 500px; overflow-y: auto; padding-right: 12px;">
            <n-form ref="formRef" :model="formData" label-width="100px" label-placement="left">
              <n-form-item label="番剧名称" path="name">
                <n-input v-model:value="formData.name" placeholder="请输入番剧名称" />
              </n-form-item>

              <n-form-item label="RSS 地址">
                <n-input
                  v-model:value="formData.rss_url"
                  type="textarea"
                  :autosize="{ minRows: 2 }"
                  placeholder="RSS 地址"
                />
              </n-form-item>

              <n-form-item label="字幕组">
                <n-input v-model:value="formData.fansub" placeholder="字幕组名称" />
              </n-form-item>

              <n-form-item label="字幕语言" v-if="formData.language">
                <n-tag type="info">{{ formData.language === 'CHS' ? '简体中文' : formData.language === 'CHT' ? '繁體中文' : formData.language }}</n-tag>
              </n-form-item>

              <n-form-item label="更新日期">
                <n-select
                  v-model:value="formData.update_day"
                  :options="weekdayOptions"
                  placeholder="选择更新日"
                />
              </n-form-item>

              <n-form-item label="季数">
                <n-input-number v-model:value="formData.season" :min="1" style="width: 100%;" />
              </n-form-item>

              <n-form-item label="Bangumi ID">
                <n-input-number
                  v-model:value="formData.bangumi_id"
                  :min="0"
                  :show-button="false"
                  style="width: 100%;"
                  placeholder="可选：手动指定 Bangumi 条目 ID"
                >
                  <template #suffix>
                    <n-button
                      text
                      size="small"
                      tag="a"
                      :href="formData.bangumi_id ? `https://bgm.tv/subject/${formData.bangumi_id}` : 'https://bgm.tv'"
                      target="_blank"
                      :disabled="!formData.bangumi_id"
                    >
                      查看
                    </n-button>
                  </template>
                </n-input-number>
              </n-form-item>

              <n-form-item label="总集数">
                <n-input-number v-model:value="formData.total_episodes" :min="0" style="width: 100%;" placeholder="0表示未知" />
              </n-form-item>

              <n-form-item label="集数偏移">
                <n-input-number v-model:value="formData.episode_offset" style="width: 100%;" />
              </n-form-item>

              <n-form-item label="下载路径">
                <n-input v-model:value="formData.download_path" placeholder="/downloads" />
              </n-form-item>

              <n-form-item label="过滤规则">
                <n-input
                  v-model:value="formData.filter_rules"
                  type="textarea"
                  :autosize="{ minRows: 2 }"
                  placeholder="留空则下载所有集数"
                />
              </n-form-item>

              <n-form-item label="启用">
                <n-switch v-model:value="formData.enabled" />
              </n-form-item>
            </n-form>
          </div>

          <n-space justify="space-between" style="margin-top: 16px;">
            <n-button @click="showRssStep = true">上一步</n-button>
            <n-space>
              <n-button @click="showModal = false">取消</n-button>
              <n-button type="primary" @click="handleSubmit" :loading="submitLoading">
                {{ editingId ? '更新' : '创建' }}
              </n-button>
            </n-space>
          </n-space>
        </div>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import {
  NButton,
  NCard,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSpace,
  NTabs,
  NTabPane,
  NTag,
  NEllipsis,
  NSelect,
  NSwitch,
  NSpin,
  NEmpty,
  NIcon,
  NTooltip,
  useMessage,
  useDialog
} from 'naive-ui'
import { subscriptionApi, type Subscription } from '@/api'
import { api } from '@/api'
import { useRoute, useRouter } from 'vue-router'
import { EditOutlined, DeleteOutlined, SearchOutlined, ReloadOutlined, DownloadOutlined } from '@vicons/antd'
import AnimeSearch from '@/components/AnimeSearch.vue'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const loading = ref(false)
const subscriptions = ref<Subscription[]>([])
const showModal = ref(false)
const showRssStep = ref(true)
const step2Loading = ref(false)
const submitLoading = ref(false)
const activeTab = ref('rss_source')
const editingId = ref<number | undefined>()
const animeSearchRef = ref<InstanceType<typeof AnimeSearch> | null>(null)

// 星期列表
const weekList = [
  { day: 0, label: '星期日' },
  { day: 1, label: '星期一' },
  { day: 2, label: '星期二' },
  { day: 3, label: '星期三' },
  { day: 4, label: '星期四' },
  { day: 5, label: '星期五' },
  { day: 6, label: '星期六' }
]

// 星期选项
const weekdayOptions = weekList.map(w => ({
  label: w.label,
  value: w.day.toString()
}))

// 按照当前星期排序的星期列表（从今天开始往后排列）
const sortedWeekList = computed(() => {
  const today = new Date().getDay() // 0-6, 0是星期日
  const sorted = []

  // 从今天开始往后排列
  for (let i = 0; i < 7; i++) {
    const targetDay = (today + i) % 7
    const weekItem = weekList.find(w => w.day === targetDay)
    if (weekItem) {
      sorted.push(weekItem)
    }
  }

  return sorted
})

// 表单数据
const formData = ref({
  name: '',
  rss_url: '',
  fansub: '',
  language: '',
  update_day: '',
  season: 1,
  bangumi_id: 0,
  total_episodes: 0,
  episode_offset: 0,
  download_path: '/downloads',
  filter_rules: '',
  enabled: true,
  rss_source_id: undefined as number | undefined,
  source_type: 'manual'
})

// 按星期分组获取连载中的订阅
const getActiveSubscriptionsByWeekday = (day: number) => {
  return subscriptions.value.filter(sub => {
    if (isCompleted(sub)) return false  // 已完结的不显示在这里
    if (!sub.update_day) return day === 0 // 未设置更新日的放在星期日
    return parseInt(sub.update_day) === day
  })
}

// 已完结的订阅
const completedSubscriptions = computed(() => {
  return subscriptions.value.filter(sub => isCompleted(sub))
})

// 格式化时间
const formatTime = (time: string) => {
  const date = new Date(time)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))

  if (days === 0) {
    const hours = Math.floor(diff / (1000 * 60 * 60))
    if (hours === 0) {
      const minutes = Math.floor(diff / (1000 * 60))
      return `${minutes} 分钟前`
    }
    return `${hours} 小时前`
  }
  if (days === 1) return '昨天'
  if (days < 7) return `${days} 天前`
  return date.toLocaleDateString()
}

// 生成年份+季度标签
const getYearSeasonLabel = (year: number, airDate?: string) => {
  let season = '未知'

  // 从 air_date 提取月份判断季度
  if (airDate && airDate.length >= 7) {
    const month = parseInt(airDate.substring(5, 7))
    if (month >= 1 && month <= 3) season = '冬'
    else if (month >= 4 && month <= 6) season = '春'
    else if (month >= 7 && month <= 9) season = '夏'
    else if (month >= 10 && month <= 12) season = '秋'
  }

  return `${year}${season}`
}

// 判断番剧是否已完结
const isCompleted = (sub: Subscription) => {
  // 没有总集数信息，无法判断
  if (!sub.total_episodes || sub.total_episodes <= 0) {
    return false
  }

  // 方案1：当前集数 >= 总集数（已采集过的订阅）
  if (sub.current_episode && sub.current_episode >= sub.total_episodes) {
    return true
  }

  // 方案2：RSS 最新集数 >= 总集数，且是老番（播出超过3个月）
  if (sub.latest_episode && sub.latest_episode >= sub.total_episodes && sub.air_date) {
    const airDate = new Date(sub.air_date)
    // 计算预计完结日期：播出日期 + 总集数周数 + 1个月缓冲
    const estimatedEndDate = new Date(airDate)
    estimatedEndDate.setDate(estimatedEndDate.getDate() + (sub.total_episodes * 7) + 30)

    if (new Date() > estimatedEndDate) {
      return true
    }
  }

  // 方案3：没有播出日期但 RSS 已有完整集数，且年份是过去的
  if (sub.latest_episode && sub.latest_episode >= sub.total_episodes && sub.air_year) {
    const currentYear = new Date().getFullYear()
    // 如果播出年份是去年或更早，认为已完结
    if (sub.air_year < currentYear) {
      return true
    }
  }

  return false
}

// 打开 Bangumi 页面
const openBangumiPage = (bangumiId: number) => {
  if (bangumiId) {
    window.open(`https://bgm.tv/subject/${bangumiId}`, '_blank')
  }
}

// 显示添加对话框
const showAddDialog = () => {
  editingId.value = undefined
  formData.value = {
    name: '',
    rss_url: '',
    fansub: '',
    language: '',
    update_day: '',
    season: 1,
    bangumi_id: 0,
    total_episodes: 0,
    episode_offset: 0,
    download_path: '/downloads',
    filter_rules: '',
    enabled: true,
    rss_source_id: undefined,
    source_type: 'manual'
  }
  activeTab.value = 'rss_source'
  showRssStep.value = true
  showModal.value = true
}

// 显示番剧搜索
const showAnimeSearch = () => {
  animeSearchRef.value?.show()
}

// 处理搜索订阅
const handleSearchSubscribe = (data: {
  title: string
  rss_url: string
  fansub: string
  language?: string
  rss_source_id?: number
}) => {
  formData.value = {
    ...formData.value,
    name: data.title,
    rss_url: data.rss_url,
    fansub: data.fansub,
    language: data.language || '',
    rss_source_id: data.rss_source_id,
    source_type: data.rss_source_id ? 'rss_source' : 'manual'
  }
  showRssStep.value = false
  showModal.value = true
}

// 处理编辑
const handleEdit = (sub: Subscription) => {
  editingId.value = sub.id
  formData.value = {
    name: sub.name,
    rss_url: sub.rss_url,
    fansub: sub.fansub || '',
    language: sub.language || '',
    update_day: sub.update_day || '',
    season: sub.season,
    bangumi_id: sub.bangumi_id || 0,
    total_episodes: sub.total_episodes || 0,
    episode_offset: sub.episode_offset || 0,
    download_path: sub.download_path,
    filter_rules: sub.filter_rules || '',
    enabled: sub.enabled !== false,
    rss_source_id: sub.rss_source_id,
    source_type: sub.source_type || 'manual'
  }
  showRssStep.value = false // 编辑时直接显示详细配置
  showModal.value = true
}

// 获取RSS数据 (第一步到第二步的过渡)
const handleGetRssData = async () => {
  if (!formData.value.rss_url) {
    message.error('请输入 RSS 地址')
    return
  }

  // 如果是手动输入模式且没有填写名称,提示用户
  if (activeTab.value === 'manual' && !formData.value.name) {
    message.error('请输入番剧名称')
    return
  }

  step2Loading.value = true
  try {
    // 这里可以调用API解析RSS获取番剧信息
    // 暂时直接进入下一步
    showRssStep.value = false
  } finally {
    step2Loading.value = false
  }
}

// 提交表单
const handleSubmit = async () => {
  if (!formData.value.name || !formData.value.rss_url) {
    message.error('请填写必填项')
    return
  }

  submitLoading.value = true
  try {
    if (editingId.value) {
      await subscriptionApi.update(editingId.value, formData.value)
      message.success('更新成功')
    } else {
      await subscriptionApi.create(formData.value)
      message.success('创建成功')
    }
    showModal.value = false
    loadSubscriptions()
  } catch (error: any) {
    message.error(error.message || '操作失败')
  } finally {
    submitLoading.value = false
  }
}

// 加载订阅列表
const loadSubscriptions = async () => {
  loading.value = true
  try {
    const res: any = await subscriptionApi.list(1, 999) // 获取所有订阅
    subscriptions.value = res.data?.list || []
  } catch (error: any) {
    message.error(error.message || '加载订阅列表失败')
  } finally {
    loading.value = false
  }
}

// 删除订阅
const handleDelete = (id: number) => {
  dialog.warning({
    title: '确认删除',
    content: '确定要删除这个订阅吗?',
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await subscriptionApi.delete(id)
        message.success('删除成功')
        loadSubscriptions()
      } catch (error: any) {
        message.error(error.message || '删除失败')
      }
    }
  })
}

// 切换订阅启用状态
const handleToggle = async (id: number, enabled: boolean) => {
  try {
    const response: any = await api.post(`/subscriptions/${id}/toggle`)
    if (response.code === 0) {
      message.success(enabled ? '已启用' : '已禁用')
      loadSubscriptions()
    } else {
      message.error(response.message || '操作失败')
    }
  } catch (error: any) {
    message.error(error.message || '操作失败')
  }
}

// 手动补全Bangumi数据
const handleEnrichBangumi = async (id: number) => {
  try {
    message.loading('正在补全番剧信息...', { duration: 0 })
    const response: any = await api.post(`/subscriptions/${id}/enrich-bangumi`)
    message.destroyAll()

    if (response.code === 0) {
      message.success('补全成功')
      loadSubscriptions()
    } else {
      message.error(response.message || '补全失败')
    }
  } catch (error: any) {
    message.destroyAll()
    message.error(error.message || '补全失败')
  }
}

// 获取收集状态的标签类型
const getCollectionTagType = (sub: Subscription): string => {
  // 如果有最新集数信息
  if (sub.latest_episode) {
    if (sub.current_episode === sub.latest_episode) {
      return 'success' // 已全部收集最新剧集
    }
    return 'warning' // 有未收集的最新剧集
  }

  // 如果有总集数信息
  if (sub.total_episodes && sub.total_episodes > 0) {
    if (sub.current_episode >= sub.total_episodes) {
      return 'success' // 已完成收集
    }
    return 'info' // 收集中
  }

  return 'default' // 没有集数信息
}

// 手动收集剧集
const handleCollectEpisodes = async (id: number) => {
  try {
    const response: any = await api.post(`/subscriptions/${id}/collect-episodes`)

    if (response.code === 0) {
      message.success('采集任务已启动，请在右上角任务管理中查看进度')
    } else if (response.code === 409) {
      message.warning(response.message || '已有任务在执行中')
    } else {
      message.error(response.message || '启动采集任务失败')
    }
  } catch (error: any) {
    if (error.response?.status === 409) {
      message.warning(error.response?.data?.message || '已有任务在执行中')
    } else {
      message.error(error.response?.data?.message || error.message || '启动采集任务失败')
    }
  }
}

// 处理从RSS源页面跳转过来的情况
onMounted(() => {
  if (route.query.from_rss === 'true') {
    showModal.value = true
    showRssStep.value = false // 直接进入第二步
    formData.value = {
      name: (route.query.name as string) || '',
      rss_url: (route.query.rss_url as string) || '',
      fansub: (route.query.fansub as string) || '',
      language: '',
      update_day: '',
      season: 1,
      total_episodes: 0,
      episode_offset: 0,
      download_path: '/downloads',
      filter_rules: '',
      enabled: true,
      rss_source_id: route.query.rss_source_id ? parseInt(route.query.rss_source_id as string) : undefined,
      source_type: 'rss_source'
    }
  }
  loadSubscriptions()
})
</script>

<style scoped>
.grid-container {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(380px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}

@media (max-width: 768px) {
  .grid-container {
    grid-template-columns: 1fr;
  }
}
</style>
