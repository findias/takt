// defineConfig берётся из vitest: он тот же самый, но знает про раздел
// test. Иначе конфигурация тестов не проходит проверку типов, а «не
// проверяется типами» здесь означает «однажды молча перестанет работать».
import { defineConfig } from 'vitest/config'
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

  // Тесты компонентов и хуков живут здесь же и на той же сборке, что
  // и приложение: отдельная конфигурация для тестов расходится с рабочей
  // ровно в тот день, когда это дороже всего.
  //
  // Модели доски проверяются встроенным в node запускателем и сюда
  // не попадают — им не нужен ни DOM, ни React, и тащить их через
  // браузерное окружение значит платить за него без причины.
  test: {
    environment: 'jsdom',
    include: ['src/**/*.dom.test.tsx'],
    setupFiles: ['src/shared/lib/test-setup.ts'],
    globals: false,
    restoreMocks: true,
  },
})
