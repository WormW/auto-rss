<template>
  <div class="calendar-page">
    <h2 class="page-title">追番日历</h2>

    <n-card class="calendar-card">
      <n-tabs v-model:value="activeTab" type="line">
        <n-tab-pane name="current" tab="本周">
          <div class="week-grid">
            <div
              v-for="day in currentWeekDays"
              :key="day.day"
              class="day-column"
              :class="{ 'is-today': day.is_today }"
            >
              <div class="day-header">
                <span class="day-label">{{ day.day_cn }}</span>
                <n-tag v-if="day.is_today" type="success" size="small">今天</n-tag>
              </div>
              <div class="day-content">
                <n-empty v-if="day.items.length === 0" description="无更新" />
                <div
                  v-for="item in day.items"
                  :key="item.subscription_id"
                  class="anime-item"
                  :class="{ 'is-downloaded': item.is_downloaded }"
                  @click="goToSubscription(item.subscription_id)"
                >
                  <div class="anime-cover">
                    <img
                      v-if="item.cover"
                      :src="`/covers/${item.cover}`"
                      :alt="item.name"
                    />
                    <div v-else class="cover-placeholder">
                      {{ item.name[0] }}
                    </div>
                  </div>
                  <div class="anime-info">
                    <div class="anime-name">{{ item.name }}</div>
                    <div class="anime-meta">
                      <span class="air-time">{{ item.air_time || '待定' }}</span>
                      <span class="episode">第 {{ item.episode }} 集</span>
                      <n-tag
                        v-if="item.is_downloaded"
                        type="success"
                        size="tiny"
                      >已下载</n-tag>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </n-tab-pane>

        <n-tab-pane name="today" tab="今日">
          <div class="today-section">
            <div v-if="todayItems.length > 0" class="today-list">
              <div
                v-for="item in todayItems"
                :key="item.subscription_id"
                class="today-item"
                @click="goToSubscription(item.subscription_id)"
              >
                <div class="today-cover">
                  <img
                    v-if="item.cover"
                    :src="`/covers/${item.cover}`"
                    :alt="item.name"
                  />
                  <div v-else class="cover-placeholder">
                    {{ item.name[0] }}
                  </div>
                </div>
                <div class="today-info">
                  <div class="today-name">{{ item.name }}</div>
                  <div class="today-meta">
                    <span class="air-time">{{ item.air_time || '待定' }}</span>
                    <span class="episode">第 {{ item.episode }} 集</span>
                    <span class="progress">
                      进度: {{ item.current_episode }} / {{ item.total_episodes || '?' }}
                    </span>
                    <n-tag
                      v-if="item.is_downloaded"
                      type="success"
                      size="small"
                    >已下载</n-tag>
                    <n-tag
                      v-else
                      type="warning"
                      size="small"
                    >待更新</n-tag>
                  </div>
                </div>
              </div>
            </div>
            <n-empty v-else description="今日没有番剧更新" />
          </div>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <!-- 统计信息 -->
    <n-card title="本周统计" size="small" class="stats-card">
      <n-grid :cols="4" :x-gap="16">
        <n-gi>
          <n-statistic label="更新总数" :value="weekStats.total" />
        </n-gi>
        <n-gi>
          <n-statistic label="已下载" :value="weekStats.downloaded" />
        </n-gi>
        <n-gi>
          <n-statistic label="待更新" :value="weekStats.pending" />
        </n-gi>
        <n-gi>
          <n-statistic label="完结番剧" :value="completedCount" />
        </n-gi>
      </n-grid>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import {
  NCard,
  NTabs,
  NTabPane,
  NTag,
  NEmpty,
  NGrid,
  NGi,
  NStatistic,
  useMessage
} from 'naive-ui'
import { calendarApi, type WeekSchedule, type CalendarItem } from '@/api'

const router = useRouter()
const message = useMessage()

const activeTab = ref('current')
const currentWeek = ref<WeekSchedule | null>(null)
const todayItems = ref<CalendarItem[]>([])
const completedCount = ref(0)

// 当前星期的每一天
const currentWeekDays = computed(() => {
  return currentWeek.value?.days || []
})

// 本周统计
const weekStats = computed(() => {
  const days = currentWeek.value?.days || []
  let total = 0
  let downloaded = 0

  days.forEach(day => {
    day.items.forEach(item => {
      total++
      if (item.is_downloaded) downloaded++
    })
  })

  return {
    total,
    downloaded,
    pending: total - downloaded
  }
})

// 加载本周日历
const loadWeekSchedule = async () => {
  try {
    const res: any = await calendarApi.getWeekSchedule(0)
    currentWeek.value = res.data
  } catch (error: any) {
    message.error(error.message || '加载日历失败')
  }
}

// 加载今日更新
const loadTodaySchedule = async () => {
  try {
    const res: any = await calendarApi.getTodaySchedule()
    todayItems.value = res.data || []
  } catch (error: any) {
    message.error(error.message || '加载今日更新失败')
  }
}

// 跳转到订阅详情
const goToSubscription = (id: number) => {
  router.push(`/subscriptions?id=${id}`)
}

onMounted(() => {
  loadWeekSchedule()
  loadTodaySchedule()
})
</script>

<style scoped>
.calendar-page {
  max-width: 1400px;
  margin: 0 auto;
}

.page-title {
  margin-bottom: 20px;
}

.calendar-card {
  margin-bottom: 20px;
}

.week-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 12px;
  min-height: 400px;
}

.day-column {
  background: var(--n-color-embedded);
  border-radius: 8px;
  padding: 12px;
  min-height: 300px;
}

.day-column.is-today {
  background: var(--n-color-success-deprecated-bg);
  border: 2px solid var(--n-color-success);
}

.day-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--n-divider-color);
}

.day-label {
  font-weight: 600;
  font-size: 14px;
}

.day-content {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.anime-item {
  display: flex;
  gap: 8px;
  padding: 8px;
  background: var(--n-color-card);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.anime-item:hover {
  background: var(--n-color-hover);
  transform: translateY(-2px);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.anime-item.is-downloaded {
  opacity: 0.7;
}

.anime-cover {
  width: 40px;
  height: 56px;
  border-radius: 4px;
  overflow: hidden;
  flex-shrink: 0;
}

.anime-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cover-placeholder {
  width: 100%;
  height: 100%;
  background: var(--n-color-embedded);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: var(--n-text-color-3);
}

.anime-info {
  flex: 1;
  min-width: 0;
}

.anime-name {
  font-size: 13px;
  font-weight: 500;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.anime-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  font-size: 11px;
  color: var(--n-text-color-3);
}

.air-time {
  color: var(--n-color-primary);
}

/* 今日更新样式 */
.today-section {
  padding: 20px;
}

.today-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.today-item {
  display: flex;
  gap: 16px;
  padding: 16px;
  background: var(--n-color-embedded);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
}

.today-item:hover {
  background: var(--n-color-hover);
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}

.today-cover {
  width: 80px;
  height: 112px;
  border-radius: 6px;
  overflow: hidden;
  flex-shrink: 0;
}

.today-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.today-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
}

.today-name {
  font-size: 18px;
  font-weight: 600;
  margin-bottom: 12px;
}

.today-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  font-size: 14px;
  color: var(--n-text-color-2);
}

.stats-card {
  margin-top: 20px;
}

/* 响应式 */
@media (max-width: 1200px) {
  .week-grid {
    grid-template-columns: repeat(4, 1fr);
  }
}

@media (max-width: 768px) {
  .week-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 480px) {
  .week-grid {
    grid-template-columns: 1fr;
  }
}
</style>
