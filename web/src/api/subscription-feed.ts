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

export const subscriptionFeedApi = {
  list: (subscriptionId: number) => api.get(`/subscriptions/${subscriptionId}/feeds`),
  preview: (subscriptionId: number, input: SubscriptionFeedInput, feedId?: number) =>
    api.post(
      feedId
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
