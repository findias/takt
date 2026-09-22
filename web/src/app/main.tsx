import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App.tsx'
import { loadLang, preferredLang } from '../shared/i18n/index.ts'
import { applyStoredAppearance } from '../shared/lib/appearance.ts'
import './styles.css'

// Тема — до всего остального: страница, открывшаяся светлой и через миг
// ставшая тёмной, мигает на весь экран.
applyStoredAppearance()

// Каталог языка грузится до первой отрисовки: экран, нарисованный
// без подписей и перерисованный через миг, мигает — а мигание хуже
// ожидания (правило загрузки, frontend-ui).
void loadLang(preferredLang()).then(() =>
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  ),
)
