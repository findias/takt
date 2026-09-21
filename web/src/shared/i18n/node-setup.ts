// Каталог языка для моделей, которые проверяет встроенный запускатель
// node (`npm test`): у приложения его грузит main.tsx, здесь — preload.
import { loadLang } from './index.ts'

await loadLang('ru')
