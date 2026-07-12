import assert from 'node:assert/strict'
import test from 'node:test'

import type { EpisodeResourceCandidate, SubscriptionEpisode } from '../src/api/episode.ts'
import {
  appendEpisodeSelection,
  appendUniqueById,
  candidateAvailableActions,
  canRestoreMissing,
  continuousOwnedEpisode,
  describeCandidateDifference,
  episodeStatusLabel,
  episodeStatusType,
  filterEpisodes,
  normalizedValuesDiffer,
  paginateItems,
  planEpisodeStatusUpdate,
  isEpisodeOwned,
  safeExternalURL,
  takeLookaheadPage
} from '../src/utils/episode-ledger.ts'

const episode = (overrides: Partial<SubscriptionEpisode> = {}): SubscriptionEpisode => ({
  id: 1,
  subscription_id: 2,
  episode: 3,
  status: 'downloaded',
  active_download_id: null,
  active_torrent_hash: '',
  active_torrent_url: '',
  active_title: '',
  status_source: 'automatic',
  downloaded_at: null,
  created_at: '2026-07-12T00:00:00Z',
  updated_at: '2026-07-12T00:00:00Z',
  action_required_candidate_count: 0,
  ...overrides
})

const candidate = (
  overrides: Partial<EpisodeResourceCandidate> = {}
): EpisodeResourceCandidate => ({
  id: 4,
  subscription_episode_id: 1,
  resource_key: 'hash:new-hash',
  torrent_hash: 'new-hash',
  torrent_url: 'https://example.com/new.torrent',
  title: '新资源标题',
  fansub: '',
  language: '',
  status: 'pending',
  pub_time: null,
  source_rss_url: '',
  failure_reason: '',
  staged_path: '',
  old_resource_path: '',
  rollback_path: '',
  final_path: '',
  replacement_stage: '',
  replacement_download_id: null,
  old_download_id: null,
  old_torrent_hash: '',
  created_at: '2026-07-12T00:00:00Z',
  updated_at: '2026-07-12T00:00:00Z',
  ...overrides
})

test('剧集状态提供稳定的中文文案和 Naive UI 标签类型', () => {
  assert.equal(episodeStatusLabel('missing'), '缺失')
  assert.equal(episodeStatusLabel('downloading'), '下载中')
  assert.equal(episodeStatusLabel('downloaded'), '已下载')
  assert.equal(episodeStatusLabel('marked_downloaded'), '已标记下载')
  assert.equal(episodeStatusLabel('ignored'), '已忽略')

  assert.equal(episodeStatusType('missing'), 'error')
  assert.equal(episodeStatusType('downloading'), 'warning')
  assert.equal(episodeStatusType('downloaded'), 'success')
  assert.equal(episodeStatusType('marked_downloaded'), 'info')
  assert.equal(episodeStatusType('ignored'), 'default')
})

test('已下载、手动标记和忽略状态均视为已处理', () => {
  assert.equal(isEpisodeOwned('downloaded'), true)
  assert.equal(isEpisodeOwned('marked_downloaded'), true)
  assert.equal(isEpisodeOwned('ignored'), true)
  assert.equal(isEpisodeOwned('downloading'), false)
  assert.equal(isEpisodeOwned('missing'), false)
})

test('活动下载任务必须先处理才能恢复缺失', () => {
  assert.equal(canRestoreMissing(episode({ status: 'downloading', active_download_id: 8 })), false)
  assert.equal(canRestoreMissing(episode({ status: 'downloading', active_download_id: null })), true)
  assert.equal(canRestoreMissing(episode({ status: 'downloaded', active_download_id: 8 })), true)
  assert.equal(canRestoreMissing(episode({ status: 'marked_downloaded' })), true)
  assert.equal(canRestoreMissing(episode({ status: 'ignored' })), true)
  assert.equal(canRestoreMissing(episode({ status: 'missing' })), false)
})

