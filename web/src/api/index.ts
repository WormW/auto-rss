import axios from 'axios'

export const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
})

api.interceptors.response.use(
  (response) => {
    return response.data
  },
  (error) => {
    console.error('API Error:', error)
    return Promise.reject(error)
  }
)

export interface Subscription {
  id: number
  name: string
  rss_url: string
  season: number
  status: string
  filter_keywords: string
  exclude_keywords: string
  subgroup_id?: number
  download_path: string
  rename_enabled: boolean
  last_check_time?: string
  created_at: string
  updated_at: string
  // 新增字段
  fansub?: string
  language?: string
  update_day?: string
  total_episodes?: number
  current_episode?: number
  episode_offset?: number
  filter_rules?: string
  enabled?: boolean
  last_download_at?: string
  rss_source_id?: number
  source_type?: string
}

export interface Download {
  id: number
  subscription_id: number
  title: string
  episode: number
  fansub: string
  torrent_url: string
  torrent_hash: string
  file_path: string
  renamed_path: string
  status: string
  qb_task_id: string
  error_message: string
  downloaded_at?: string
  created_at: string
  updated_at: string
}

export const subscriptionApi = {
  list: (page = 1, pageSize = 20) =>
    api.get('/subscriptions', { params: { page, page_size: pageSize } }),
  getById: (id: number) =>
    api.get(`/subscriptions/${id}`),
  create: (data: Partial<Subscription>) =>
    api.post('/subscriptions', data),
  update: (id: number, data: Partial<Subscription>) =>
    api.put(`/subscriptions/${id}`, data),
  delete: (id: number) =>
    api.delete(`/subscriptions/${id}`)
}

export const downloadApi = {
  list: (page = 1, pageSize = 20, status?: string) =>
    api.get('/downloads', { params: { page, page_size: pageSize, status } }),
  getById: (id: number) =>
    api.get(`/downloads/${id}`),
  delete: (id: number) =>
    api.delete(`/downloads/${id}`),
  retry: (id: number) =>
    api.post(`/downloads/${id}/retry`)
}

export const rssApi = {
  refresh: () =>
    api.post('/rss/refresh')
}

export const configApi = {
  getAll: () =>
    api.get('/config'),
  update: (key: string, value: string) =>
    api.put('/config', { key, value })
}

export * from './rss-source'
export * from './mikan'

export default api
