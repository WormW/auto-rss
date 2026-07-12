import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  getEpisodeProgressPercent,
  getRelativeCurrentEpisode,
  getRelativeEpisode,
  getRelativeLatestEpisode,
  isEpisodeProgressComplete
} from '../src/utils/episodes.ts'

test('偏移订阅按季度内集数计算进度和完结状态', () => {
  const subscription = {
    current_episode: 221,
    latest_episode: 222,
    episode_offset: 170,
    total_episodes: 52
  }

  assert.equal(getRelativeCurrentEpisode(subscription), 51)
  assert.equal(getRelativeLatestEpisode(subscription), 52)
  assert.equal(getEpisodeProgressPercent(subscription), 98)
  assert.equal(isEpisodeProgressComplete(subscription), false)
})

test('偏移订阅达到原始第222集时完结', () => {
  const subscription = {
    current_episode: 222,
    latest_episode: 222,
    episode_offset: 170,
    total_episodes: 52
  }

  assert.equal(getEpisodeProgressPercent(subscription), 100)
  assert.equal(isEpisodeProgressComplete(subscription), true)
})

test('相对集数不会小于零且无偏移行为不变', () => {
  assert.equal(getRelativeEpisode(169, 170), 0)
  assert.equal(getRelativeEpisode(12, 0), 12)
  assert.equal(getRelativeEpisode(12, -5), 12)
})

test('剧集管理新增交互目标保持至少 40px 点击区域', () => {
  const drawer = readFileSync(
    new URL('../src/components/EpisodeManagerDrawer.vue', import.meta.url),
    'utf8'
  )

  assert.match(drawer, /<n-input-number[\s\S]*?class="manual-episode-number"[\s\S]*?button-placement="both"[\s\S]*?\/>/)
  assert.match(drawer, /<n-select[\s\S]*?class="candidate-select"[\s\S]*?\/>/)
  assert.equal((drawer.match(/class="error-retry-button"/g) || []).length, 2)

  const fortyPixelRules = [
    /\.episode-filter-tabs :deep\(\.n-tabs-tab\)\s*\{[^}]*min-height:\s*40px;/s,
    /\.manual-episode-number :deep\(\.n-input\)\s*\{[^}]*min-height:\s*40px;/s,
    /\.manual-episode-number :deep\(\.n-input__prefix > \.n-button\),[\s\S]*?\.manual-episode-number :deep\(\.n-input__suffix > \.n-button\)\s*\{[^}]*min-width:\s*40px;[^}]*min-height:\s*40px;/s,
    /\.manual-episode-input > :deep\(\.n-button\)\s*\{[^}]*min-height:\s*40px;/s,
    /\.candidate-select :deep\(\.n-base-selection\)\s*\{[^}]*min-height:\s*40px;/s,
    /\.selection-toolbar :deep\(\.n-checkbox\)\s*\{[^}]*min-height:\s*40px;/s,
    /\.error-retry-button\s*\{[^}]*min-width:\s*40px;[^}]*min-height:\s*40px;/s,
    /\.episode-pagination :deep\(\.n-pagination-item\)\s*\{[^}]*min-width:\s*40px;[^}]*min-height:\s*40px;/s,
    /\.candidate-load-more :deep\(\.n-button\)\s*\{[^}]*min-height:\s*40px;/s
  ]

  for (const rule of fortyPixelRules) {
    assert.match(drawer, rule)
  }
})

test('候选分页和任务轮询在刷新及切换 scope 时保持隔离', () => {
  const drawer = readFileSync(
    new URL('../src/components/EpisodeManagerDrawer.vue', import.meta.url),
    'utf8'
  )

  assert.match(drawer, /const candidateOffset = ref\(0\)/)
  assert.match(drawer, /const offset = append \? candidateOffset\.value : 0/)
  assert.match(drawer, /candidateOffset\.value = append[\s\S]*?candidateOffset\.value \+ page\.items\.length[\s\S]*?: page\.items\.length/)
  assert.match(drawer, /watch\(selectedCandidateScopeKey,[\s\S]*?candidateActionLoading\.value = ''/)
  assert.match(drawer, /if \(taskPollScopeKey === selectedCandidateScopeKey\.value\) return/)
  assert.match(drawer, /else if \(!task && selectedCandidate\.value && !candidateTaskIsRunning\(selectedCandidate\.value\)\)/)
  assert.match(drawer, /await episodeApi\.keepExisting[\s\S]*?emit\('changed'\)[\s\S]*?await Promise\.all/)
})

test('批量状态 mutation 在 drawer 和订阅 scope 间隔离', () => {
  const drawer = readFileSync(
    new URL('../src/components/EpisodeManagerDrawer.vue', import.meta.url),
    'utf8'
  )
  const applyStatus = drawer.slice(
    drawer.indexOf('async function applyStatus'),
    drawer.indexOf('async function openCandidates')
  )

  assert.match(drawer, /let episodeMutationGeneration = 0/)
  assert.match(drawer, /function invalidateEpisodeMutation\(\)/)
  assert.match(drawer, /已选择 \{\{ selectedEpisodeNumbers\.length \}\} \/ \{\{ MAX_EPISODE_SELECTION \}\} 集/)
  assert.match(applyStatus, /const subscriptionId = props\.subscription\?\.id/)
  assert.match(applyStatus, /const generation = \+\+episodeMutationGeneration/)
  assert.match(applyStatus, /episodeApi\.updateStatus\(subscriptionId,/)
  assert.match(applyStatus, /if \(!episodeMutationIsCurrent\(generation, subscriptionId\)\) return/)
  assert.match(applyStatus, /catch \(error: any\) \{\s*if \(episodeMutationIsCurrent\(generation, subscriptionId\)\)/)
  assert.match(applyStatus, /finally \{\s*if \(episodeMutationIsCurrent\(generation, subscriptionId\)\)/)
})

test('替换任务保留跨页候选关联并容忍 task 快照缺口', () => {
  const drawer = readFileSync(
    new URL('../src/components/EpisodeManagerDrawer.vue', import.meta.url),
    'utf8'
  )
  const startReplacement = drawer.slice(
    drawer.indexOf('async function startReplacement'),
    drawer.indexOf('async function retryCleanup')
  )
  const retryCleanup = drawer.slice(
    drawer.indexOf('async function retryCleanup'),
    drawer.indexOf('function candidateScope')
  )

  assert.doesNotMatch(startReplacement, /loadCandidates\(/)
  assert.doesNotMatch(retryCleanup, /loadCandidates\(/)
  assert.match(startReplacement, /updateSelectedCandidateTaskState\('replacing', 'queued'\)/)
  assert.match(retryCleanup, /updateSelectedCandidateTaskState\('accepted_cleanup_failed', 'cleanup_queued'\)/)
  assert.match(drawer, /candidatePageOffset:/)
  assert.match(drawer, /async function refreshCandidatePage/)
  assert.match(drawer, /const TASK_MISSING_REFRESH_THRESHOLD = 3/)
  assert.match(drawer, /const TASK_MISSING_FAILURE_THRESHOLD = 6/)
  assert.match(drawer, /taskMissingPollCount\+\+/)
  assert.match(drawer, /limit: CANDIDATE_REQUEST_LIMIT/)
  assert.match(drawer, /const CANDIDATE_REQUEST_LIMIT = CANDIDATE_PAGE_SIZE \+ 1/)
  assert.match(drawer, /<n-button :disabled="Boolean\(candidateActionLoading\)" @click="closeCandidateModal">关闭<\/n-button>/)
})
