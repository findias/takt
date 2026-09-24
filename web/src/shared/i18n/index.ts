/**
 * Язык интерфейса (ROADMAP 30.2): русский — исходный, английский — второй.
 *
 * Каталог — обычный объект: ключ → строка или функция от чисел и имён.
 * Английский обязан повторять русский ключ в ключ и функцию в функцию —
 * это проверяет TypeScript (`Catalog`), а не глаз: забытая строка
 * в каталоге второго языка — ошибка сборки, а не русское слово посреди
 * английского экрана.
 *
 * `t` — живая ссылка на выбранный каталог. Её читают и компоненты,
 * и модели без DOM (подписи ленты изменений, происхождение метки):
 * контекст React сюда не дотянулся бы, а перерисовка всей доски ради
 * смены языка, которую делают раз в жизни, незачем. Поэтому язык
 * меняется перезагрузкой страницы.
 *
 * Оба каталога — отдельные куски сборки, и нужный грузится до первой
 * отрисовки. Замер 21.09.2026: русские строки весили в основной сборке
 * 27 КБ, а ссылки на ключи добавили бы ещё около 13 — при пороге 400
 * и сборке в 398. Вынесенными они не стоят ничего тому, кто их
 * не читает.
 */
import type { ru } from './ru.ts'
import type { team } from './ru/team.ts'
import type { hooks } from './ru/hooks.ts'
import type { labelsAdmin } from './ru/labelsAdmin.ts'
import type { structure } from './ru/structure.ts'
import type { flow } from './ru/flow.ts'
import type { flowReport } from './ru/flowReport.ts'
import type { hintText } from './ru/hintText.ts'
import type { imports } from './ru/imports.ts'
import type { stand } from './ru/stand.ts'
import type { passwordLink } from './ru/passwordLink.ts'
import type { tasks } from './ru/tasks.ts'
import type { reports } from './ru/reports.ts'
import type { tree } from './ru/tree.ts'

/**
 * Разделы, которые едут со своим экраном, а не при открытии.
 *
 * Тексты «Команды», «Структуры», «Потока» раньше лежали в отдельных
 * кусках этих экранов и грузились, только когда экран открывали.
 * Единый каталог унёс их в первую загрузку — 22 КБ, которых первому
 * экрану не нужно (замер 21.09.2026). Раздел грузится вместе с кодом
 * экрана (`withSections` в `lazy`) и до его отрисовки: обратиться
 * к `t.team` раньше, чем экран загружен, нечем и незачем.
 */
type Lazy = {
  team: typeof team
  hooks: typeof hooks
  labelsAdmin: typeof labelsAdmin
  structure: typeof structure
  flow: typeof flow
  flowReport: typeof flowReport
  hintText: typeof hintText
  imports: typeof imports
  stand: typeof stand
  passwordLink: typeof passwordLink
  tasks: typeof tasks
  reports: typeof reports
  tree: typeof tree
}
export type Section = keyof Lazy
export type Catalog = typeof ru & Lazy

const SECTIONS: Record<Section, Record<Lang, () => Promise<Record<string, unknown>>>> = {
  team: { ru: () => import('./ru/team.ts'), en: () => import('./en/team.ts') },
  hooks: { ru: () => import('./ru/hooks.ts'), en: () => import('./en/hooks.ts') },
  labelsAdmin: {
    ru: () => import('./ru/labelsAdmin.ts'),
    en: () => import('./en/labelsAdmin.ts'),
  },
  structure: { ru: () => import('./ru/structure.ts'), en: () => import('./en/structure.ts') },
  flow: { ru: () => import('./ru/flow.ts'), en: () => import('./en/flow.ts') },
  flowReport: {
    ru: () => import('./ru/flowReport.ts'),
    en: () => import('./en/flowReport.ts'),
  },
  hintText: { ru: () => import('./ru/hintText.ts'), en: () => import('./en/hintText.ts') },
  imports: { ru: () => import('./ru/imports.ts'), en: () => import('./en/imports.ts') },
  stand: { ru: () => import('./ru/stand.ts'), en: () => import('./en/stand.ts') },
  passwordLink: {
    ru: () => import('./ru/passwordLink.ts'),
    en: () => import('./en/passwordLink.ts'),
  },
  tasks: { ru: () => import('./ru/tasks.ts'), en: () => import('./en/tasks.ts') },
  reports: { ru: () => import('./ru/reports.ts'), en: () => import('./en/reports.ts') },
  tree: { ru: () => import('./ru/tree.ts'), en: () => import('./en/tree.ts') },
}

/** Подгрузить разделы на выбранном языке. Повторная загрузка ничего
 *  не стоит: сборщик отдаёт уже загруженный модуль. */
export async function loadSections(...names: Section[]): Promise<void> {
  await Promise.all(
    names.map(async (name) => {
      const module = await SECTIONS[name][lang]()
      ;(t as Record<string, unknown>)[name] = module[name]
    }),
  )
}

