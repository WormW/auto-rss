<template>
  <n-config-provider :theme="theme" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-dialog-provider>
        <!-- 移动端布局 -->
        <router-view v-if="hideShell" v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>

        <n-layout v-else-if="isMobile" style="height: 100vh">
          <!-- 移动端 Header -->
          <n-layout-header bordered class="mobile-header">
            <div class="mobile-header-left">
              <n-button text @click="mobileDrawerVisible = true">
                <template #icon>
                  <n-icon size="24"><MenuOutline /></n-icon>
                </template>
              </n-button>
              <n-icon size="24" color="#18a058" style="margin-left: 8px;">
                <Leaf />
              </n-icon>
              <span class="mobile-logo-text">Auto-RSS</span>
            </div>
            <n-space align="center" :size="8">
              <TaskManager />
              <n-button text @click="toggleTheme">
                <template #icon>
                  <n-icon size="20">
                    <Moon v-if="isDark" />
                    <Sunny v-else />
                  </n-icon>
                </template>
              </n-button>
            </n-space>
          </n-layout-header>

          <!-- 移动端内容区 -->
          <n-layout-content class="mobile-content">
            <router-view v-slot="{ Component }">
              <transition name="fade" mode="out-in">
                <component :is="Component" />
              </transition>
            </router-view>
          </n-layout-content>

          <!-- 移动端抽屉菜单 -->
          <n-drawer v-model:show="mobileDrawerVisible" placement="left" :width="280">
            <n-drawer-content>
              <template #header>
                <div style="display: flex; align-items: center; gap: 8px;">
                  <n-icon size="24" color="#18a058"><Leaf /></n-icon>
                  <span style="font-weight: bold; font-size: 16px;">Auto-RSS</span>
                </div>
              </template>
              <n-menu
                :value="currentRoute"
                :options="menuOptions"
                @update:value="handleMobileMenuSelect"
              />
              <template #footer>
                <n-dropdown :options="userOptions" @select="handleUserSelect" trigger="click">
                  <n-button block secondary>
                    <template #icon>
                      <n-avatar round size="small" src="https://0.gravatar.com/avatar/0?d=mp&f=y" />
                    </template>
                    个人设置
                  </n-button>
                </n-dropdown>
              </template>
            </n-drawer-content>
          </n-drawer>
        </n-layout>

        <!-- 桌面端布局 -->
        <n-layout v-else has-sider style="height: 100vh">
          <n-layout-sider
            bordered
            collapse-mode="width"
            :collapsed-width="64"
            :width="240"
            :collapsed="collapsed"
            show-trigger
            @collapse="collapsed = true"
            @expand="collapsed = false"
            style="height: 100vh; transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);"
          >
            <div style="height: 64px; display: flex; align-items: center; justify-content: center; overflow: hidden; white-space: nowrap; position: relative;">
              <n-icon size="32" color="#18a058" style="transition: all 0.3s ease; filter: drop-shadow(0 2px 4px rgba(24, 160, 88, 0.3));">
                <Leaf />
              </n-icon>
              <h2 v-if="!collapsed" style="margin: 0 0 0 12px; font-size: 18px; font-weight: bold; background: linear-gradient(90deg, #18a058 0%, #52c41a 100%); -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text;">Auto-RSS</h2>
            </div>
            <n-menu
              :value="currentRoute"
              :collapsed="collapsed"
              :collapsed-width="64"
              :collapsed-icon-size="22"
              :options="menuOptions"
              @update:value="handleMenuSelect"
            />
          </n-layout-sider>

          <n-layout>
            <n-layout-header bordered style="height: 64px; padding: 0 24px; display: flex; align-items: center; justify-content: space-between; backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px); transition: all 0.3s ease; position: sticky; top: 0; z-index: 100;">
              <!-- Left side of header (Breadcrumb or Page Title could go here) -->
              <div style="display: flex; align-items: center;">
                <!-- Placeholder for future breadcrumb -->
              </div>

              <!-- Right side of header -->
              <n-space align="center" size="large">
                <TaskManager />

                <!-- WebSocket Connection Status Indicator -->
                <n-tooltip v-if="wsStore.status !== 'connected'" placement="bottom">
                  <template #trigger>
                    <n-tag
                      :type="connectionTagType"
                      size="small"
                      style="cursor: pointer;"
                      @click="handleReconnect"
                    >
                      <template #icon>
                        <n-icon :class="{ 'spin-animation': wsStore.status === 'connecting' }">
                          <RefreshOutline v-if="wsStore.status === 'connecting'" />
                          <CloudOfflineOutline v-else />
                        </n-icon>
                      </template>
                      {{ wsStore.statusText }}
                    </n-tag>
                  </template>
                  <div v-if="wsStore.status === 'connecting'">
                    正在尝试重新连接... (第 {{ wsStore.reconnectAttempt }} 次)<br>
                    下次重试: {{ Math.ceil(wsStore.nextReconnectDelay / 1000) }} 秒后
                  </div>
                  <div v-else-if="wsStore.lastError">
                    错误: {{ wsStore.lastError }}
                  </div>
                  <div v-else>
                    点击重新连接
                  </div>
                </n-tooltip>

                <!-- Manual Reconnect Button (when disconnected and not connecting) -->
                <n-button
                  v-if="wsStore.canReconnect"
                  size="small"
                  secondary
                  type="warning"
                  @click="handleReconnect"
                  :loading="wsStore.status === 'connecting'"
                >
                  <template #icon>
                    <n-icon><RefreshOutline /></n-icon>
                  </template>
                  重新连接
                </n-button>

                <n-button circle secondary @click="toggleTheme" style="transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);">
                  <template #icon>
                    <n-icon style="transition: transform 0.3s ease;">
                      <Moon v-if="isDark" />
                      <Sunny v-else />
                    </n-icon>
                  </template>
                </n-button>

                <n-dropdown :options="userOptions" @select="handleUserSelect">
                  <n-avatar
                    round
                    size="small"
                    src="https://0.gravatar.com/avatar/0?d=mp&f=y"
                    style="cursor: pointer; transition: all 0.3s ease; box-shadow: 0 2px 8px rgba(0,0,0,0.1);"
                    @mouseenter="$event.target.style.transform = 'scale(1.1)'"
                    @mouseleave="$event.target.style.transform = 'scale(1)'"
                  />
                </n-dropdown>
              </n-space>
            </n-layout-header>

            <n-layout-content content-style="padding: 24px; min-height: calc(100vh - 64px);">
              <router-view v-slot="{ Component }">
                <transition name="fade" mode="out-in">
                  <component :is="Component" />
                </transition>
              </router-view>
            </n-layout-content>
          </n-layout>
        </n-layout>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { ref, computed, h, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  NConfigProvider,
  NMessageProvider,
  NDialogProvider,
  NLayout,
  NLayoutHeader,
  NLayoutSider,
  NLayoutContent,
  NMenu,
  NSpace,
  NButton,
  NIcon,
  NAvatar,
  NDropdown,
  NDrawer,
  NDrawerContent,
  NTag,
  NTooltip,
  darkTheme,
  zhCN,
  dateZhCN
} from 'naive-ui'
import type { MenuOption } from 'naive-ui'
import {
  RadioOutline,
  AlbumsOutline,
  CloudDownloadOutline,
  SettingsOutline,
  DocumentTextOutline,
  ArchiveOutline,
  CalendarOutline,
  NotificationsOutline,
  Moon,
  Sunny,
  Leaf,
  LogOutOutline,
  PersonOutline,
  RocketOutline,
  MenuOutline,
  CloudOfflineOutline,
  RefreshOutline
} from '@vicons/ionicons5'
import TaskManager from './components/TaskManager.vue'
import { useWebSocketStore } from './stores/websocket'
import { createWebSocketService, WebSocketService } from './services/websocket'
import { authApi } from './api'
import { clearAuthTokens, getAccessToken } from './services/auth-state'

