import assert from 'node:assert/strict'
import test from 'node:test'

import type { EpisodeResourceCandidate, SubscriptionEpisode } from '../src/api/episode.ts'
import {
  canRestoreMissing,
  describeCandidateDifference,
  episodeStatusLabel,
  episodeStatusType,
  isEpisodeOwned
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
  assert.equal(result.title.candidate.endsWith('...'), true)
  assert.equal(result.title.candidate.length <= 83, true)
  assert.equal(result.publishedAt, '未知')
})
