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
    /\.error-retry-button\s*\{[^}]*min-width:\s*40px;[^}]*min-height:\s*40px;/s
  ]

  for (const rule of fortyPixelRules) {
    assert.match(drawer, rule)
  }
})
