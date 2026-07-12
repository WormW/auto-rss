import type { TagProps } from 'naive-ui'

import type {
  CandidateStatus,
  EditableEpisodeStatus,
  EpisodeResourceCandidate,
  EpisodeStatus,
  SubscriptionEpisode
} from '../api/episode.ts'

type EpisodeStatusTagType = NonNullable<TagProps['type']>

export type EpisodeFilter = 'all' | 'candidate' | EpisodeStatus
export type CandidateAction = 'keep' | 'replace' | 'retry_replace' | 'retry_cleanup' | 'progress'

export interface EpisodeStatusUpdatePlan {
  eligible: number[]
  blocked: number[]
  unchanged: number[]
}

export interface ResourceDifferenceField {
  current: string
  candidate: string
  different: boolean
}

export interface CandidateDifferenceDescription {
  action: 'manual_review'
  hash: ResourceDifferenceField
  url: ResourceDifferenceField
  title: ResourceDifferenceField
  fansub: string
  language: string
  publishedAt: string
  sourceRSSURL: string
}

export interface PaginatedItems<T> {
  items: T[]
  page: number
  pageCount: number
  total: number
}

export interface EpisodeSelectionResult {
  selected: number[]
  rejected: number[]
}

export interface LookaheadPage<T> {
  items: T[]
  hasMore: boolean
}

const UNKNOWN_VALUE = '未知'

const statusLabels: Record<EpisodeStatus, string> = {
  missing: '缺失',
  downloading: '下载中',
  downloaded: '已下载',
  marked_downloaded: '已标记下载',
  ignored: '已忽略'
}

const statusTypes: Record<EpisodeStatus, EpisodeStatusTagType> = {
  missing: 'error',
  downloading: 'warning',
  downloaded: 'success',
  marked_downloaded: 'info',
  ignored: 'default'
}

const normalizeValue = (value?: string | null): string => value?.trim() || ''

const displayValue = (value?: string | null): string => normalizeValue(value) || UNKNOWN_VALUE

const describeField = (
  currentValue: string | null | undefined,
  candidateValue: string | null | undefined,
  display: (value?: string | null) => string = displayValue
): ResourceDifferenceField => {
  const current = normalizeValue(currentValue)
  const candidate = normalizeValue(candidateValue)
  return {
    current: display(current),
    candidate: display(candidate),
    different: current !== candidate
  }
}

export const episodeStatusLabel = (status: EpisodeStatus): string => statusLabels[status]

export const safeExternalURL = (value?: string | null): string | undefined => {
  const normalized = normalizeValue(value)
  if (!normalized) return undefined
  try {
    const parsed = new URL(normalized)
    return ['http:', 'https:', 'magnet:'].includes(parsed.protocol.toLowerCase())
      ? normalized
      : undefined
  } catch {
    return undefined
  }
}

export const paginateItems = <T>(items: T[], requestedPage: number, requestedPageSize: number): PaginatedItems<T> => {
  const pageSize = Math.max(1, Math.floor(requestedPageSize) || 1)
  const pageCount = Math.max(1, Math.ceil(items.length / pageSize))
  const page = Math.min(pageCount, Math.max(1, Math.floor(requestedPage) || 1))
  const offset = (page - 1) * pageSize
  return {
    items: items.slice(offset, offset + pageSize),
    page,
    pageCount,
    total: items.length
  }
}

export const appendUniqueById = <T extends { id: number }>(current: T[], incoming: T[]): T[] => {
  const merged = [...current]
  const indexById = new Map(merged.map((item, index) => [item.id, index]))
  for (const item of incoming) {
    const existingIndex = indexById.get(item.id)
    if (existingIndex == null) {
      indexById.set(item.id, merged.length)
      merged.push(item)
    } else {
      merged[existingIndex] = item
    }
  }
  return merged
}

