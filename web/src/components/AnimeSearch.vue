<template>
  <n-modal v-model:show="visible" style="width: 800px;" preset="card" title="浏览番剧" :bordered="false">
    <!-- 搜索栏 -->
    <n-space vertical>
      <n-input
        v-model:value="searchText"
        placeholder="请输入番剧名称搜索"
        clearable
        @keyup.enter="handleSearch"
      >
        <template #prefix>
          <n-icon><SearchOutlined /></n-icon>
        </template>
        <template #suffix>
          <n-button text type="primary" @click="handleSearch" :loading="searching">
            搜索
          </n-button>
        </template>
      </n-input>

      <!-- 季度选择 -->
      <n-space v-if="seasons.length > 0">
        <n-select
          v-model:value="selectedSeason"
          :options="seasonOptions"
          placeholder="选择季度"
          style="width: 200px;"
          @update:value="handleSeasonChange"
        />
      </n-space>
    </n-space>

    <!-- 番剧列表 -->
    <n-spin :show="loading" style="margin-top: 16px;">
      <div style="max-height: 600px; overflow-y: auto;">
        <n-collapse v-if="animeList.length > 0" @update:expanded-names="handleCollapseChange">
          <n-collapse-item
            v-for="anime in animeList"
            :key="anime.title"
            :title="anime.title"
            :name="anime.title"
          >
            <template #header-extra>
              <n-space :size="4">
                <n-tag v-if="anime.score" type="error" size="small">
                  {{ anime.score.toFixed(1) }}
                </n-tag>
                <n-tag v-if="anime.exists" type="success" size="small">
                  已订阅
                </n-tag>
              </n-space>
            </template>

            <!-- 字幕组列表 -->
            <n-spin :show="loadingGroups[anime.url]">
              <div v-if="fansubGroups[anime.url]">
                <n-space vertical :size="8">
                  <n-card
                    v-for="group in fansubGroups[anime.url]"
                    :key="group.rss"
                    size="small"
                    hoverable
                  >
                    <template #header>
                      <n-space justify="space-between" align="center" style="width: 100%;">
                        <div>
                          <n-text strong>{{ group.name }}</n-text>
                          <n-text depth="3" style="margin-left: 8px;" v-if="group.update_day">
                            {{ group.update_day }}
                          </n-text>
                        </div>
                      </n-space>
                    </template>

                    <!-- 标签和语言选择 -->
                    <div style="margin-bottom: 12px;">
                      <n-space :size="4" v-if="group.tags && group.tags.length > 0">
                        <n-tag
                          v-for="(tag, idx) in group.tags.slice(0, 5)"
                          :key="idx"
                          size="small"
                          type="info"
                        >
                          {{ tag }}
                        </n-tag>
                      </n-space>
                    </div>

                    <!-- 语言选择区 -->
                    <div v-if="getLanguageOptions(group.tags).length > 0" style="margin-bottom: 12px;">
                      <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 8px;">
                        选择字幕语言:
                      </n-text>
                      <n-space :size="8">
                        <n-button
                          v-for="lang in getLanguageOptions(group.tags)"
                          :key="lang.value"
                          size="small"
                          :type="selectedLanguage[`${anime.url}_${group.rss}`] === lang.value ? 'primary' : 'default'"
                          @click="selectLanguage(anime.url, group.rss, lang.value)"
                        >
                          {{ lang.label }}
                        </n-button>
                      </n-space>
                    </div>

                    <!-- 最新集数预览 -->
                    <div v-if="group.episodes && group.episodes.length > 0" style="margin-bottom: 12px;">
                      <n-text depth="3" style="font-size: 12px;">
                        最新集数: {{ group.episodes.slice(0, 3).join(', ') }}
                        <span v-if="group.episodes.length > 3">...</span>
                      </n-text>
                    </div>

                    <!-- 订阅按钮 -->
                    <n-space justify="end">
                      <n-button
                        size="small"
                        type="primary"
                        :disabled="getLanguageOptions(group.tags).length > 0 && !selectedLanguage[`${anime.url}_${group.rss}`]"
                        @click="handleSubscribe(anime, group)"
                      >
                        订阅
                      </n-button>
                    </n-space>
                  </n-card>
                </n-space>
              </div>
              <n-empty v-else description="暂无字幕组" size="small" />
            </n-spin>
          </n-collapse-item>
        </n-collapse>

        <n-empty v-else-if="!loading" description="暂无结果">
          <template #extra>
            <n-button size="small" @click="handleSearch">重新搜索</n-button>
          </template>
        </n-empty>
      </div>
    </n-spin>
  </n-modal>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  NModal,
  NInput,
  NButton,
  NSpace,
  NSelect,
  NCollapse,
  NCollapseItem,
  NCard,
  NTag,
  NText,
  NSpin,
  NEmpty,
  NIcon,
  useMessage
} from 'naive-ui'
import { SearchOutlined } from '@vicons/antd'
import { mikanApi, type MikanAnimeItem, type MikanSearchResult } from '@/api'