test('候选资源比较只展示差异并始终要求人工审阅', () => {
  const result = describeCandidateDifference(
    episode({
      active_torrent_hash: 'old-hash',
      active_torrent_url: 'https://example.com/old.torrent',
      active_title: '旧资源标题'
    }),
    candidate()
  )

  assert.equal(result.action, 'manual_review')
  assert.deepEqual(result.hash, {
    current: 'old-hash',
    candidate: 'new-hash',
    different: true
  })
  assert.deepEqual(result.url, {
    current: 'https://example.com/old.torrent',
    candidate: 'https://example.com/new.torrent',
    different: true
  })
  assert.deepEqual(result.title, {
    current: '旧资源标题',
    candidate: '新资源标题',
    different: true
  })
  assert.equal(result.publishedAt, '未知')
})

test('候选资源比较安全处理空资源身份、长标题和空发布时间', () => {
  const longTitle = '超长标题'.repeat(50)
  const result = describeCandidateDifference(
    episode({ active_torrent_hash: '', active_torrent_url: '', active_title: '' }),
    candidate({ torrent_hash: '', torrent_url: '', title: longTitle, pub_time: null })
  )

  assert.equal(result.action, 'manual_review')
  assert.equal(result.hash.current, '未知')
  assert.equal(result.hash.candidate, '未知')
  assert.equal(result.hash.different, false)
  assert.equal(result.url.current, '未知')
  assert.equal(result.url.candidate, '未知')
  assert.equal(result.url.different, false)
  assert.equal(result.title.current, '未知')
  assert.equal(result.title.candidate, longTitle)
  assert.equal(result.publishedAt, '未知')
})

test('候选资源比较完整保留前缀相同但版本不同的长标题', () => {
  const sharedPrefix = '共同前缀'.repeat(20)
  const currentTitle = `${sharedPrefix} v1`
  const candidateTitle = `${sharedPrefix} v2`
  const result = describeCandidateDifference(
    episode({ active_title: currentTitle }),
    candidate({ title: candidateTitle })
  )

  assert.equal(result.title.current, currentTitle)
  assert.equal(result.title.candidate, candidateTitle)
  assert.equal(result.title.different, true)
})

test('连续进度在首个缺口停止并忽略输入顺序', () => {
  const episodes = [
    episode({ episode: 4, status: 'downloaded' }),
    episode({ episode: 2, status: 'ignored' }),
    episode({ episode: 1, status: 'marked_downloaded' }),
    episode({ episode: 3, status: 'missing' })
  ]

  assert.equal(continuousOwnedEpisode(episodes), 2)
})

test('剧集筛选支持状态和待处理候选', () => {
  const episodes = [
    episode({ episode: 1, status: 'downloaded' }),
    episode({ episode: 2, status: 'missing', action_required_candidate_count: 2 }),
    episode({ episode: 3, status: 'ignored', action_required_candidate_count: 1 })
  ]

  assert.deepEqual(filterEpisodes(episodes, 'missing').map(item => item.episode), [2])
  assert.deepEqual(filterEpisodes(episodes, 'candidate').map(item => item.episode), [2, 3])
  assert.deepEqual(filterEpisodes(episodes, 'all').map(item => item.episode), [1, 2, 3])
})

test('批量恢复缺失会拆分可执行、无变化和被活动下载阻止的集数', () => {
  const episodes = [
    episode({ episode: 1, status: 'downloaded' }),
    episode({ episode: 2, status: 'missing' }),
    episode({ episode: 3, status: 'downloading', active_download_id: 9 })
  ]

  assert.deepEqual(planEpisodeStatusUpdate(episodes, [1, 2, 3, 4, 4], 'missing'), {
    eligible: [1, 4],
    blocked: [3],
    unchanged: [2]
  })
})