/** Для `lazy`: код экрана и его тексты — одним ожиданием. */
export function withSections<T>(load: () => Promise<T>, ...names: Section[]): () => Promise<T> {
  return () => Promise.all([load(), loadSections(...names)]).then(([m]) => m)
}

/** Все разделы разом — для проверок, которым экраны нужны сразу. */
export const ALL_SECTIONS = Object.keys(SECTIONS) as Section[]
export type Lang = 'ru' | 'en'
export const LANGS: Lang[] = ['ru', 'en']

// Присваивается до первой отрисовки (`loadLang`) и в подготовке тестов.
// eslint-disable-next-line import/no-mutable-exports
export let t: Catalog
export let lang: Lang = 'ru'

/** Язык по умолчанию: выбранный раньше, иначе язык браузера. Русский
 *  браузер получает русский, всякий другой — английский: человек,
 *  не читающий по-русски, не найдёт на русском экране и переключателя.
 *
 *  После хранилища — cookie: её ставит и сервер при входе, если язык
 *  выбран у человека. Там, где хранилище закрыто, без неё язык
 *  учётной записи не удержался бы, и переключение по «кто я»
 *  перезагружало бы страницу по кругу. */
export function preferredLang(): Lang {
  try {
    const saved = localStorage.getItem('lang')
    if (saved === 'ru' || saved === 'en') return saved
  } catch {
    // Хранилище закрыто (частное окно) — дальше cookie и браузер.
  }
  const cookie = typeof document === 'undefined' ? null : /(?:^|; )lang=(ru|en)(?:;|$)/.exec(document.cookie)
  if (cookie) return cookie[1] as Lang
  const wanted = typeof navigator === 'undefined' ? [] : navigator.languages ?? [navigator.language]
  return wanted.some((l) => l?.toLowerCase().startsWith('ru')) ? 'ru' : 'en'
}

export async function loadLang(next: Lang): Promise<void> {
  const catalog =
    next === 'en' ? (await import('./en.ts')).en : (await import('./ru.ts')).ru
  // Копия, а не сам модуль: разделы экранов дописываются в неё
  // по мере загрузки.
  t = { ...catalog } as Catalog
  lang = next
  if (typeof document !== 'undefined') {
    document.documentElement.lang = next
    // Сервер отвечает на том же языке. Заголовок ставит сам клиент,
    // но переход браузера — возврат от провайдера входа с причиной
    // отказа — идёт без наших заголовков, и язык ему несёт cookie.
    document.cookie = `lang=${next}; path=/; max-age=31536000; samesite=lax`
  }
}

/** Сменить язык: запомнить и перезагрузить — см. выше, почему так. */
export function switchLang(next: Lang) {
  try {
    localStorage.setItem('lang', next)
  } catch {
    // Не запомнится в хранилище — запомнит cookie ниже.
  }
  document.cookie = `lang=${next}; path=/; max-age=31536000; samesite=lax`
  location.reload()
}

/**
 * Перейти на язык учётной записи, если он выбран и отличается от того,
 * на котором страница открыта. Так выбор, сделанный на работе, встречает
 * человека и дома — один раз, при первом входе на новом устройстве.
 * Возвращает, ушла ли страница на перезагрузку: рисовать под ней
 * незачем.
 */
export function followAccountLang(account: Lang | null | undefined): boolean {
  if (!account || account === lang) return false
  switchLang(account)
  return true
}

/** Язык для `Intl` и `toLocaleString`. Английский — британский: сутки
 *  по 24 часа и день перед месяцем, как у русского, — привычка
 *  читающего время на доске не должна зависеть от языка подписей. */
export function locale(): string {
  return lang === 'en' ? 'en-GB' : 'ru-RU'
}

/**
 * Словарь, который читает выбранный каталог в момент обращения.
 *
 * Названия ролей, видимостей, событий объявлены константами уровня
 * модуля, и таких обращений по коду десятки: `ROLE_NAMES[role]`,
 * `Object.entries(SCOPE_NAMES)`. Константа, собранная из `t` при
 * импорте, упала бы — каталога в этот момент ещё нет — или застыла бы
 * на языке первой загрузки. Прокси сохраняет форму обращения
 * и берёт слово тогда, когда его читают.
 */
export function live<V>(pick: () => Record<string, V>): Record<string, V> {
  return new Proxy({} as Record<string, V>, {
    get: (_, key) => (typeof key === 'string' ? pick()[key] : undefined),
    has: (_, key) => typeof key === 'string' && key in pick(),
    ownKeys: () => Reflect.ownKeys(pick()),
    getOwnPropertyDescriptor: (_, key) =>
      typeof key === 'string' && key in pick()
        ? { enumerable: true, configurable: true, value: pick()[key] }
        : undefined,
  })
}
