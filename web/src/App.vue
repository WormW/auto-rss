<template>
  <n-config-provider :theme="theme" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-dialog-provider>
        <n-layout has-sider style="height: 100vh">
          <n-layout-sider
            bordered
            collapse-mode="width"
            :collapsed-width="64"
            :width="240"
            :collapsed="collapsed"
            show-trigger
            @collapse="collapsed = true"
            @expand="collapsed = false"
            style="height: 100vh"
          >
            <div style="height: 64px; display: flex; align-items: center; justify-content: center; overflow: hidden; white-space: nowrap;">
              <n-icon size="32" color="#18a058">
                <Leaf />
              </n-icon>
              <h2 v-if="!collapsed" style="margin: 0 0 0 12px; font-size: 18px; font-weight: bold;">Auto-RSS</h2>
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
            <n-layout-header bordered style="height: 64px; padding: 0 24px; display: flex; align-items: center; justify-content: space-between;">
              <!-- Left side of header (Breadcrumb or Page Title could go here) -->
              <div style="display: flex; align-items: center;">
                <!-- Placeholder for future breadcrumb -->
              </div>

              <!-- Right side of header -->
              <n-space align="center" size="large">
                <TaskManager />
                
                <n-button circle secondary @click="toggleTheme">
                  <template #icon>
                    <n-icon>
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
                    style="cursor: pointer"
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
import { ref, computed, h, onMounted } from 'vue'
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
  Moon,
  Sunny,
  Leaf,
  LogOutOutline,
  PersonOutline
} from '@vicons/ionicons5'
import TaskManager from './components/TaskManager.vue'

const router = useRouter()
const route = useRoute()

// Theme logic
const storedTheme = localStorage.getItem('theme')
const isDark = ref(storedTheme === 'dark')
const theme = computed(() => isDark.value ? darkTheme : null)

const toggleTheme = () => {
  isDark.value = !isDark.value
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

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
    label: '系统配置',
    key: 'config',
    icon: renderIcon(SettingsOutline)
  },
  {
    label: '系统日志',
    key: 'logs',
    icon: renderIcon(DocumentTextOutline)
  }
]

const userOptions = [
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

const handleUserSelect = (key: string) => {
  if (key === 'logout') {
    // Implement logout logic here
    console.log('Logout clicked')
  }
}
</script>
