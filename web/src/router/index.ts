import { createRouter, createWebHistory } from 'vue-router'
import RSSSources from '@/views/RSSSources.vue'
import Subscriptions from '@/views/Subscriptions.vue'
import Downloads from '@/views/Downloads.vue'
import Config from '@/views/Config.vue'
import Logs from '@/views/Logs.vue'

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
      component: RSSSources
    },
    {
      path: '/subscriptions',
      name: 'subscriptions',
      component: Subscriptions
    },
    {
      path: '/downloads',
      name: 'downloads',
      component: Downloads
    },
    {
      path: '/config',
      name: 'config',
      component: Config
    },
    {
      path: '/logs',
      name: 'logs',
      component: Logs
    }
  ]
})

export default router
