import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import type { SubscriptionDiagnostics } from '../src/api/index.ts'
import {
  getDiagnosticActionFollowUp,
  isCurrentDiagnosticRequest,
  mergeDiagnosticCheck,
  summarizeDiagnosticChecks
} from '../src/utils/subscription-diagnostics.ts'

const subscriptionsViewSource = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../src/views/Subscriptions.vue'),
  'utf8'
)

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

test('打开订阅诊断面板只初始化会话并加载初始状态，不自动执行单项检查', () => {
  const handleDiagnostics = extractFunctionBody(subscriptionsViewSource, 'handleDiagnostics')
  const loadInitialDiagnostics = extractFunctionBody(subscriptionsViewSource, 'loadInitialDiagnostics')

  assert.match(handleDiagnostics, /diagnosticsSession\.value\+\+/)
  assert.match(handleDiagnostics, /diagnosticsData\.value = null/)
  assert.match(handleDiagnostics, /diagnosticsCheckLoading\.value = \{\}/)
  assert.match(handleDiagnostics, /diagnosticsActionLoading\.value = ''/)
  assert.match(handleDiagnostics, /diagnosticsActionResult\.value = ''/)
  assert.match(handleDiagnostics, /showDiagnosticsModal\.value = true/)
  assert.match(handleDiagnostics, /await loadInitialDiagnostics\(\)/)

  assert.match(loadInitialDiagnostics, /subscriptionApi\.diagnostics\(request\.subscriptionId\)/)
  assert.doesNotMatch(handleDiagnostics, /checkDiagnostic|diagnosticCheck|runDiagnosticCheck|forEach|for\s*\(|for\s+await/)
  assert.doesNotMatch(loadInitialDiagnostics, /checkDiagnostic|diagnosticCheck|runDiagnosticCheck|forEach|for\s*\(|for\s+await/)
})

test('诊断面板没有检查全部入口，单项检查按钮使用 keyed loading 状态', () => {
  const diagnosticsTemplate = extractBetween(
    subscriptionsViewSource,
    '<!-- 健康诊断面板 -->',
    '<!-- 添加/编辑订阅对话框 -->'
  )
  const runDiagnosticCheck = extractFunctionBody(subscriptionsViewSource, 'runDiagnosticCheck')

  assert.doesNotMatch(diagnosticsTemplate, /检查全部|重新检查全部/)
  assert.match(diagnosticsTemplate, /v-for="check in diagnosticsData\.checks"/)
  assert.match(diagnosticsTemplate, /:loading="diagnosticsCheckLoading\[check\.key\]"/)
  assert.match(diagnosticsTemplate, /:disabled="diagnosticsCheckLoading\[check\.key\]"/)
  assert.match(diagnosticsTemplate, /@click="runDiagnosticCheck\(check\.key\)"/)
  assert.match(runDiagnosticCheck, /\.\.\.diagnosticsCheckLoading\.value/)
  assert.match(runDiagnosticCheck, /\[key\]: true/)
  assert.match(runDiagnosticCheck, /\[key\]: false/)
  assert.doesNotMatch(runDiagnosticCheck, /diagnosticsCheckLoading\.value = true|diagnosticsCheckLoading\.value = false/)
})

test('未执行对应检查前诊断指标保持占位，动作按钮仍受 enabled 状态约束', () => {
  const diagnosticsTemplate = extractBetween(
    subscriptionsViewSource,
    '<!-- 健康诊断面板 -->',
    '<!-- 添加/编辑订阅对话框 -->'
  )

  assert.match(diagnosticsTemplate, /hasDiagnosticCheck\('downloads'\) \? diagnosticsData\.downloads\.total : '--'/)
  assert.match(diagnosticsTemplate, /v-if="hasDiagnosticCheck\('downloads'\)">失败/)
  assert.match(diagnosticsTemplate, /hasDiagnosticCheck\('files'\) \? diagnosticsData\.files\.completed_with_file : '--'/)
  assert.match(diagnosticsTemplate, /v-if="hasDiagnosticCheck\('files'\)">未记录路径/)
  assert.match(diagnosticsTemplate, /hasDiagnosticCheck\('disk'\) \? formatBytes\(diagnosticsData\.disk\.free_bytes\) : '--'/)
  assert.match(diagnosticsTemplate, /v-if="hasDiagnosticCheck\('disk'\)">/)
  assert.match(diagnosticsTemplate, /hasDiagnosticCheck\('episode_progress'\) \? \(diagnosticsData\.files\.missing_episodes\?\.length \|\| 0\) : '--'/)
  assert.match(diagnosticsTemplate, /v-if="hasDiagnosticCheck\('episode_progress'\)"/)
  assert.match(diagnosticsTemplate, /v-else>未检查<\/small>/)
  assert.match(diagnosticsTemplate, /v-if="hasDiagnosticCheck\('downloads'\) && diagnosticsData\.downloads\.failed_items\?\.length"/)
  assert.match(diagnosticsTemplate, /:disabled="!action\.enabled \|\| diagnosticsActionLoading === action\.key"/)
  assert.match(diagnosticsTemplate, /diagnosticsData\.actions\.some\(action => !action\.enabled && action\.reason\)/)
})

function extractBetween(source: string, startMarker: string, endMarker: string): string {
  const start = source.indexOf(startMarker)
  const end = source.indexOf(endMarker, start)
  assert.notEqual(start, -1, `missing start marker: ${startMarker}`)
  assert.notEqual(end, -1, `missing end marker: ${endMarker}`)
  return source.slice(start, end)
}

function extractFunctionBody(source: string, name: string): string {
  const declaration = new RegExp(`const ${name} = (?:async )?\\([^)]*\\) => \\{`, 'm')
  const match = declaration.exec(source)
  assert.ok(match, `missing function: ${name}`)

  const bodyStart = match.index + match[0].length
  let depth = 1
  for (let index = bodyStart; index < source.length; index++) {
    const char = source[index]
    if (char === '{') {
      depth++
    } else if (char === '}') {
      depth--
      if (depth === 0) {
        return source.slice(bodyStart, index)
      }
    }
  }

  assert.fail(`unterminated function: ${name}`)
}

function createInitialDiagnostics(): SubscriptionDiagnostics {
  const definitions = [
    ['subscription_enabled', '订阅状态'],
    ['rss_reachability', 'RSS 可达性'],
    ['rss_freshness', '最近检查'],
    ['episode_progress', '待收集集数'],
    ['downloads', '下载任务'],
    ['qbittorrent', 'qBittorrent'],
    ['files', '已记录路径'],
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
