import { api } from './index'

export interface MikanAnimeItem {
  title: string
  url: string
  cover: string
  score: number
  exists: boolean
  id: string
}

export interface MikanSeason {
  year: number
  season: string
  select: boolean
}

export interface MikanAnimeGroup {
  label: string
  items: MikanAnimeItem[]
}

export interface MikanSearchResult {
  groups: MikanAnimeGroup[]
  seasons: MikanSeason[]
}

export interface MikanFansubGroup {
  name: string
  rss: string
  update_day: string
  tags: string[]
  episodes: string[]
}

export const mikanApi = {
  // 搜索番剧
  search: (text: string) =>
    api.get<{ data: MikanSearchResult }>('/mikan/search', { params: { text } }),

  // 按季度获取番剧
  getBySeason: (year: number, season: string) =>
    api.get<{ data: MikanSearchResult }>('/mikan/season', { params: { year, season } }),

  // 获取字幕组列表
  getFansubGroups: (url: string) =>
    api.get<{ data: MikanFansubGroup[] }>('/mikan/fansub-groups', { params: { url } })
}