const router = useRouter()
const route = useRoute()

// WebSocket store and service
const wsStore = useWebSocketStore()
const wsService = ref<WebSocketService | null>(null)
const token = ref<string>('')

// Computed property for connection status tag type
const connectionTagType = computed(() => {
  switch (wsStore.status) {
    case 'connected':
      return 'success'
    case 'connecting':
      return 'warning'
    case 'disconnected':
    case 'error':
    case 'max_retries_exceeded':
      return 'error'
    default:
      return 'default'
  }
})
const hideShell = computed(() => route.meta.hideShell === true)

// Handle manual reconnect
const handleReconnect = () => {
  console.log('[App] Manual reconnect triggered, current status:', wsStore.status)
  
  // If service doesn't exist, initialize it first
  if (!wsService.value) {
    console.log('[App] WebSocket service not initialized, creating new instance')
    initWebSocket()
    return
  }
  
  // If service exists and reconnection is possible, trigger reconnect
  if (wsStore.canReconnect) {
    console.log('[App] Triggering WebSocket reconnect')
    wsService.value.reconnect()
  } else {
    console.log('[App] Cannot reconnect, status:', wsStore.status)
  }
}

// Initialize WebSocket connection
const initWebSocket = () => {
  // Get token from localStorage (optional, for authenticated access)
  const storedToken = getAccessToken()
  token.value = storedToken || ''
  
  // Always create WebSocket service, token is optional
  wsService.value = createWebSocketService(wsStore)
  wsService.value.connect(token.value)
}

