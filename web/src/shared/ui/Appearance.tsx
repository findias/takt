import { useEffect, useState } from 'react'
import { Menu } from './Menu.tsx'
import { ContrastIcon } from './icons.tsx'
import { LANGS, lang, switchLang, t } from '../i18n/index.ts'

type Theme = 'system' | 'light' | 'dark'
type Density = 'normal' | 'compact'

const THEMES: Theme[] = ['system', 'light', 'dark']

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

  // Одна кнопка с меню, а не список и флажок в каждой шапке. Тема
  // и плотность — личные настройки смотрящего, их меняют раз в месяц,
  // а занимали они два места в шапке каждой доски — рядом с тем,
  // что меняют каждую минуту (разбор 21.09.2026).
  const label = t.appearance.label(t.appearance[theme], density === 'compact')
  return (
    // Имя классу нужно не для оформления, а чтобы печать могла его
    // убрать: тема и плотность на бумаге не значат ничего.
    <div className="appearance">
      <Menu
        label={label}
        align="right"
        items={[
          ...THEMES.map((th) => ({
            id: th,
            label: t.appearance[th],
            checked: theme === th,
            radio: true,
            onSelect: () => setTheme(th),
          })),
          {
            id: 'density',
            label: t.appearance.compact,
            checked: density === 'compact',
            onSelect: () => setDensity(density === 'compact' ? 'normal' : 'compact'),
          },
          // Язык — здесь же: это такая же личная настройка смотрящего,
          // как тема. Названия языков — каждое на своём языке: человек,
          // не читающий по-русски, ищет «English», а не «Английский».
          // Подсказка «язык» отделяет эти два пункта от тем, которые
          // тоже выбираются одним из нескольких.
          ...LANGS.map((l) => ({
            id: `lang-${l}`,
            label: t.lang[l],
            hint: t.lang.menu,
            checked: lang === l,
            radio: true,
            onSelect: () => {
              if (l !== lang) switchLang(l)
            },
          })),
        ]}
      >
        <ContrastIcon />
      </Menu>
    </div>
  )
}
