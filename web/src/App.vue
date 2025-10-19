<template>
  <n-config-provider :theme="theme">
    <n-message-provider>
      <n-dialog-provider>
        <n-layout style="height: 100vh">
          <n-layout-header bordered style="height: 64px; padding: 0 24px; display: flex; align-items: center">
            <h1 style="font-size: 20px; margin: 0">Auto-RSS</h1>
            <n-space style="margin-left: auto">
              <n-button @click="toggleTheme" text>
                {{ isDark ? '浅色模式' : '深色模式' }}
              </n-button>
            </n-space>
          </n-layout-header>
          <n-layout has-sider style="height: calc(100vh - 64px)">
            <n-layout-sider bordered content-style="padding: 24px">
              <n-menu
                :value="currentRoute"
                :options="menuOptions"
                @update:value="handleMenuSelect"
              />
            </n-layout-sider>
            <n-layout-content content-style="padding: 24px">
              <router-view />
            </n-layout-content>
          </n-layout>
        </n-layout>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { ref, computed, h } from 'vue'
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
  darkTheme
} from 'naive-ui'
import type { MenuOption } from 'naive-ui'

const router = useRouter()
const route = useRoute()

const isDark = ref(false)
const theme = computed(() => isDark.value ? darkTheme : null)

const currentRoute = computed(() => route.name as string)

const menuOptions: MenuOption[] = [
  {
    label: 'RSS 源',
    key: 'rss-sources'
  },
  {
    label: '订阅管理',
    key: 'subscriptions'
  },
  {
    label: '下载任务',
    key: 'downloads'
  },
  {
    label: '系统配置',
    key: 'config'
  },
  {
    label: '系统日志',
    key: 'logs'
  }
]

const toggleTheme = () => {
  isDark.value = !isDark.value
}

const handleMenuSelect = (key: string) => {
  router.push({ name: key })
}
</script>
