/**
 * Тема и плотность — личные настройки смотрящего, и живут они
 * в браузере, а не у учётной записи (решение владельца 22.09.2026):
 * они часто разные на разных устройствах — светлая на ноутбуке,
 * тёмная на телефоне. Язык, в отличие от них, хранится на сервере.
 *
 * Плотность — одна переменная `--scaling` на корне, а не второй набор
 * стилей: второй набор расходится с первым за месяц.
 *
 * Применяются при запуске (`applyStoredAppearance` в `main.tsx`),
 * а не эффектом компонента: пока их применяла кнопка в шапке, экран
 * без этой кнопки открывался в теме по умолчанию.
 */
export type Theme = 'system' | 'light' | 'dark'
export type Density = 'normal' | 'compact'

export const THEMES: Theme[] = ['system', 'light', 'dark']

// Хранилище бывает закрыто (частное окно): тогда настройка действует
// до перезагрузки, а не роняет экран.
function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Не запомнится — но на этой странице уже применено.
  }
}

export function storedTheme(): Theme {
  const v = read('theme')
  return v === 'light' || v === 'dark' ? v : 'system'
}

export function storedDensity(): Density {
  return read('density') === 'compact' ? 'compact' : 'normal'
}

export function setTheme(theme: Theme) {
  const root = document.documentElement
  if (theme === 'system') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', theme)
  write('theme', theme)
}

export function setDensity(density: Density) {
  const root = document.documentElement
  if (density === 'normal') root.removeAttribute('data-density')
  else root.setAttribute('data-density', density)
  write('density', density)
}

export function applyStoredAppearance() {
  setTheme(storedTheme())
  setDensity(storedDensity())
}
