import { useEffect, useState } from 'react'

type Theme = 'system' | 'light' | 'dark'
type Density = 'normal' | 'compact'

const THEMES: Record<Theme, string> = {
  system: 'Как в системе',
  light: 'Светлая',
  dark: 'Тёмная',
}

/**
 * Тема и плотность.
 *
 * Плотность — одна переменная `--scaling` на корне, а не второй набор
 * стилей: второй набор расходится с первым за месяц. Тумблера плотности
 * нет ни у Linear, ни у GitHub, ни у Atlassian — но множитель обязан
 * существовать, иначе его некуда вкрутить, когда попросят.
 */
export function Appearance() {
  const [theme, setTheme] = useState<Theme>(
    () => (localStorage.getItem('theme') as Theme) ?? 'system',
  )
  const [density, setDensity] = useState<Density>(
    () => (localStorage.getItem('density') as Density) ?? 'normal',
  )

  useEffect(() => {
    const root = document.documentElement
    if (theme === 'system') root.removeAttribute('data-theme')
    else root.setAttribute('data-theme', theme)
    localStorage.setItem('theme', theme)
  }, [theme])

  useEffect(() => {
    const root = document.documentElement
    if (density === 'normal') root.removeAttribute('data-density')
    else root.setAttribute('data-density', density)
    localStorage.setItem('density', density)
  }, [density])

  return (
    // Имя классу нужно не для оформления, а чтобы печать могла его
    // убрать: тема и плотность на бумаге не значат ничего.
    <div className="row row--tight appearance">
      <select
        value={theme}
        aria-label="Тема"
        className="small"
        onChange={(e) => setTheme(e.target.value as Theme)}
      >
        {(Object.keys(THEMES) as Theme[]).map((t) => (
          <option key={t} value={t}>
            {THEMES[t]}
          </option>
        ))}
      </select>
      <label className="row row--tight">
        <input
          type="checkbox"
          checked={density === 'compact'}
          onChange={(e) => setDensity(e.target.checked ? 'compact' : 'normal')}
        />
        <span className="small">Плотнее</span>
      </label>
    </div>
  )
}
