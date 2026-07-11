import assert from 'node:assert/strict'
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
