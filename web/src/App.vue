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
            <n-layout-header bordered style="height: 64px; padding: 0 24px; display: flex; align-items: center; justify-content: space-between; backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px); transition: all 0.3s ease;">
              <!-- Left side of header (Breadcrumb or Page Title could go here) -->
              <div style="display: flex; align-items: center;">
                <!-- Placeholder for future breadcrumb -->
              </div>

              <!-- Right side of header -->
              <n-space align="center" size="large">
                <TaskManager />

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

<style scoped>
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
</style>
