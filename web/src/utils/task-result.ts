export interface CollectionTaskResult {
  feeds_checked?: number
  items_scanned?: number
  downloads_created?: number
  candidates_created?: number
  feed_errors?: number
}

const count = (value: number | undefined): number => {
  return Number.isFinite(value) && (value || 0) > 0 ? Number(value) : 0
}

export const formatCollectionTaskResult = (result: CollectionTaskResult): string => {
  const parts = [
    `下载 ${count(result.downloads_created)}`,
    `扫描 ${count(result.items_scanned)}`
  ]
  const candidates = count(result.candidates_created)
  if (candidates > 0) parts.push(`候选 ${candidates}`)
  parts.push(`Feed ${count(result.feeds_checked)}`)
  const errors = count(result.feed_errors)
  if (errors > 0) parts.push(`错误 ${errors}`)
  return parts.join(' / ')
}

export const collectionTaskHasErrors = (result: CollectionTaskResult): boolean => {
  return count(result.feed_errors) > 0
}