const emit = defineEmits<{
  subscribe: [data: {
    title: string
    rss_url: string
    fansub: string
    language?: string
    rss_source_id?: number
  }]
}>()

const message = useMessage()
const visible = ref(false)
const loading = ref(false)
const searching = ref(false)
const searchText = ref('')
const selectedSeason = ref<string>('')
const seasons = ref<Array<{ year: number; season: string }>>([])
const animeList = ref<MikanAnimeItem[]>([])
const fansubGroups = ref<Record<string, Array<any>>>({})
const loadingGroups = ref<Record<string, boolean>>({})
const selectedLanguage = ref<Record<string, string>>({})

// 季度选项
const seasonOptions = computed(() => {
  return seasons.value.map(s => ({
    label: `${s.year} ${s.season}`,
    value: `${s.year}_${s.season}`
  }))
})

// 显示对话框
const show = (sourceId?: number, searchQuery?: string) => {
  visible.value = true
  searchText.value = searchQuery || ''
  animeList.value = []
  fansubGroups.value = {}
  seasons.value = []
  selectedSeason.value = ''

  // 只有传入搜索关键词时才自动搜索，否则等待用户点击搜索按钮
  if (searchQuery) {
    handleSearch()
  }
}

// 加载当前季度
const loadCurrentSeason = async () => {
  loading.value = true
  try {
    const currentYear = new Date().getFullYear()
    const currentMonth = new Date().getMonth() + 1

    // 根据月份判断季度
    let season = '春季'
    if (currentMonth >= 1 && currentMonth <= 3) {
      season = '冬季'
    } else if (currentMonth >= 4 && currentMonth <= 6) {
      season = '春季'
    } else if (currentMonth >= 7 && currentMonth <= 9) {
      season = '夏季'
    } else {
      season = '秋季'
    }

    // 获取当前季度番剧
    const result: any = await mikanApi.getBySeason(currentYear, season)
    processSearchResult(result.data)
  } catch (error: any) {
    message.error(error.message || '加载失败')
  } finally {
    loading.value = false
  }
}

// 搜索番剧
const handleSearch = async () => {
  if (!searchText.value || searchText.value.length < 2) {
    message.warning('请输入至少2个字符')
    return
  }

  searching.value = true
  loading.value = true
  try {
    const result: any = await mikanApi.search(searchText.value)
    processSearchResult(result.data)
  } catch (error: any) {
    message.error(error.message || '搜索失败')
  } finally {
    searching.value = false
    loading.value = false
  }
}

// 处理搜索结果
const processSearchResult = (data: MikanSearchResult) => {
  // 设置季度列表
  if (data.seasons && data.seasons.length > 0) {
    seasons.value = data.seasons
  }

  // 展开所有番剧到单一列表
  animeList.value = []
  if (data.groups && data.groups.length > 0) {
    data.groups.forEach(group => {
      if (group.items && group.items.length > 0) {
        animeList.value.push(...group.items)
      }
    })
  }
}

