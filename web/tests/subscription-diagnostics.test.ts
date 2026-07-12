import assert from 'node:assert/strict'
import test from 'node:test'

import type { SubscriptionDiagnostics } from '../src/api/index.ts'
import {
	getDiagnosticActionFollowUp,
	isCurrentDiagnosticRequest,
  mergeDiagnosticCheck,
  summarizeDiagnosticChecks
} from '../src/utils/subscription-diagnostics.ts'

test('单项结果只替换对应检查并计算部分汇总', () => {
  const initial = createInitialDiagnostics()
  const next = mergeDiagnosticCheck(initial, {
    check: {
      key: 'rss_freshness',
      label: '最近检查',
      checked: true,
      status: 'warning',
      summary: '2 天未检查',
      detail: ''
    }
  })

  assert.equal(next.summary.checked, 1)
  assert.equal(next.summary.total, 9)
  assert.equal(next.summary.overall, 'warning')
  assert.equal(
    next.checks.find(item => item.key === 'rss_reachability')?.status,
    'unknown'
  )
})

test('单项扩展数据采用浅合并并保留其他检查指标', () => {
  const initial = createInitialDiagnostics()
  initial.files.completed_with_file = 3
  initial.files.completed_missing_file = 1

  const next = mergeDiagnosticCheck(initial, {
    check: {
      key: 'episode_progress',
      label: '待收集集数',
      checked: true,
      status: 'warning',
      summary: '待收集 1 集',
      detail: ''
    },
    files: {
      missing_episodes: [52]
    }
  })

  assert.equal(next.files.completed_with_file, 3)
  assert.equal(next.files.completed_missing_file, 1)
  assert.deepEqual(next.files.missing_episodes, [52])
})

test('RSS 单项检查更新 feed 健康结果', () => {
  const initial = createInitialDiagnostics()
  const feeds: SubscriptionDiagnostics['feeds'] = [{
    subscription_feed_id: 7,
    name: '字幕组 A',
    fansub: 'A',
    rss_url: 'https://example.test/a.xml',
    status: 'healthy',
    response_time_ms: 18
  }]

  const next = mergeDiagnosticCheck(initial, {
    check: {
      key: 'rss_reachability',
      label: 'RSS 可达性',
      checked: true,
      status: 'healthy',
      summary: '1 个订阅源可用',
      detail: ''
    },
    feeds
  })

  assert.deepEqual(next.feeds, feeds)
})

test('汇总忽略未检查项并按已检查项计算最坏状态', () => {
  const checks = createInitialDiagnostics().checks
  checks[0] = { ...checks[0], checked: true, status: 'healthy' }
  checks[1] = { ...checks[1], checked: true, status: 'error' }

  assert.deepEqual(summarizeDiagnosticChecks(checks), {
    overall: 'error',
    checked: 2,
    total: 9,
    healthy: 1,
    warning: 0,
    error: 1,
    unknown: 7
  })
})

test('已执行但无法判断的检查仍计入已检查数量', () => {
  const checks = createInitialDiagnostics().checks
  checks[0] = { ...checks[0], checked: true, status: 'unknown', summary: '未配置 RSS' }

  assert.equal(summarizeDiagnosticChecks(checks).checked, 1)
})

test('仅接受当前诊断会话的异步响应', () => {
  const current = { subscriptionId: 2, session: 4 }

  assert.equal(isCurrentDiagnosticRequest(current, { subscriptionId: 2, session: 4 }), true)
  assert.equal(isCurrentDiagnosticRequest(current, { subscriptionId: 1, session: 4 }), false)
  assert.equal(isCurrentDiagnosticRequest(current, { subscriptionId: 2, session: 3 }), false)
})

test('修复动作提示重新检查受影响项目', () => {
  assert.match(getDiagnosticActionFollowUp('refresh_rss'), /RSS 可达性/)
  assert.match(getDiagnosticActionFollowUp('retry_failed'), /下载任务/)
  assert.match(getDiagnosticActionFollowUp('reorganize_files'), /整理\/重命名/)
  assert.match(getDiagnosticActionFollowUp('rename_files'), /整理\/重命名/)
  assert.match(getDiagnosticActionFollowUp('toggle_subscription'), /订阅状态/)
})

function createInitialDiagnostics(): SubscriptionDiagnostics {
  const definitions = [
    ['subscription_enabled', '订阅状态'],
    ['rss_reachability', 'RSS 可达性'],
    ['rss_freshness', '最近检查'],
    ['episode_progress', '待收集集数'],
    ['downloads', '下载任务'],
    ['qbittorrent', 'qBittorrent'],
    ['files', '本地文件'],
    ['organizer', '整理/重命名'],
    ['disk', '磁盘空间']
  ] as const

  return {
    subscription_id: 1,
    name: 'Anime',
    enabled: true,
    checked_at: '2026-07-11T12:00:00Z',
    feeds: [],
    summary: {
      overall: 'unknown',
      checked: 0,
      total: 9,
      healthy: 0,
      warning: 0,
      error: 0,
      unknown: 9
    },
    checks: definitions.map(([key, label]) => ({
      key,
      label,
      checked: false,
      status: 'unknown',
      summary: '未检查',
      detail: ''
    })),
    downloads: {
      total: 0,
      pending: 0,
      downloading: 0,
      stalled: 0,
      failed: 0,
      completed: 0,
      organizing: 0,
      retryable: 0,
      missing_torrent_tasks: 0,
      failed_items: []
    },
    files: {
      expected_path: '',
      folder_exists: false,
      rename_enabled: false,
      completed_with_file: 0,
      completed_missing_file: 0,
      missing_renamed: 0,
      missing_episodes: []
    },
    disk: {
      path: '',
      exists: false,
      status: '',
      total_bytes: 0,
      free_bytes: 0,
      used_bytes: 0,
      usage_percent: 0,
      warning_threshold_gb: 0,
      critical_threshold_gb: 0
    },
    actions: []
  }
}
