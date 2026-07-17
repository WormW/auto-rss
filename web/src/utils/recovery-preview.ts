import type {
  RecoveryScanResult,
  RecoverySubscriptionScanResult
} from '../api/index.ts'

export const RECOVERY_ORPHAN_DISPLAY_LIMIT = 20
export const RECOVERY_APPLIED_WARNING = '恢复扫描返回已应用状态，请检查服务端配置'

export interface RecoveryOrphanPreview {
  files: string[]
  omitted: number
}

export function episodeChanged(subscription: RecoverySubscriptionScanResult): boolean {
  return subscription.current_episode_old !== subscription.current_episode_new
}

export function latestChanged(subscription: RecoverySubscriptionScanResult): boolean {
  return subscription.latest_episode_old !== subscription.latest_episode_new
}

export function hasRecoveryChanges(subscription: RecoverySubscriptionScanResult): boolean {
  return episodeChanged(subscription) ||
    latestChanged(subscription) ||
    subscription.downloads_to_update_count > 0 ||
    subscription.downloads_to_create_count > 0 ||
    subscription.downloads_missing_count > 0
}

export function countRecoveryChanges(result?: RecoveryScanResult | null): number {
  if (!result) return 0
  return result.subscriptions.reduce((total, subscription) => {
    return total +
      subscription.downloads_to_update_count +
      subscription.downloads_to_create_count +
      (episodeChanged(subscription) ? 1 : 0) +
      (latestChanged(subscription) ? 1 : 0)
  }, 0)
}

export function countRecoveryMissing(result?: RecoveryScanResult | null): number {
  if (!result) return 0
  return result.downloads_missing_count
}

export function hasNoRecoverySubscriptionMatches(result?: RecoveryScanResult | null): boolean {
  if (!result) return false
  return result.subscriptions.length === 0
}

export function getRecoveryEmptyDescription(
  selectedSubscriptionId: number,
  scopeLabel: string
): string {
  const scope = selectedSubscriptionId ? scopeLabel : '全部订阅'
  return `${scope}没有匹配到可对账的订阅文件`
}

export function getRecoveryOrphanPreview(
  result?: RecoveryScanResult | null,
  limit = RECOVERY_ORPHAN_DISPLAY_LIMIT
): RecoveryOrphanPreview {
  const orphanFiles = result?.orphan_file_samples ?? []
  const displayLimit = Math.max(0, Math.floor(limit) || 0)
  const locallyOmitted = Math.max(0, orphanFiles.length - displayLimit)
  return {
    files: orphanFiles.slice(0, displayLimit),
    omitted: Math.max(0, result?.orphan_file_omitted_count ?? 0) + locallyOmitted
  }
}

export function getRecoveryAppliedWarning(result?: RecoveryScanResult | null): string | null {
  return result?.applied ? RECOVERY_APPLIED_WARNING : null
}
