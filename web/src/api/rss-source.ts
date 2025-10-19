import { api } from './index'

export interface RSSSource {
  id: number
  name: string
  base_url: string
  description: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface RSSAnime {
  title: string
  rss_url: string
  fansub: string
  update_day: string
  episodes: string[]
  source_id: number
  source_name: string
}

export const rssSourceApi = {
  list: (page: number, pageSize: number, enabled?: boolean) => {
    const params: any = { page, page_size: pageSize }
    if (enabled !== undefined) {
      params.enabled = enabled
    }
    return api.get('/rss-sources', { params })
  },

  get: (id: number) => {
    return api.get(`/rss-sources/${id}`)
  },

  create: (data: Partial<RSSSource>) => {
    return api.post('/rss-sources', data)
  },

  update: (id: number, data: Partial<RSSSource>) => {
    return api.put(`/rss-sources/${id}`, data)
  },

  delete: (id: number) => {
    return api.delete(`/rss-sources/${id}`)
  },

  fetchAnimes: (id: number) => {
    return api.get(`/rss-sources/${id}/animes`)
  }
}
