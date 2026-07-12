import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { buildBangumiSubscriptionPatch } from '../src/utils/bangumi.ts'

test('Bangumi 条目映射到创建订阅表单', () => {
  const patch = buildBangumiSubscriptionPatch({
    id: 12345,
    type: 2,
    name: 'Original title',
    name_cn: '中文标题',
    summary: '简介',
    score: 8.6,
    total_episodes: 12,
    air_date: '2026-07-03',
    air_weekday: 5,
    season: 2,
    images: { large: 'https://example.test/cover.jpg' },
    rating: { rank: 88, score: 8.6 }
  })

  assert.deepEqual(patch, {
    name: '中文标题',
    bangumi_id: 12345,
    season: 2,
    total_episodes: 12,
    air_day: '5',
    update_day: '5'
  })
})

test('没有中文名或可用扩展字段时保留必要映射', () => {
  const patch = buildBangumiSubscriptionPatch({
    id: 7,
    type: 2,
    name: 'Fallback title',
    name_cn: '',
    summary: '',
    score: 0,
    total_episodes: 0,
    air_date: '',
    air_weekday: -1,
    season: 0
  })

  assert.deepEqual(patch, {
    name: 'Fallback title',
    bangumi_id: 7
  })
})

test('创建订阅表单提供按 Bangumi ID 获取信息入口', () => {
  const view = readFileSync(
    new URL('../src/views/Subscriptions.vue', import.meta.url),
    'utf8'
  )

  assert.match(view, /bangumiApi\.getSubject/)
  assert.match(view, /handleLookupBangumi/)
  assert.match(view, />\s*获取信息\s*</)
  assert.match(view, /bangumi-lookup-preview/)
})
