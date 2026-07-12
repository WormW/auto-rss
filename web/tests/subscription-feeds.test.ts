import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import {
  buildFeedSavePlan,
  hasDuplicateFeedURLs,
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

test('同一规范化 URL 即使偏移不同也视为重复 feed', () => {
  assert.equal(hasDuplicateFeedURLs([
    { name: 'A', rss_url: 'HTTPS://Example.COM:443/rss', episode_offset: 0, enabled: true },
    { name: 'B', rss_url: 'https://example.com/rss', episode_offset: 100, enabled: true }
  ]), true)
})

test('feed 编辑器要求映射预览并提供可访问的行操作', () => {
  const editor = readFileSync(
    new URL('../src/components/SubscriptionFeedsEditor.vue', import.meta.url),
    'utf8'
  )

  assert.match(editor, /<n-data-table/)
  assert.match(editor, /original_episode/)
  assert.match(editor, /relative_episode/)
  assert.match(editor, /invalid_reason/)
  assert.match(editor, /previewValid/)
  assert.match(editor, /validation-change/)
  assert.match(editor, /['"]aria-label['"]:\s*['"]预览 feed['"]/)
  assert.match(editor, /['"]aria-label['"]:\s*['"]编辑 feed['"]/)
  assert.match(editor, /['"]aria-label['"]:\s*['"]删除 feed['"]/)
})

test('订阅表单用 feed 编辑器和保存计划替代单 RSS 偏移表单', () => {
  const view = readFileSync(
    new URL('../src/views/Subscriptions.vue', import.meta.url),
    'utf8'
  )

  assert.match(view, /<SubscriptionFeedsEditor/)
  assert.match(view, /v-model="feedDrafts"/)
  assert.match(view, /buildFeedSavePlan/)
  assert.match(view, /RSS × \{\{ getSubscriptionFeedCount\(sub\) \}\}/)
  assert.doesNotMatch(view, /handleOffsetEdit/)
  assert.doesNotMatch(view, /<n-form-item label="集数偏移">/)
})

test('候选资源比较展示 feed 来源和原始集数映射', () => {
  const drawer = readFileSync(
    new URL('../src/components/EpisodeManagerDrawer.vue', import.meta.url),
    'utf8'
  )

  assert.match(drawer, /source_feed_name/)
  assert.match(drawer, /source_fansub/)
  assert.match(drawer, /source_episode_offset/)
  assert.match(drawer, /原始集数/)
  assert.match(drawer, /相对集数/)
})
