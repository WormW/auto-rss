import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue(), vueJsx()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:7892',
        changeOrigin: true
      }
    }
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          const normalizedId = id.replace(/\\/g, '/')

          if (!normalizedId.includes('/node_modules/')) {
            return undefined
          }

          if (normalizedId.includes('/vue/') || normalizedId.includes('/@vue/')) {
            return 'vendor-vue'
          }
          if (normalizedId.includes('/vue-router/')) {
            return 'vendor-router'
          }
          if (normalizedId.includes('/pinia/')) {
            return 'vendor-pinia'
          }
          if (normalizedId.includes('/@vicons/')) {
            return 'vendor-icons'
          }
          if (normalizedId.includes('/axios/')) {
            return 'vendor-axios'
          }

          return undefined
        }
      }
    }
  }
})
