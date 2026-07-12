import type { SubscriptionFeedInput } from '../api/subscription-feed.ts'

export type PlannedFeed = SubscriptionFeedInput & { requiresPreview: boolean }

export interface FeedSavePlan {
  create: PlannedFeed[]
  update: PlannedFeed[]
  remove: number[]
}

export function normalizeFeedURLForComparison(raw: string): string {
  const parsed = new URL(raw.trim())
  parsed.hash = ''
  parsed.searchParams.sort()
  return parsed.toString()
}

export function hasDuplicateFeedURLs(feeds: SubscriptionFeedInput[]): boolean {
  const urls = feeds.map((feed) => normalizeFeedURLForComparison(feed.rss_url))
  return new Set(urls).size !== urls.length
}

function changed(before: SubscriptionFeedInput, after: SubscriptionFeedInput): boolean {
  return before.name !== after.name ||
    (before.fansub || '') !== (after.fansub || '') ||
    normalizeFeedURLForComparison(before.rss_url) !== normalizeFeedURLForComparison(after.rss_url) ||
    before.episode_offset !== after.episode_offset ||
    before.enabled !== after.enabled
}

export function buildFeedSavePlan(
  original: SubscriptionFeedInput[],
  current: SubscriptionFeedInput[]
): FeedSavePlan {
  const originalById = new Map(
    original.filter((item) => item.id).map((item) => [item.id!, item])
  )
  const currentIds = new Set(current.filter((item) => item.id).map((item) => item.id!))
  const create = current
    .filter((item) => !item.id)
    .map((item) => ({ ...item, requiresPreview: true }))
  const update = current.flatMap((item) => {
    if (!item.id) return []
    const before = originalById.get(item.id)
    if (!before || !changed(before, item)) return []
    const requiresPreview =
      normalizeFeedURLForComparison(before.rss_url) !==
        normalizeFeedURLForComparison(item.rss_url) ||
      before.episode_offset !== item.episode_offset
    return [{ ...item, requiresPreview }]
  })
  const remove = original
    .filter((item) => item.id && !currentIds.has(item.id))
    .map((item) => item.id!)
  return { create, update, remove }
}
