export interface EpisodeProgress {
  current_episode?: number
  latest_episode?: number
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

export const isEpisodeProgressComplete = (progress: EpisodeProgress): boolean => {
  const total = progress.total_episodes || 0
  return total > 0 && getRelativeCurrentEpisode(progress) >= total
}

export const getEpisodeProgressPercent = (progress: EpisodeProgress): number => {
  const total = progress.total_episodes || 0
  if (total <= 0) return 0
  return Math.min(100, Math.round((getRelativeCurrentEpisode(progress) / total) * 100))
}
