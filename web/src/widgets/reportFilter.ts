// Параметры выгрузки для руководства (этап 24): живут в адресе экрана
// и теми же именами уходят в запрос к серверу. Одно имя на оба конца —
// ссылка, присланная коллеге, открывает тот же отбор, а кнопка
// скачивания собирается из адреса без перевода одного словаря в другой.

export const PRIORITIES = ['highest', 'high', 'medium', 'low'] as const
export const STATES = ['queued', 'active', 'done', 'discarded'] as const
export type Priority = (typeof PRIORITIES)[number]
export type State = (typeof STATES)[number]
export type Format = 'xlsx' | 'csv' | 'json'

export type ReportFilter = {
  /** Готовый период словом: «прошлый квартал», открытый в январе,
   *  обязан дать октябрь–декабрь, а не квартал, когда ссылку сохранили.
   *  Пусто — период задан датами. */
  period: Preset | null
  from: string
  to: string
  boards: string[]
  teams: string[]
  assignees: string[]
  labels: string[]
  priorities: Priority[]
  states: State[]
  iteration: string | null
  archived: boolean
}

/** Списки в адресе — через запятую: так ссылка короче, и сервер
 *  понимает оба вида. */
const LISTS = {
  boards: 'board',
  teams: 'team',
  assignees: 'assignee',
  labels: 'label',
  priorities: 'priority',
  states: 'state',
} as const

const DAY = 24 * 60 * 60 * 1000

/** Дата вида 2026-09-24 по местному календарю: «сегодня» человека,
 *  а не UTC, иначе вечером по Москве период кончался бы вчера. */
export function isoDay(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** Период по умолчанию — последние 90 дней: окно «Потока». */
export function defaultPeriod(now: Date): { from: string; to: string } {
  return { from: isoDay(new Date(now.getTime() - 89 * DAY)), to: isoDay(now) }
}

export type Preset = 'days30' | 'days90' | 'quarter' | 'lastQuarter'
export const PRESETS: Preset[] = ['days30', 'days90', 'quarter', 'lastQuarter']

/** Готовые периоды — те, о которых спрашивает руководитель: «за месяц»,
 *  «за квартал», «за прошлый квартал». */
export function presetPeriod(preset: Preset, now: Date): { from: string; to: string } {
  const quarterStart = (year: number, q: number) => new Date(year, q * 3, 1)
  const q = Math.floor(now.getMonth() / 3)
  switch (preset) {
    case 'days30':
      return { from: isoDay(new Date(now.getTime() - 29 * DAY)), to: isoDay(now) }
    case 'days90':
      return defaultPeriod(now)
    case 'quarter':
      return { from: isoDay(quarterStart(now.getFullYear(), q)), to: isoDay(now) }
    case 'lastQuarter': {
      const start = q === 0 ? quarterStart(now.getFullYear() - 1, 3) : quarterStart(now.getFullYear(), q - 1)
      const end = new Date(quarterStart(now.getFullYear(), q).getTime() - DAY)
      return { from: isoDay(start), to: isoDay(end) }
    }
  }
}

const DATE = /^\d{4}-\d{2}-\d{2}$/

function list(query: URLSearchParams, name: string): string[] {
  return query
    .getAll(name)
    .flatMap((v) => v.split(','))
    .map((v) => v.trim())
    .filter(Boolean)
}

export function parseReportFilter(query: URLSearchParams, now: Date): ReportFilter {
  const from = query.get('from') ?? ''
  const to = query.get('to') ?? ''
  const named = query.get('period') as Preset | null
  // Период словом сильнее дат; без того и другого — 90 дней, тоже
  // словом: сохранённый так срез скользит вместе с календарём.
  const dated = DATE.test(from) && DATE.test(to)
  const period = named && PRESETS.includes(named) ? named : dated ? null : 'days90'
  const dates = period ? presetPeriod(period, now) : { from, to }
  return {
    period,
    from: dates.from,
    to: dates.to,
    boards: list(query, LISTS.boards),
    teams: list(query, LISTS.teams),
    assignees: list(query, LISTS.assignees),
    labels: list(query, LISTS.labels),
    // Незнакомые значения отбрасываются здесь, а не на сервере:
    // иначе испорченная ссылка давала бы отказ вместо отчёта.
    priorities: list(query, LISTS.priorities).filter((v): v is Priority =>
      (PRIORITIES as readonly string[]).includes(v),
    ),
    states: list(query, LISTS.states).filter((v): v is State => (STATES as readonly string[]).includes(v)),
    iteration: query.get('iteration') || null,
    archived: query.get('archived') === '1',
  }
}

/** Параметры запроса к серверу: период всегда датами — сервер
 *  не знает, какое «сегодня» у человека. */
export function reportQuery(f: ReportFilter): URLSearchParams {
  return withSelection(new URLSearchParams({ from: f.from, to: f.to }), f)
}

/** Параметры адреса экрана и сохранённого среза: готовый период —
 *  словом, свой — датами. */
export function addressQuery(f: ReportFilter): URLSearchParams {
  return withSelection(
    new URLSearchParams(f.period ? { period: f.period } : { from: f.from, to: f.to }),
    f,
  )
}

function withSelection(q: URLSearchParams, f: ReportFilter): URLSearchParams {
  for (const [key, name] of Object.entries(LISTS) as [keyof typeof LISTS, string][]) {
    if (f[key].length > 0) q.set(name, f[key].join(','))
  }
  if (f.iteration) q.set('iteration', f.iteration)
  if (f.archived) q.set('archived', '1')
  return q
}

/** Адрес файла. Ссылкой, а не запросом из кода: браузер скачивает
 *  файл сам, потоком и со своей полосой загрузки, а не собирает
 *  десятки мегабайт в памяти вкладки. */
export function downloadHref(f: ReportFilter, format: Format): string {
  const q = reportQuery(f)
  q.set('format', format)
  return `/api/reports/cards?${q.toString()}`
}

/** Период задом наперёд — ошибка ввода, которую видно до запроса. */
export function periodReversed(f: ReportFilter): boolean {
  return f.to < f.from
}
