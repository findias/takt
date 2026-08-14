import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// В разработке фронтенд поднимается отдельно и проксирует запросы к API,
// в продакшене статику отдаёт то же приложение на Go — отдельного сервиса
// для фронтенда нет.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8099', changeOrigin: true },
    },
  },
  build: { outDir: 'dist', emptyOutDir: true },
})
