import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios'
import {
  clearAuthTokens,
  getAccessToken,
  getRefreshToken,
  setAuthTokens,
  type TokenPair
} from '@/services/auth-state'

interface RetryableRequestConfig extends InternalAxiosRequestConfig {
  _retry?: boolean
}

export const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
})

const refreshApi = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
})

api.interceptors.request.use((config) => {
  const token = getAccessToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (response) => {
    return response.data
  },
  async (error: AxiosError) => {
    const originalRequest = error.config as RetryableRequestConfig | undefined
    const requestPath = originalRequest?.url || ''
    const isAuthEndpoint = requestPath.startsWith('/auth/')

    if (error.response?.status === 401 && originalRequest && !originalRequest._retry && !isAuthEndpoint) {
      const refreshToken = getRefreshToken()
      if (refreshToken) {
        originalRequest._retry = true
        try {
          const response = await refreshApi.post('/auth/refresh', { refresh_token: refreshToken })
          const tokenPair = response.data.data as TokenPair
          setAuthTokens(tokenPair)
          originalRequest.headers.Authorization = `Bearer ${tokenPair.access_token}`
          return api(originalRequest)
        } catch (refreshError) {
          clearAuthTokens()
          window.dispatchEvent(new CustomEvent('auth:required'))
          return Promise.reject(refreshError)
        }
      }

      clearAuthTokens()
      window.dispatchEvent(new CustomEvent('auth:required'))
    }

    console.error('API Error:', error)
    return Promise.reject(error)
  }
)

export interface AuthStatus {
  auth_enabled: boolean
  username: string
}

let authStatusCache: AuthStatus | null = null
let authStatusPromise: Promise<AuthStatus> | null = null

const extractData = <T>(response: any): T => response.data as T

export const authApi = {
  status: async (force = false): Promise<AuthStatus> => {
    if (!force && authStatusCache) {
      return authStatusCache
    }
    if (!force && authStatusPromise) {
      return authStatusPromise
    }

    authStatusPromise = api.get('/auth/status')
      .then((response) => {
        const status = extractData<AuthStatus>(response)
        authStatusCache = status
        return status
      })
      .finally(() => {
        authStatusPromise = null
      })

    return authStatusPromise
  },
  login: async (username: string, password: string): Promise<TokenPair> => {
    const response = await api.post('/auth/login', { username, password })
    const tokens = extractData<TokenPair>(response)
    setAuthTokens(tokens)
    authStatusCache = null
    return tokens
  },
  logout: async (): Promise<void> => {
    const refreshToken = getRefreshToken()
    try {
      if (refreshToken) {
        await api.post('/auth/logout', { refresh_token: refreshToken })
      }
    } finally {
      clearAuthTokens()
    }
  },
  clearStatusCache: () => {
    authStatusCache = null
  }
}

export interface Subscription {
  id: number
  name: string
  rss_url: string
  season: number
  status: string
  filter_keywords: string
  exclude_keywords: string
  subgroup_id?: number
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
  collection_torrent?: string
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
  // 语言偏好设置
  language_preference?: string
  // 追番日历相关
  air_day?: string
  air_time?: string
  air_timezone?: string
  notify_enabled?: boolean
  notify_before_min?: number
}

export interface Download {
  id: number
  subscription_id: number
  title: string
  episode: number
  fansub: string
  language?: string
  torrent_url: string
  torrent_hash: string
  file_path: string
  renamed_path: string
  media_library_path: string
  media_library_refresh_status: 'pending' | 'disabled' | 'success' | 'failed' | ''
  media_library_refresh_error: string
  media_library_refreshed_at?: string
  status: string
  qb_task_id: string
  error_message: string
  downloaded_at?: string
  created_at: string
  updated_at: string
  // 重试相关字段
  retry_count?: number
  max_retries?: number
  next_retry_at?: string
  last_error?: string
  retry_reason?: string
  subscription?: Subscription
}

export interface SubscriptionPreviewItem {
  title: string
  episode: number
  relative_episode: number
  fansub?: string
  language?: string
  language_keyword?: string
  pub_date?: string
  torrent_url?: string
  torrent_hash?: string
  action: 'download' | 'replace' | 'duplicate' | 'skip'
  reason: string
  existing_download_id?: number
  download_path?: string
  rename_preview?: string
}

export interface SubscriptionPreviewSummary {
  total_items: number
  previewed_items: number
  download_items: number
  replace_items: number
  skipped_items: number
  duplicate_items: number
  latest_episode: number
  download_path: string
  subscription_name: string
  season: number
  limited: boolean
}

export interface SubscriptionPreview {
  summary: SubscriptionPreviewSummary
  items: SubscriptionPreviewItem[]
}

export interface DownloadDiagnostics {
  id: number
  status: string
  severity: 'success' | 'info' | 'warning' | 'error'
  category: string
  title: string
  detail: string
  can_retry: boolean
  retry_blocked?: string
  checks: Record<string, boolean>
  actions: Array<{ key: string; label: string; enabled: boolean }>
}

export type DiagnosticStatus = 'healthy' | 'warning' | 'error' | 'unknown'

export interface SubscriptionDiagnosticCheck {
  key: string
  label: string
  status: DiagnosticStatus
  summary: string
  detail: string
}

export interface SubscriptionDiagnosticAction {
  key: string
  label: string
  method: string
  endpoint: string
  enabled: boolean
  reason?: string
}

export interface SubscriptionDownloadDiagnosticItem {
  id: number
  title: string
  episode: number
  status: string
  severity: string
  category: string
  reason: string
  can_retry: boolean
  retry_blocked?: string
}

export interface SubscriptionDiagnostics {
  subscription_id: number
  name: string
  enabled: boolean
  checked_at: string
  summary: {
    overall: DiagnosticStatus
    healthy: number
    warning: number
    error: number
    unknown: number
  }
  checks: SubscriptionDiagnosticCheck[]
  downloads: {
    total: number
    pending: number
    downloading: number
    stalled: number
    failed: number
    completed: number
    organizing: number
    retryable: number
    missing_torrent_tasks: number
    failed_items: SubscriptionDownloadDiagnosticItem[]
  }
  files: {
    expected_path: string
    folder_exists: boolean
    rename_enabled: boolean
    completed_with_file: number
    completed_missing_file: number
    missing_renamed: number
    missing_episodes: number[]
  }
  disk: {
    path: string
    exists: boolean
    status: string
    total_bytes: number
    free_bytes: number
    used_bytes: number
    usage_percent: number
    warning_threshold_gb: number
    critical_threshold_gb: number
    error?: string
  }
  actions: SubscriptionDiagnosticAction[]
}

export interface SubscriptionRetryFailedResult {
  id: number
  title: string
  status: string
  success: boolean
  message: string
}

export interface SubscriptionRetryFailedResponse {
  subscription_id: number
  retried: number
  failed: number
  skipped: number
  results: SubscriptionRetryFailedResult[]
}

export interface MediaLibraryPathMapping {
  from: string
  to: string
}

export interface MediaLibraryConfig {
  enabled: boolean
  provider: 'jellyfin' | 'emby' | 'plex'
  base_url: string
  token?: string
  token_configured?: boolean
  username?: string
  library_id?: string
  section_id?: string
  path_mappings: MediaLibraryPathMapping[]
  refresh_on_import: boolean
}

export interface MediaLibraryRefreshResult {
  enabled: boolean
  status: 'pending' | 'disabled' | 'success' | 'failed'
  message: string
  path: string
  refreshed_at?: string
}

export const subscriptionApi = {
  list: (page = 1, pageSize = 20) =>
    api.get('/subscriptions', { params: { page, page_size: pageSize } }),
  getById: (id: number) =>
    api.get(`/subscriptions/${id}`),
  diagnostics: (id: number) =>
    api.get(`/subscriptions/${id}/diagnostics`),
  retryFailed: (id: number) =>
    api.post(`/subscriptions/${id}/diagnostics/retry-failed`),
  create: (data: Partial<Subscription>) =>
    api.post('/subscriptions', data),
  preview: (data: Partial<Subscription> & { id?: number; limit?: number }) =>
    api.post('/subscriptions/preview', data),
  update: (id: number, data: Partial<Subscription>) =>
    api.put(`/subscriptions/${id}`, data),
  delete: (id: number) =>
    api.delete(`/subscriptions/${id}`),
  renameFiles: (id: number) =>
    api.post(`/subscriptions/${id}/rename-files`),
  scanFolder: (id: number, data: { folder_path: string; dry_run: boolean; rename_files: boolean }) =>
    api.post(`/subscriptions/${id}/scan-folder`, data),
  batchImportFromRSS: (items: Array<{title: string, fansub?: string, rss_url?: string, season?: number, source_id?: number, source_name?: string}>) =>
    api.post('/subscriptions/batch-import-from-rss', { items }),
}

export const downloadApi = {
  list: (page = 1, pageSize = 20, status?: string) =>
    api.get('/downloads', { params: { page, page_size: pageSize, status } }),
  getById: (id: number) =>
    api.get(`/downloads/${id}`),
  diagnostics: (id: number) =>
    api.get(`/downloads/${id}/diagnostics`),
  delete: (id: number) =>
    api.delete(`/downloads/${id}`),
  retry: (id: number) =>
    api.post(`/downloads/${id}/retry`),
  batchDelete: (ids: number[]) =>
    api.post('/downloads/batch-delete', { ids }),
  clear: (status?: string) =>
    api.delete('/downloads/clear', { params: { status } })
}

export const mediaLibraryApi = {
  getConfig: () =>
    api.get('/media-library/config'),
  saveConfig: (config: MediaLibraryConfig) =>
    api.put('/media-library/config', config),
  testConnection: (config: MediaLibraryConfig) =>
    api.post('/media-library/test', config),
  refreshDownload: (id: number) =>
    api.post(`/media-library/downloads/${id}/refresh`),
  getSubscriptionStatus: (id: number) =>
    api.get(`/media-library/subscriptions/${id}/status`)
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

export interface BackupPackageSummary {
  configs: number
  rss_sources: number
  groups: number
  tags: number
  subscriptions: number
  subscription_tags: number
  notification_settings: number
}

export interface BackupPackage {
  schema_version: string
  app: string
  exported_at: string
  includes_sensitive: boolean
  sensitive_placeholder: string
  summary: BackupPackageSummary
  configs: any[]
  rss_sources: any[]
  groups: any[]
  tags: any[]
  subscriptions: any[]
  subscription_tags: any[]
  notification_settings: any[]
}

export interface BackupImportSummary {
  total: number
  create: number
  overwrite: number
  merge: number
  skip: number
  sensitive_skipped: number
}

export interface BackupImportItem {
  resource: string
  key: string
  name: string
  action: 'create' | 'overwrite' | 'merge' | 'skip'
  reason: string
  conflict: boolean
  sensitive: boolean
}

export interface BackupImportPlan {
  source_format: string
  strategy: 'skip' | 'overwrite' | 'merge'
  summary: BackupImportSummary
  items: BackupImportItem[]
}

export const backupApi = {
  export: (includeSensitive = false) =>
    api.get('/backup/export', { params: { include_sensitive: includeSensitive } }),
  preview: (data: unknown, sourceFormat = 'auto', strategy: 'skip' | 'overwrite' | 'merge' = 'skip') =>
    api.post('/backup/preview', {
      data,
      source_format: sourceFormat,
      strategy
    }),
  import: (data: unknown, sourceFormat = 'auto', strategy: 'skip' | 'overwrite' | 'merge' = 'skip') =>
    api.post('/backup/import', {
      data,
      source_format: sourceFormat,
      strategy
    })
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

// 通知相关接口
export interface NotificationSetting {
  id: number
  channel: string
  enabled: boolean
  config: string
  created_at: string
  updated_at: string
}

export interface WebhookTemplate {
  name: string
  label: string
  description: string
  template: string
}

export const notificationApi = {
  getSettings: () =>
    api.get('/notifications/settings'),
  getSetting: (channel: string) =>
    api.get(`/notifications/settings/${channel}`),
  updateSetting: (data: { channel: string; enabled: boolean; config: object }) =>
    api.put('/notifications/settings', data),
  deleteSetting: (channel: string) =>
    api.delete(`/notifications/settings/${channel}`),
  testChannel: (data: { channel: string; config: object }) =>
    api.post('/notifications/test', data),
  listNotifications: (page = 1, pageSize = 20, status?: string, channel?: string) =>
    api.get('/notifications', { params: { page, page_size: pageSize, status, channel } }),
  getWebhookTemplates: () =>
    api.get('/notifications/webhook/templates'),
  getWebSocketStatus: () =>
    api.get('/notifications/websocket/status')
}

// 日历相关接口
export interface CalendarItem {
  subscription_id: number
  name: string
  episode: number
  air_time: string
  air_day: string
  current_episode: number
  total_episodes: number
  is_downloaded: boolean
  is_completed: boolean
  cover?: string
}

export interface DaySchedule {
  day: string
  day_cn: string
  items: CalendarItem[]
  is_today: boolean
}

export interface WeekSchedule {
  week: string
  days: DaySchedule[]
}

export const calendarApi = {
  getWeekSchedule: (weekOffset = 0) =>
    api.get('/calendar', { params: { week: weekOffset } }),
  getTodaySchedule: () =>
    api.get('/calendar/today')
}

// 磁盘相关接口
export interface DiskStatus {
  path: string
  download_path: string
  total: number
  free: number
  used: number
  usage_percent: number
  status: 'healthy' | 'warning' | 'critical'
}

export interface DiskSample extends DiskStatus {
  created_at: string
}

export interface DiskCleanupRecord {
  id: number
  trigger: 'manual' | 'auto' | string
  strategy: string
  download_path: string
  deleted_count: number
  skipped_count: number
  freed_bytes: number
  before_free_bytes: number
  after_free_bytes: number
  media_library_status: 'unconfigured' | 'connected' | 'failed' | string
  message: string
  created_at: string
}

export interface DiskHistory {
  samples: DiskSample[]
  cleanup: DiskCleanupRecord[]
  list: DiskCleanupRecord[]
  total: number
  page: number
}

export interface DiskSettings {
  enabled: boolean
  strategy: string
  retention_days: number
  min_free_gb: number
  warning_threshold_gb: number
  critical_threshold_gb: number
  protect_watching: boolean
  media_library_status: 'unconfigured' | 'connected' | 'failed' | string
  media_library_message: string
}

export const diskApi = {
  getStatus: () =>
    api.get('/disk/status'),
  getInfo: () =>
    api.get('/disk/info'),
  getSettings: () =>
    api.get('/disk/settings'),
  getHistory: (page = 1, pageSize = 20) =>
    api.get('/disk/history', { params: { page, page_size: pageSize } }),
  cleanup: (payload: { strategy?: string; keep_days?: number; keep_gb?: number }) =>
    api.post('/disk/cleanup', payload)
}

export * from './rss-source'
export * from './mikan'

export default api
