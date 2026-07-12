import type { TagProps } from 'naive-ui'

import type {
  EpisodeResourceCandidate,
  EpisodeStatus,
  SubscriptionEpisode
} from '../api/episode.ts'

type EpisodeStatusTagType = NonNullable<TagProps['type']>

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

const UNKNOWN_VALUE = '未知'
const MAX_TITLE_LENGTH = 80

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

const displayTitle = (value?: string | null): string => {
  const normalized = normalizeValue(value)
  if (!normalized) return UNKNOWN_VALUE
  if (normalized.length <= MAX_TITLE_LENGTH) return normalized
  return `${normalized.slice(0, MAX_TITLE_LENGTH)}...`
}

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

export const episodeStatusType = (status: EpisodeStatus): EpisodeStatusTagType => statusTypes[status]

export const isEpisodeOwned = (status: EpisodeStatus): boolean => {
  return status === 'downloaded' || status === 'marked_downloaded' || status === 'ignored'
}

export const canRestoreMissing = (episode: SubscriptionEpisode): boolean => {
  if (episode.status === 'missing') return false
  return episode.status !== 'downloading' || episode.active_download_id == null
}

export const describeCandidateDifference = (
  current: SubscriptionEpisode,
  candidate: EpisodeResourceCandidate
): CandidateDifferenceDescription => ({
  action: 'manual_review',
  hash: describeField(current.active_torrent_hash, candidate.torrent_hash),
  url: describeField(current.active_torrent_url, candidate.torrent_url),
  title: describeField(current.active_title, candidate.title, displayTitle),
  fansub: displayValue(candidate.fansub),
  language: displayValue(candidate.language),
  publishedAt: displayValue(candidate.pub_time),
  sourceRSSURL: displayValue(candidate.source_rss_url)
})