// 季度变化
const handleSeasonChange = async () => {
  if (!selectedSeason.value) return

  const [yearStr, season] = selectedSeason.value.split('_')
  const year = parseInt(yearStr)

  loading.value = true
  try {
    const result: any = await mikanApi.getBySeason(year, season)
    processSearchResult(result.data)
  } catch (error: any) {
    message.error(error.message || '加载失败')
  } finally {
    loading.value = false
  }
}

// 加载字幕组
const loadFansubGroups = async (anime: MikanAnimeItem) => {
  if (fansubGroups.value[anime.url]) {
    console.log('Already cached, skipping')
    return // 已加载过
  }

  console.log('Starting to load fansub groups for:', anime.url)
  loadingGroups.value[anime.url] = true
  try {
    console.log('Calling API...')
    const result: any = await mikanApi.getFansubGroups(anime.url)
    console.log('API response:', result)
    fansubGroups.value[anime.url] = result.data || []
    console.log('Stored groups:', fansubGroups.value[anime.url])
  } catch (error: any) {
    console.error('Error loading fansub groups:', error)
    message.error(error.message || '加载字幕组失败')
  } finally {
    loadingGroups.value[anime.url] = false
    console.log('Loading complete, loading state:', loadingGroups.value[anime.url])
  }
}

// 获取语言选项
const getLanguageOptions = (tags: string[]) => {
  const languages: Array<{ label: string; value: string }> = []

  if (!tags || tags.length === 0) return languages

  // 检测简体中文
  if (tags.some(tag => tag === '简体' || tag === '简中' || tag === 'CHS')) {
    languages.push({ label: '简体中文', value: 'CHS' })
  }

  // 检测繁体中文
  if (tags.some(tag => tag === '繁体' || tag === '繁中' || tag === 'CHT')) {
    languages.push({ label: '繁體中文', value: 'CHT' })
  }

  return languages
}

// 选择语言
const selectLanguage = (animeUrl: string, groupRss: string, language: string) => {
  const key = `${animeUrl}_${groupRss}`
  selectedLanguage.value[key] = language
}

// 订阅番剧
const handleSubscribe = (anime: MikanAnimeItem, group: any) => {
  const key = `${anime.url}_${group.rss}`
  const language = selectedLanguage.value[key]

  // 如果有语言选项但未选择,提示用户
  const languageOptions = getLanguageOptions(group.tags || [])
  if (languageOptions.length > 0 && !language) {
    message.warning('请先选择字幕语言')
    return
  }

  emit('subscribe', {
    title: anime.title,
    rss_url: group.rss,
    fansub: group.name,
    language: language,
    rss_source_id: undefined
  })
  visible.value = false
  message.success('已添加到订阅表单')
}

// 监听折叠面板展开
const expandedNames = ref<(string | number)[]>([])

const handleCollapseChange = async (names: (string | number)[]) => {
  console.log('Collapse expanded names:', names)

  // 找出新展开的项
  const newExpanded = names.filter(name => !expandedNames.value.includes(name))
  console.log('Newly expanded:', newExpanded)

  // 更新展开状态
  expandedNames.value = names

  // 为每个新展开的项加载字幕组
  for (const name of newExpanded) {
    const anime = animeList.value.find(a => a.title === name)
    console.log('Found anime for:', name, anime)
    if (anime) {
      console.log('Anime URL:', anime.url)
      console.log('Already loaded:', !!fansubGroups.value[anime.url])
      if (!fansubGroups.value[anime.url]) {
        console.log('Loading fansub groups...')
        await loadFansubGroups(anime)
      }
    }
  }
}

defineExpose({ show })
</script>

<style scoped>
.n-collapse-item :deep(.n-collapse-item__header) {
  font-weight: 500;
}
</style>