// Theme logic
const storedTheme = localStorage.getItem('theme')
const isDark = ref(storedTheme === 'dark')
const theme = computed(() => isDark.value ? darkTheme : null)

const toggleTheme = () => {
  isDark.value = !isDark.value
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// Mobile detection
const isMobile = ref(false)
const mobileDrawerVisible = ref(false)

const checkMobile = () => {
  isMobile.value = window.innerWidth < 768
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
  window.addEventListener('auth:changed', handleAuthChanged)
  window.addEventListener('auth:required', handleAuthRequired)
  // Initialize WebSocket after DOM is ready
  if (!hideShell.value) {
    initWebSocket()
  }
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
  window.removeEventListener('auth:changed', handleAuthChanged)
  window.removeEventListener('auth:required', handleAuthRequired)
  // Clean up WebSocket connection
  if (wsService.value) {
    wsService.value.disconnect()
    wsService.value = null
  }
})

// Sidebar logic
const collapsed = ref(false)
const currentRoute = computed(() => route.name as string)

const renderIcon = (icon: any) => {
  return () => h(NIcon, null, { default: () => h(icon) })
}

const menuOptions: MenuOption[] = [
  {
    label: 'RSS 源',
    key: 'rss-sources',
    icon: renderIcon(RadioOutline)
  },
  {
    label: '订阅管理',
    key: 'subscriptions',
    icon: renderIcon(AlbumsOutline)
  },
  {
    label: '下载任务',
    key: 'downloads',
    icon: renderIcon(CloudDownloadOutline)
  },
  {
    label: '追番日历',
    key: 'calendar',
    icon: renderIcon(CalendarOutline)
  },
  {
    label: '通知管理',
    key: 'notifications',
    icon: renderIcon(NotificationsOutline)
  },
  {
    label: '系统配置',
    key: 'config',
    icon: renderIcon(SettingsOutline)
  },
  {
    label: '备份恢复',
    key: 'backup',
    icon: renderIcon(ArchiveOutline)
  },
  {
    label: '系统日志',
    key: 'logs',
    icon: renderIcon(DocumentTextOutline)
  }
]

const userOptions = [
  {
    label: '配置向导',
    key: 'onboarding',
    icon: renderIcon(RocketOutline)
  },
  {
    label: '个人资料',
    key: 'profile',
    icon: renderIcon(PersonOutline)
  },
  {
    label: '退出登录',
    key: 'logout',
    icon: renderIcon(LogOutOutline)
  }
]

const handleMenuSelect = (key: string) => {
  router.push({ name: key })
}

