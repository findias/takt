import { useEffect } from 'react'
import { lang } from '../i18n/index.ts'

/**
 * Справка с экрана (ROADMAP 30.3): «Справка» и F1 открывают раздел про
 * тот экран, где человек сейчас, а не титульную страницу.
 *
 * Адрес — одна пара «страница#якорь» на экран: у оригинала и перевода
 * адреса и якоря общие (`<a id="…"></a>` в `docs/*.md`), а язык
 * подставляется свой. Что каждый якорь существует на обоих языках,
 * проверяет сервер (`internal/help`), разбирая этот самый файл, —
 * поэтому пары пишутся строками вида `'howto#board'` и никак иначе.
 */
export const HELP_TOPICS = {
  boards: 'quickstart#create-board',
  board: 'howto#board',
  card: 'quickstart#describe-work',
  table: 'howto#table',
  tree: 'howto#tree',
  flow: 'reference#flow',
  archive: 'howto#archive',
  iterations: 'howto#iterations',
  team: 'howto#team',
  tasks: 'howto#tasks',
  reports: 'howto#reports',
  structure: 'howto#structure',
  account: 'howto#language',
  import: 'howto#import',
  importYougile: 'howto#import-yougile',
  importPackage: 'howto#import-package',
  taktFetch: 'takt-fetch#get',
  // Понятия с «?» (ROADMAP 30.4): «Подробнее в справке» ведёт сюда.
  markup: 'howto#columns',
  limit: 'howto#columns',
  promise: 'howto#promise',
  block: 'howto#block',
  labelScope: 'howto#label-scope',
  visibility: 'howto#visibility',
  iteration: 'howto#iterations',
  cycleTime: 'reference#flow',
  age: 'reference#flow',
  accumulation: 'reference#flow',
  throughput: 'reference#flow',
  forecast: 'reference#flow',
  roles: 'reference#roles',
  subdivisionAdmin: 'reference#roles',
  keyScopes: 'reference#key-scopes',
  subscription: 'howto#subscriptions',
  export: 'howto#export',
  savedView: 'howto#filter',
  grouping: 'howto#swimlanes',
  attention: 'howto#board',
  notifications: 'howto#notifications',
} as const

export type HelpTopic = keyof typeof HELP_TOPICS

export function helpUrl(topic: HelpTopic): string {
  return `/help/${lang}/${HELP_TOPICS[topic]}`
}

/**
 * Какой экран сейчас открыт — стопкой: доска кладёт «доску», открытая
 * поверх неё карточка — «карточку», а закрывшись, снимает свою, и под
 * ней снова доска. Стопка, а не состояние React: кнопке «Справка»
 * и F1 тема нужна в момент нажатия, и перерисовывать ради неё шапку
 * при каждой смене экрана незачем.
 */
const stack: HelpTopic[] = []

export function useHelpTopic(topic: HelpTopic | null) {
  useEffect(() => {
    if (!topic) return
    stack.push(topic)
    return () => {
      const at = stack.lastIndexOf(topic)
      if (at >= 0) stack.splice(at, 1)
    }
  }, [topic])
}

export function currentHelpTopic(): HelpTopic {
  return stack[stack.length - 1] ?? 'boards'
}

/** Открыть справку по текущему экрану в новой вкладке: работа на доске
 *  не должна пропадать ради того, чтобы прочитать про неё. */
export function openHelp() {
  window.open(helpUrl(currentHelpTopic()), '_blank', 'noopener')
}

/** F1 и «?» вне поля ввода. «?» в поле — это вопросительный знак,
 *  а не просьба о помощи. */
export function isHelpKey(e: KeyboardEvent): boolean {
  if (e.key === 'F1') return true
  if (e.key !== '?' || e.ctrlKey || e.metaKey || e.altKey) return false
  const target = e.target as HTMLElement | null
  return !(
    target &&
    (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
  )
}
