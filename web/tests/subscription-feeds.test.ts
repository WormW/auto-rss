import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildFeedSavePlan,
  normalizeFeedURLForComparison
} from '../src/utils/subscription-feeds.ts'

test('不同偏移的两个 feed 保持为两个独立保存项', () => {
  const plan = buildFeedSavePlan(
    [{ id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 0, enabled: true }],
    [
      { id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 0, enabled: true },
      { name: 'B', rss_url: 'https://b.test/rss', episode_offset: 100, enabled: true }
    ]
  )

  assert.deepEqual(plan.create.map((item) => item.episode_offset), [100])
  assert.equal(plan.update.length, 0)
  assert.equal(plan.remove.length, 0)
})

test('URL 或偏移变化要求重新预览', () => {
  const plan = buildFeedSavePlan(
    [{ id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 0, enabled: true }],
    [{ id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 100, enabled: true }]
  )

  assert.equal(plan.update[0].requiresPreview, true)
})

test('比较 URL 时忽略 host 大小写和默认端口', () => {
  assert.equal(
    normalizeFeedURLForComparison('HTTPS://Example.COM:443/rss?b=2&a=1'),
    'https://example.com/rss?a=1&b=2'
  )
})
