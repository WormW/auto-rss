import assert from 'node:assert/strict'
import test from 'node:test'

import type {
  RecoveryScanResult,
  RecoverySubscriptionScanResult
} from '../src/api/index.ts'
import {
  RECOVERY_APPLIED_WARNING,
  RECOVERY_ORPHAN_DISPLAY_LIMIT,
  countRecoveryChanges,
  countRecoveryMissing,
  getRecoveryAppliedWarning,
  getRecoveryEmptyDescription,
  getRecoveryOrphanPreview
} from '../src/utils/recovery-preview.ts'

test('恢复预览统计拟议变更数量', () => {
  const result = createRecoveryResult({
    subscriptions: [
      createSubscription({
        current_episode_old: 1,
        current_episode_new: 2,
        latest_episode_old: 3,
        latest_episode_new: 4,
        downloads_to_update: [11, 12],
        downloads_to_create: [5]
      }),
      createSubscription({
        subscription_id: 2,
        name: 'No changes'
      })
    ]
  })

  assert.equal(countRecoveryChanges(result), 5)
  assert.equal(countRecoveryChanges(null), 0)
})

test('恢复预览统计缺失下载记录数量', () => {
  const result = createRecoveryResult({
    subscriptions: [
      createSubscription({ downloads_missing: [1, 2, 3] }),
      createSubscription({
        subscription_id: 2,
        name: 'Another',
        downloads_missing: [7]
      })
    ]
  })

  assert.equal(countRecoveryMissing(result), 4)
  assert.equal(countRecoveryMissing(undefined), 0)
})

test('恢复预览空状态文案区分全部订阅和指定范围', () => {
  assert.equal(
    getRecoveryEmptyDescription(0, '订阅：不会使用'),
    '全部订阅没有匹配到可对账的订阅文件'
  )
  assert.equal(
    getRecoveryEmptyDescription(12, '订阅：葬送的芙莉莲'),
    '订阅：葬送的芙莉莲没有匹配到可对账的订阅文件'
  )
})

test('恢复预览限制未匹配文件展示并返回省略数量', () => {
  const orphanFiles = Array.from(
    { length: RECOVERY_ORPHAN_DISPLAY_LIMIT + 3 },
    (_, index) => `/downloads/orphan-${index + 1}.mkv`
  )

  const preview = getRecoveryOrphanPreview(createRecoveryResult({ orphan_files: orphanFiles }))

  assert.equal(preview.files.length, RECOVERY_ORPHAN_DISPLAY_LIMIT)
  assert.equal(preview.files.at(0), '/downloads/orphan-1.mkv')
  assert.equal(preview.files.at(-1), `/downloads/orphan-${RECOVERY_ORPHAN_DISPLAY_LIMIT}.mkv`)
  assert.equal(preview.omitted, 3)
})

test('恢复预览识别 applied=true 的安全警告', () => {
  assert.equal(
    getRecoveryAppliedWarning(createRecoveryResult({ applied: true })),
    RECOVERY_APPLIED_WARNING
  )
  assert.equal(getRecoveryAppliedWarning(createRecoveryResult({ applied: false })), null)
  assert.equal(getRecoveryAppliedWarning(null), null)
})

function createRecoveryResult(overrides: Partial<RecoveryScanResult> = {}): RecoveryScanResult {
  return {
    scanned_files: 0,
    matched_files: 0,
    orphan_files: [],
    subscriptions: [],
    applied: false,
    ...overrides
  }
}

function createSubscription(
  overrides: Partial<RecoverySubscriptionScanResult> = {}
): RecoverySubscriptionScanResult {
  return {
    subscription_id: 1,
    name: 'Anime',
    current_episode_old: 1,
    current_episode_new: 1,
    latest_episode_old: 1,
    latest_episode_new: 1,
    episodes_on_disk: [],
    matched_episodes: [],
    downloads_to_update: [],
    downloads_to_create: [],
    downloads_missing: [],
    ...overrides
  }
}
