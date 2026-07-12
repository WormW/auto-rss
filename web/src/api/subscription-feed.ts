import { api } from './index'

export interface SubscriptionFeed {
  id: number
  subscription_id: number
  name: string
  fansub: string
  rss_url: string
  episode_offset: number
  enabled: boolean
  baseline_pending: boolean
  last_rss_pub_time?: string
  last_check_time?: string
  last_success_at?: string
  last_error: string
}

export interface SubscriptionFeedInput {
  id?: number
  name: string
  fansub?: string
  rss_url: string
  episode_offset: number
  enabled: boolean
}

export interface SubscriptionFeedPreviewItem {
  title: string
  original_episode: number
  episode_offset: number
  relative_episode: number
  valid: boolean
  invalid_reason: string
}

export interface SubscriptionFeedPreview {
  items: SubscriptionFeedPreviewItem[]
  parsed_items: number
  valid_items: number
  warning?: string
}

export const subscriptionFeedApi = {
  list: (subscriptionId: number) => api.get(`/subscriptions/${subscriptionId}/feeds`),
  preview: (subscriptionId: number | undefined, input: SubscriptionFeedInput, feedId?: number) =>
    api.post(
      subscriptionId === undefined
        ? '/subscriptions/feeds/preview'
        : feedId
        ? `/subscriptions/${subscriptionId}/feeds/${feedId}/preview`
        : `/subscriptions/${subscriptionId}/feeds/preview`,
      input
    ),
  create: (subscriptionId: number, input: SubscriptionFeedInput) =>
    api.post(`/subscriptions/${subscriptionId}/feeds`, input),
  update: (subscriptionId: number, feedId: number, input: SubscriptionFeedInput) =>
    api.put(`/subscriptions/${subscriptionId}/feeds/${feedId}`, input),
  remove: (subscriptionId: number, feedId: number) =>
    api.delete(`/subscriptions/${subscriptionId}/feeds/${feedId}`)
}
