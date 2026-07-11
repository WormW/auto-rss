import type {
  DiagnosticStatus,
  SubscriptionDiagnosticCheck,
  SubscriptionDiagnosticCheckResponse,
  SubscriptionDiagnostics
} from '../api/index.ts'

export function mergeDiagnosticCheck(
  current: SubscriptionDiagnostics,
  result: SubscriptionDiagnosticCheckResponse
): SubscriptionDiagnostics {
  const hasCurrentCheck = current.checks.some(check => check.key === result.check.key)
  const checks = hasCurrentCheck
    ? current.checks.map(check => check.key === result.check.key ? result.check : check)
    : [...current.checks, result.check]

  return {
    ...current,
    summary: summarizeDiagnosticChecks(checks),
    checks,
    downloads: result.downloads
      ? { ...current.downloads, ...result.downloads }
      : current.downloads,
    files: result.files
      ? { ...current.files, ...result.files }
      : current.files,
    disk: result.disk
      ? { ...current.disk, ...result.disk }
      : current.disk,
    actions: result.actions ?? current.actions
  }
}

export function summarizeDiagnosticChecks(
  checks: SubscriptionDiagnosticCheck[]
): SubscriptionDiagnostics['summary'] {
  const summary: SubscriptionDiagnostics['summary'] = {
    overall: 'unknown',
    checked: 0,
    total: checks.length,
    healthy: 0,
    warning: 0,
    error: 0,
    unknown: 0
  }

  for (const check of checks) {
    if (!check.checked) {
      summary.unknown++
      continue
    }
    summary.checked++
    if (check.status !== 'unknown') {
      summary[check.status]++
    }
  }

  summary.overall = worstCheckedStatus(summary)
  return summary
}

export interface DiagnosticRequestContext {
  subscriptionId: number
  session: number
}

export function isCurrentDiagnosticRequest(
  current: DiagnosticRequestContext,
  request: DiagnosticRequestContext
): boolean {
  return current.subscriptionId === request.subscriptionId && current.session === request.session
}

export function getDiagnosticActionFollowUp(actionKey: string): string {
  const followUps: Record<string, string> = {
    refresh_rss: '可重新检查 RSS 可达性、最近检查和待收集集数',
    retry_failed: '可重新检查下载任务和 qBittorrent',
    reorganize_files: '可重新检查本地文件和整理/重命名',
    rename_files: '可重新检查本地文件和整理/重命名',
    toggle_subscription: '可重新检查订阅状态'
  }
  return followUps[actionKey] || ''
}

function worstCheckedStatus(summary: SubscriptionDiagnostics['summary']): DiagnosticStatus {
  if (summary.error > 0) return 'error'
  if (summary.warning > 0) return 'warning'
  if (summary.healthy > 0) return 'healthy'
  return 'unknown'
}