export const appendEpisodeSelection = (
  current: number[],
  incoming: number[],
  limit: number
): EpisodeSelectionResult => {
  const normalizedLimit = Math.max(0, Math.floor(limit) || 0)
  const selected: number[] = []
  const rejected: number[] = []
  const seen = new Set<number>()

  for (const episode of [...current, ...incoming]) {
    if (seen.has(episode)) continue
    seen.add(episode)
    if (selected.length < normalizedLimit) selected.push(episode)
    else rejected.push(episode)
  }

  return {
    selected: selected.sort((left, right) => left - right),
    rejected
  }
}

export const takeLookaheadPage = <T>(items: T[], pageSize: number): LookaheadPage<T> => {
  const normalizedPageSize = Math.max(1, Math.floor(pageSize) || 1)
  return {
    items: items.slice(0, normalizedPageSize),
    hasMore: items.length > normalizedPageSize
  }
}

export const normalizedValuesDiffer = (
  current?: string | null,
  candidate?: string | null
): boolean => normalizeValue(current) !== normalizeValue(candidate)

export const episodeStatusType = (status: EpisodeStatus): EpisodeStatusTagType => statusTypes[status]

export const isEpisodeOwned = (status: EpisodeStatus): boolean => {
  return status === 'downloaded' || status === 'marked_downloaded' || status === 'ignored'
}

export const continuousOwnedEpisode = (episodes: SubscriptionEpisode[]): number => {
  let continuous = 0
  const ordered = [...episodes].sort((left, right) => left.episode - right.episode)
  for (const episode of ordered) {
    if (episode.episode === continuous + 1 && isEpisodeOwned(episode.status)) {
      continuous++
    }
  }
  return continuous
}

export const filterEpisodes = (
  episodes: SubscriptionEpisode[],
  filter: EpisodeFilter
): SubscriptionEpisode[] => {
  if (filter === 'all') return episodes
  if (filter === 'candidate') {
    return episodes.filter(episode => episode.action_required_candidate_count > 0)
  }
  return episodes.filter(episode => episode.status === filter)
}

export const canRestoreMissing = (episode: SubscriptionEpisode): boolean => {
  if (episode.status === 'missing') return false
  return episode.status !== 'downloading' || episode.active_download_id == null
}

export const planEpisodeStatusUpdate = (
  episodes: SubscriptionEpisode[],
  selectedEpisodes: number[],
  targetStatus: EditableEpisodeStatus
): EpisodeStatusUpdatePlan => {
  const byNumber = new Map(episodes.map(episode => [episode.episode, episode]))
  const plan: EpisodeStatusUpdatePlan = { eligible: [], blocked: [], unchanged: [] }

  for (const episodeNumber of [...new Set(selectedEpisodes)]) {
    const episode = byNumber.get(episodeNumber)
    if (episode?.status === targetStatus) {
      plan.unchanged.push(episodeNumber)
    } else if (targetStatus === 'missing' && episode && !canRestoreMissing(episode)) {
      plan.blocked.push(episodeNumber)
    } else {
      plan.eligible.push(episodeNumber)
    }
  }
  return plan
}

export const candidateAvailableActions = (status: CandidateStatus): CandidateAction[] => {
  switch (status) {
    case 'pending':
      return ['keep', 'replace']
    case 'failed':
      return ['retry_replace']
    case 'replacing':
      return ['progress']
    case 'accepted_cleanup_failed':
      return ['retry_cleanup']
    default:
      return []
  }
}

export const describeCandidateDifference = (
  current: SubscriptionEpisode,
  candidate: EpisodeResourceCandidate
): CandidateDifferenceDescription => ({
  action: 'manual_review',
  hash: describeField(current.active_torrent_hash, candidate.torrent_hash),
  url: describeField(current.active_torrent_url, candidate.torrent_url),
  title: describeField(current.active_title, candidate.title),
  fansub: displayValue(candidate.fansub),
  language: displayValue(candidate.language),
  publishedAt: displayValue(candidate.pub_time),
  sourceRSSURL: displayValue(candidate.source_rss_url)
})
