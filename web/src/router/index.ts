import { createRouter, createWebHistory } from 'vue-router'
import { authApi, onboardingApi, type OnboardingStatus } from '@/api'
import { hasAuthTokens } from '@/services/auth-state'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      redirect: '/rss-sources'
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/Login.vue'),
      meta: { public: true, hideShell: true }
    },
    {
      path: '/onboarding',
      name: 'onboarding',
      component: () => import('@/views/Onboarding.vue'),
      meta: { hideShell: true }
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
      path: '/backup',
      name: 'backup',
      component: () => import('@/views/BackupRestore.vue')
    },
    {
      path: '/logs',
      name: 'logs',
      component: () => import('@/views/Logs.vue')
    }
  ]
})

router.beforeEach(async (to) => {
  const redirectTarget = typeof to.query.redirect === 'string' ? to.query.redirect : '/rss-sources'
  const shouldCheckOnboarding = to.name !== 'login' && to.name !== 'onboarding'

  try {
    const status = await authApi.status()
    if (!status.auth_enabled) {
      if (to.name === 'login') {
        return { name: 'rss-sources' }
      }
      if (shouldCheckOnboarding) {
        const onboardingResponse: any = await onboardingApi.status()
        const onboardingStatus = onboardingResponse.data as OnboardingStatus
        if (onboardingStatus.should_show) {
          return { name: 'onboarding', query: { redirect: to.fullPath } }
        }
      }
      return true
    }

    if (to.name === 'login') {
      return hasAuthTokens() ? redirectTarget : true
    }

    if (!hasAuthTokens()) {
      return { name: 'login', query: { redirect: to.fullPath } }
    }

    if (shouldCheckOnboarding) {
      const onboardingResponse: any = await onboardingApi.status()
      const onboardingStatus = onboardingResponse.data as OnboardingStatus
      if (onboardingStatus.should_show) {
        return { name: 'onboarding', query: { redirect: to.fullPath } }
      }
    }

    return true
  } catch {
    if (to.name === 'login') {
      return true
    }
    return hasAuthTokens() ? true : { name: 'login', query: { redirect: to.fullPath } }
  }
})

export default router