const handleMobileMenuSelect = (key: string) => {
  router.push({ name: key })
  mobileDrawerVisible.value = false
}

const disconnectWebSocket = () => {
  if (wsService.value) {
    wsService.value.disconnect()
    wsService.value = null
  }
}

const handleAuthChanged = () => {
  const nextToken = getAccessToken()
  token.value = nextToken
  disconnectWebSocket()
  if (!hideShell.value) {
    initWebSocket()
  }
}

const handleAuthRequired = () => {
  disconnectWebSocket()
  if (route.name !== 'login') {
    router.replace({ name: 'login', query: { redirect: route.fullPath } })
  }
}

watch(hideShell, (isHidden) => {
  if (isHidden) {
    disconnectWebSocket()
    return
  }
  initWebSocket()
})

const handleUserSelect = async (key: string) => {
  if (key === 'onboarding') {
    await router.push({ name: 'onboarding' })
    return
  }
  if (key === 'logout') {
    disconnectWebSocket()
    await authApi.logout()
    token.value = ''
    await router.push({ name: 'login' })
  }
}

// Watch for token expiration errors
watch(() => wsStore.status, (newStatus) => {
  if (newStatus === 'error' && wsStore.lastError?.toLowerCase().includes('token')) {
    console.log('[App] Token error detected, redirecting to login')
    clearAuthTokens()
    token.value = ''
    handleAuthRequired()
  }
})
</script>

<style scoped>
/* 移动端 Header */
.mobile-header {
  height: 56px;
  padding: 0 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  position: sticky;
  top: 0;
  z-index: 100;
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
}

.mobile-header-left {
  display: flex;
  align-items: center;
}

.mobile-logo-text {
  margin-left: 8px;
  font-size: 16px;
  font-weight: bold;
  background: linear-gradient(90deg, #18a058 0%, #52c41a 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.mobile-content {
  padding: 12px;
  min-height: calc(100vh - 56px);
  overflow-y: auto;
}

/* 侧边栏菜单项悬浮效果 */
:deep(.n-menu-item) {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  border-radius: 8px;
  margin: 4px 8px;
}

:deep(.n-menu-item:hover) {
  transform: translateX(4px);
}

:deep(.n-menu-item.n-menu-item--selected) {
  background: linear-gradient(90deg, rgba(24, 160, 88, 0.1) 0%, rgba(82, 196, 26, 0.1) 100%);
  border-left: 3px solid #18a058;
}

/* 侧边栏折叠按钮美化 */
:deep(.n-layout-sider__trigger) {
  background: linear-gradient(135deg, #f5f5f5 0%, #e8e8e8 100%);
  transition: all 0.3s ease;
}

:deep(.n-layout-sider__trigger:hover) {
  background: linear-gradient(135deg, #e8e8e8 0%, #d9d9d9 100%);
}

@media (prefers-color-scheme: dark) {
  :deep(.n-layout-sider__trigger) {
    background: linear-gradient(135deg, #333 0%, #2a2a2a 100%);
  }

  :deep(.n-layout-sider__trigger:hover) {
    background: linear-gradient(135deg, #3a3a3a 0%, #333 100%);
  }
}

/* 主题切换按钮悬浮效果 */
.n-button:hover :deep(.n-icon) {
  transform: rotate(180deg);
}

/* 下拉菜单美化 */
:deep(.n-dropdown-menu) {
  border-radius: 12px;
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.15);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
}

:deep(.n-dropdown-option) {
  transition: all 0.2s ease;
  border-radius: 6px;
  margin: 2px 4px;
}

:deep(.n-dropdown-option:hover) {
  transform: translateX(4px);
}

/* 内容区域动画 */
:deep(.n-layout-content) {
  animation: fadeIn 0.5s ease-out;
}

@keyframes fadeIn {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

/* Logo 图标动画 */
.n-icon:hover {
  animation: bounce 0.6s ease;
}

@keyframes bounce {
  0%, 100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-10px);
  }
}

/* Spin animation for connecting state */
.spin-animation {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
</style>
