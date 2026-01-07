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
  latest_episode?: number
  episode_offset?: number
  filter_rules?: string
  enabled?: boolean
  last_download_at?: string
  rss_source_id?: number
  source_type?: string
  // Bangumi相关字段
  bangumi_id?: number
  bangumi_score?: number
  bangumi_summary?: string
  bangumi_cover?: string
  bangumi_cover_local?: string
  bangumi_rank?: number
  bangumi_season?: number
  // 下载统计字段
  downloading_count?: number
  // 开播信息
  air_date?: string
  air_year?: number
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
    api.delete(`/subscriptions/${id}`),
  batchImportFromRSS: (items: Array<{title: string, fansub?: string, rss_url?: string, source_id?: number, source_name?: string}>) =>
    api.post('/subscriptions/batch-import-from-rss', { items })
}

export const downloadApi = {
  list: (page = 1, pageSize = 20, status?: string) =>
    api.get('/downloads', { params: { page, page_size: pageSize, status } }),
  getById: (id: number) =>
    api.get(`/downloads/${id}`),
  delete: (id: number) =>
    api.delete(`/downloads/${id}`),
  retry: (id: number) =>
    api.post(`/downloads/${id}/retry`),
  batchDelete: (ids: number[]) =>
    api.post('/downloads/batch-delete', { ids }),
  clear: (status?: string) =>
    api.delete('/downloads/clear', { params: { status } })
}

export const rssApi = {
  refresh: () =>
    api.post('/rss/refresh')
}

export const configApi = {
  getAll: () =>
    api.get('/config'),
  update: (key: string, value: string) =>
    api.put('/config', { key, value }),
  testQBittorrent: (host: string, username: string, password: string) =>
    api.post('/config/qbittorrent/test', { host, username, password }),
  saveQBittorrent: (host: string, username: string, password: string) =>
    api.post('/config/qbittorrent/save', { host, username, password }),
  // 重命名模板 API
  getRenamePresets: () =>
    api.get('/config/rename/presets'),
  getRenameTemplate: () =>
    api.get('/config/rename/template'),
  saveRenameTemplate: (template: string) =>
    api.post('/config/rename/template', { template }),
  previewRenameTemplate: (template: string) =>
    api.post('/config/rename/preview', { template })
}

export const fileOrganizerApi = {
  triggerScan: () =>
    api.post('/file-organizer/trigger'),
  reloadConfig: () =>
    api.post('/file-organizer/reload')
}

// 任务类型
export interface Task {
  id: string
  type: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  subscription_id?: number
  name: string
  progress: number
  message: string
  error?: string
  started_at?: string
  completed_at?: string
  result?: any
}

export const taskApi = {
  getCurrent: () =>
    api.get('/tasks/current'),
  getHistory: () =>
    api.get('/tasks/history'),
  cancel: () =>
    api.post('/tasks/cancel')
}

export * from './rss-source'
export * from './mikan'

export default api
