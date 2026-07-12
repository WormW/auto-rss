import type { BangumiSubject } from '@/api'

export interface BangumiSubscriptionPatch {
  name: string
  bangumi_id: number
  season?: number
  total_episodes?: number
  air_day?: string
  update_day?: string
}

export function buildBangumiSubscriptionPatch(subject: BangumiSubject): BangumiSubscriptionPatch {
  const patch: BangumiSubscriptionPatch = {
    name: subject.name_cn.trim() || subject.name.trim(),
    bangumi_id: subject.id
  }

  if (subject.season > 0) {
    patch.season = subject.season
  }
  if (subject.total_episodes > 0) {
    patch.total_episodes = subject.total_episodes
  }
  if (Number.isInteger(subject.air_weekday) && subject.air_weekday >= 0 && subject.air_weekday <= 6) {
    const weekday = String(subject.air_weekday)
    patch.air_day = weekday
    patch.update_day = weekday
  }

  return patch
}