test('候选状态只暴露后端允许的操作', () => {
  assert.deepEqual(candidateAvailableActions('pending'), ['keep', 'replace'])
  assert.deepEqual(candidateAvailableActions('failed'), ['retry_replace'])
  assert.deepEqual(candidateAvailableActions('replacing'), ['progress'])
  assert.deepEqual(candidateAvailableActions('accepted_cleanup_failed'), ['retry_cleanup'])
  assert.deepEqual(candidateAvailableActions('accepted'), [])
  assert.deepEqual(candidateAvailableActions('kept_existing'), [])
})

test('资源外链只允许 http、https 和 magnet scheme', () => {
  assert.equal(safeExternalURL(' https://example.com/a '), 'https://example.com/a')
  assert.equal(safeExternalURL('HTTP://example.com/a'), 'HTTP://example.com/a')
  assert.equal(safeExternalURL('magnet:?xt=urn:btih:abc'), 'magnet:?xt=urn:btih:abc')
  assert.equal(safeExternalURL('javascript:alert(1)'), undefined)
  assert.equal(safeExternalURL('data:text/html,test'), undefined)
  assert.equal(safeExternalURL('file:///tmp/a'), undefined)
  assert.equal(safeExternalURL('//example.com/a'), undefined)
  assert.equal(safeExternalURL('/relative'), undefined)
  assert.equal(safeExternalURL(''), undefined)
})

test('一万集分页只返回当前页并将越界页码收敛到有效范围', () => {
  const episodes = Array.from({ length: 10_000 }, (_, index) => index + 1)
  const middle = paginateItems(episodes, 2, 120)
  assert.equal(middle.page, 2)
  assert.equal(middle.pageCount, 84)
  assert.equal(middle.items.length, 120)
  assert.equal(middle.items[0], 121)
  assert.equal(middle.items.at(-1), 240)

  const clamped = paginateItems(episodes, 999, 120)
  assert.equal(clamped.page, 84)
  assert.equal(clamped.items.length, 40)
  assert.equal(clamped.items[0], 9961)
  assert.equal(clamped.items.at(-1), 10_000)
})

test('候选加载更多按 ID 去重、更新旧项并保持顺序', () => {
  const merged = appendUniqueById(
    [{ id: 1, value: 'a' }, { id: 2, value: 'old' }],
    [{ id: 2, value: 'new' }, { id: 3, value: 'c' }]
  )
  assert.deepEqual(merged, [
    { id: 1, value: 'a' },
    { id: 2, value: 'new' },
    { id: 3, value: 'c' }
  ])
})

test('字幕组和语言差异使用原始值 trim 后比较', () => {
  assert.equal(normalizedValuesDiffer(' Group ', 'Group'), false)
  assert.equal(normalizedValuesDiffer('', null), false)
  assert.equal(normalizedValuesDiffer('CHS', 'CHT'), true)
})

test('跨页、单选和手工添加共用最多 500 集的选择上限', () => {
  const existing = Array.from({ length: 499 }, (_, index) => index + 1)
  const result = appendEpisodeSelection(existing, [499, 500, 501], 500)

  assert.equal(result.selected.length, 500)
  assert.equal(result.selected.at(-1), 500)
  assert.deepEqual(result.rejected, [501])

  const full = appendEpisodeSelection(result.selected, [600], 500)
  assert.deepEqual(full.selected, result.selected)
  assert.deepEqual(full.rejected, [600])
})

test('候选分页使用 101 条 lookahead 且只展示前 100 条', () => {
  const exact = takeLookaheadPage(Array.from({ length: 100 }, (_, index) => index + 1), 100)
  assert.equal(exact.items.length, 100)
  assert.equal(exact.hasMore, false)

  const lookahead = takeLookaheadPage(Array.from({ length: 101 }, (_, index) => index + 1), 100)
  assert.equal(lookahead.items.length, 100)
  assert.equal(lookahead.items.at(-1), 100)
  assert.equal(lookahead.hasMore, true)
})
