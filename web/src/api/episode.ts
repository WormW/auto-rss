import { api } from './index'

export type EpisodeStatus =
  | 'missing'
  | 'downloading'
  | 'downloaded'
  | 'marked_downloaded'
  | 'ignored'

export type EpisodeStatusSource = 'automatic' | 'user' | 'migration'

export type CandidateStatus =
  | 'pending'
  | 'kept_existing'
  | 'replacing'
  | 'accepted'
  | 'accepted_cleanup_failed'
  | 'failed'

export type ReplacementStage =
  | 'queued'
  | 'downloading'
  | 'download_cleanup'
  | 'terminal_cleanup'
  | 'detaching'
  | 'staged'
  | 'old_backed_up'
  | 'promoted'
  | 'switched'
  | 'cleanup_queued'
  | 'cleanup_active'
  | 'cleaning'
  | 'done'
  | ''

export type EpisodeTaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface EpisodeApiResponse<T> {
  code: number
  message: string
  data: T
  reason?: string
}

export interface SubscriptionEpisode {
  id: number
  subscription_id: number
  episode: number
  status: EpisodeStatus
  active_download_id: number | null
  active_torrent_hash: string
  active_torrent_url: string
  active_title: string
  status_source: EpisodeStatusSource
  downloaded_at: string | null
  created_at: string
  updated_at: string
  action_required_candidate_count: number
}

export interface EpisodeResourceCandidate {
  id: number
  subscription_episode_id: number
  resource_key: string
  torrent_hash: string
  torrent_url: string
  title: string
  fansub: string
  language: string
  pub_time: string | null
  source_rss_url: string
  status: CandidateStatus
  failure_reason: string
  staged_path: string
  old_resource_path: string
  rollback_path: string
  final_path: string
  replacement_stage: ReplacementStage
  replacement_download_id: number | null
  old_download_id: number | null
  old_torrent_hash: string
  created_at: string
  updated_at: string
}

export interface EpisodeCandidateListParams {
  limit?: number
  offset?: number
}

export interface EpisodeStatusUpdateResult {
  episodes: number[]
  status: EpisodeStatus
}

export interface EpisodeTaskResult {
  task_id: string
  status: EpisodeTaskStatus
}

export const episodeApi = {
  list: (subscriptionId: number) =>
    api.get<
      EpisodeApiResponse<SubscriptionEpisode[]>,
      EpisodeApiResponse<SubscriptionEpisode[]>
    >(
      `/subscriptions/${subscriptionId}/episodes`
    ),
  updateStatus: (subscriptionId: number, episodes: number[], status: EpisodeStatus) =>
    api.put<
      EpisodeApiResponse<EpisodeStatusUpdateResult>,
      EpisodeApiResponse<EpisodeStatusUpdateResult>
    >(
      `/subscriptions/${subscriptionId}/episodes/status`,
      { episodes, status }
    ),
  listCandidates: (
    subscriptionId: number,
    episode: number,
    params?: EpisodeCandidateListParams
  ) =>
    api.get<
      EpisodeApiResponse<EpisodeResourceCandidate[]>,
      EpisodeApiResponse<EpisodeResourceCandidate[]>
    >(
      `/subscriptions/${subscriptionId}/episodes/${episode}/candidates`,
      { params }
    ),
  keepExisting: (subscriptionId: number, episode: number, candidateId: number) =>
    api.post<
      EpisodeApiResponse<EpisodeResourceCandidate>,
      EpisodeApiResponse<EpisodeResourceCandidate>
    >(
      `/subscriptions/${subscriptionId}/episodes/${episode}/candidates/${candidateId}/keep`
    ),
  replace: (subscriptionId: number, episode: number, candidateId: number) =>
    api.post<EpisodeApiResponse<EpisodeTaskResult>, EpisodeApiResponse<EpisodeTaskResult>>(
      `/subscriptions/${subscriptionId}/episodes/${episode}/candidates/${candidateId}/replace`
    ),
  retryCleanup: (subscriptionId: number, episode: number, candidateId: number) =>
    api.post<EpisodeApiResponse<EpisodeTaskResult>, EpisodeApiResponse<EpisodeTaskResult>>(
      `/subscriptions/${subscriptionId}/episodes/${episode}/candidates/${candidateId}/retry-cleanup`
    )
}
