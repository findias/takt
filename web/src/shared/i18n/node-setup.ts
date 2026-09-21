// Каталог языка для моделей, которые проверяет встроенный запускатель
// node (`npm test`): у приложения его грузит main.tsx, здесь — preload.
import { ALL_SECTIONS, loadLang, loadSections } from './index.ts'

await loadLang('ru')
await loadSections(...ALL_SECTIONS)
