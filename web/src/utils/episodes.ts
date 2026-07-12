export interface EpisodeProgress {
  current_episode?: number
  latest_episode?: number
  rss_latest_episode?: number
  bangumi_latest_episode?: number
  episode_offset?: number
  total_episodes?: number
}

export const getRelativeEpisode = (episode = 0, offset = 0): number => {
  return Math.max(0, episode - Math.max(0, offset))
}

export const getRelativeCurrentEpisode = (progress: EpisodeProgress): number => {
  return getRelativeEpisode(progress.current_episode || 0, progress.episode_offset || 0)
}

export const getRelativeLatestEpisode = (progress: EpisodeProgress): number => {
  return getRelativeEpisode(progress.latest_episode || 0, progress.episode_offset || 0)
}

export const getRelativeRSSLatestEpisode = (progress: EpisodeProgress): number => {
  if (progress.rss_latest_episode === undefined) {
    return getRelativeLatestEpisode(progress)
  }
  return getRelativeEpisode(progress.rss_latest_episode, progress.episode_offset || 0)
}

export const getRelativeAiredEpisode = (progress: EpisodeProgress): number => {
  return Math.max(0, progress.bangumi_latest_episode || getRelativeLatestEpisode(progress))
}

export const getRSSMissingEpisodes = (progress: EpisodeProgress): number[] => {
  const current = getRelativeCurrentEpisode(progress)
  const latest = getRelativeRSSLatestEpisode(progress)
  if (latest <= current) return []

  return Array.from({ length: latest - current }, (_, index) => current + index + 1)
}

export const isEpisodeProgressComplete = (progress: EpisodeProgress): boolean => {
  const total = progress.total_episodes || 0
  return total > 0 && getRelativeCurrentEpisode(progress) >= total
}

export const getEpisodeProgressPercent = (progress: EpisodeProgress): number => {
  const total = progress.total_episodes || 0
  if (total <= 0) return 0
  return Math.min(100, Math.round((getRelativeCurrentEpisode(progress) / total) * 100))
}
