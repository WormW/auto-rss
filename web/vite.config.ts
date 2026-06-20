import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue(), vueJsx()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return
          }

          if (id.includes('/node_modules/vue/') || id.includes('/node_modules/@vue/')) {
            return 'vendor-vue'
          }

          if (id.includes('/node_modules/vue-router/')) {
            return 'vendor-router'
          }

          if (id.includes('/node_modules/pinia/')) {
            return 'vendor-pinia'
          }

          if (id.includes('/node_modules/@vicons/')) {
            return 'vendor-icons'
          }

          if (id.includes('/node_modules/axios/')) {
            return 'vendor-axios'
          }
        }
      }
    }
  },
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
  }
})
