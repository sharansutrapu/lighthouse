import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': { target: process.env.LIGHTHOUSE_API_TARGET || 'http://127.0.0.1:8000', ws: true },
      '/auth': { target: process.env.LIGHTHOUSE_API_TARGET || 'http://127.0.0.1:8000' },
      '/ws': { target: process.env.LIGHTHOUSE_API_TARGET || 'http://127.0.0.1:8000', ws: true },
    },
  },
})
