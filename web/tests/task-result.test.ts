import assert from 'node:assert/strict'
import test from 'node:test'

import { formatCollectionTaskResult } from '../src/utils/task-result.ts'

test('采集任务展示后端真实统计字段', () => {
  assert.equal(formatCollectionTaskResult({
    feeds_checked: 2,
    items_scanned: 15,
    downloads_created: 2,
    downloads_recovered: 1,
    candidates_created: 1,
    feed_errors: 1
  }), '下载 2 / 恢复 1 / 扫描 15 / 候选 1 / Feed 2 / 错误 1')
})

test('采集任务零结果仍明确展示下载和扫描数量', () => {
  assert.equal(formatCollectionTaskResult({
    feeds_checked: 1,
    items_scanned: 0,
    downloads_created: 0,
    candidates_created: 0,
    feed_errors: 0
  }), '下载 0 / 扫描 0 / Feed 1')
})
