import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      redirect: '/rss-sources'
    },
    {
      path: '/rss-sources',
      name: 'rss-sources',
      component: () => import('@/views/RSSSources.vue')
    },
    {
      path: '/subscriptions',
      name: 'subscriptions',
      component: () => import('@/views/Subscriptions.vue')
    },
    {
      path: '/downloads',
      name: 'downloads',
      component: () => import('@/views/Downloads.vue')
    },
    {
      path: '/calendar',
      name: 'calendar',
      component: () => import('@/views/Calendar.vue')
    },
    {
      path: '/notifications',
      name: 'notifications',
      component: () => import('@/views/Notifications.vue')
    },
    {
      path: '/disk-monitor',
      name: 'disk-monitor',
      component: () => import('@/views/DiskMonitor.vue')
    },
    {
      path: '/config',
      name: 'config',
      component: () => import('@/views/Config.vue')
    },
    {
      path: '/logs',
      name: 'logs',
      component: () => import('@/views/Logs.vue')
    }
  ]
})

export default router
