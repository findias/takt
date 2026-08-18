import type { Card } from '../../shared/api/index.ts'
import { agingLabel } from '../../entities/board/model.ts'
import { dueIsHot } from '../../entities/card/model.ts'

/**
 * Что показывать на доске.
 *
 * Фильтры — состояние адреса, а не компонента: если после перезагрузки
 * человек должен увидеть то же самое, это живёт в URL. Отсюда же
 * следует, что отфильтрованный вид можно прислать коллеге ссылкой,
 * и это половина смысла фильтров вообще.
 *
 * Разбор и сборка — чистые функции: их проверяют без браузера, а вся
 * работа с адресом сводится к двум вызовам.
 */
export type Filters = {
  /** Подстрока в названии и описании. Пусто — не фильтруем.
   *
   *  Именно подстрока: словоформы поиск не знает, «смета» не найдёт
   *  «смету», а «смет» найдёт оба. Морфология была бы отдельным
   *  продуктом, а подстроки хватает — так же ищут Jira и Linear. */
  text: string
  /** Идентификатор человека, `none` — «ни на ком». */
  assignee: string | null
  /** Идентификаторы меток: карточка должна иметь все выбранные.
   *
   *  Именно все, а не любую: «срочно И снаружи» — осмысленный запрос,
   *  «срочно ИЛИ снаружи» почти всегда означает, что человек хотел
   *  первое и промахнулся. Так же решено в Jira и GitHub Projects. */
  labels: string[]
  /** Только заблокированные. */
  blocked: boolean
  /** Только те, что идут дольше обещанного. */
  aging: boolean
  /** Только высокий и наивысший. Отбор один, а не выбор уровня:
   *  спрашивают «что у нас горит», а не «покажи низкие». */
  urgent: boolean
  /** Только те, у кого срок сегодня, завтра, послезавтра или прошёл.
   *  Отбор про обещанное наружу — тем и отличается от «Дольше
   *  обещанного», которое про обещание доски. */
  due: boolean
  /** Идентификатор итерации, `none` — «не в итерации». Итерация —
   *  это ответ на «к чему это привязано снаружи», и без отбора по ней
   *  спринт нельзя увидеть на доске: он живёт только в отчёте. */
  iteration: string | null
}

export const EMPTY: Filters = {
  text: '',
  assignee: null,
  labels: [],
  blocked: false,
  aging: false,
  urgent: false,
  due: false,
  iteration: null,
}

/** «Ни на ком» — тоже ответ на вопрос «чьё это», и его надо уметь
 *  спросить: работа без исполнителя и есть то, что теряется. */
export const UNASSIGNED = 'none'

/** «Не в итерации» — по той же причине, что и «ни на ком»: работа,
 *  которую никуда не положили, и есть та, что теряется. */
export const NO_ITERATION = 'none'

export function isEmpty(f: Filters): boolean {
  return (
    f.text.trim() === '' &&
    f.assignee === null &&
    f.labels.length === 0 &&
    !f.blocked &&
    !f.aging &&
    !f.urgent &&
    !f.due &&
    f.iteration === null
  )
}

/**
 * Сколько отборов действует, не считая поиска.
 *
 * Нужно на телефоне: там всё, кроме поиска, убрано под кнопку, и число
 * на ней заменяет вид самой полосы. Правило «спрятанный фильтр — это
 * забытый фильтр» держится именно на этом числе, а не на видимости
 * полей. Поиск не считается: его строка остаётся на виду и говорит
 * о себе сама.
 */
export function activeCount(f: Filters): number {
  return (
    (f.assignee === null ? 0 : 1) +
    f.labels.length +
    (f.blocked ? 1 : 0) +
    (f.aging ? 1 : 0) +
    (f.urgent ? 1 : 0) +
    (f.due ? 1 : 0) +
    (f.iteration === null ? 0 : 1)
  )
}

export function parseFilters(query: URLSearchParams): Filters {
  const labels = query.get('labels')
  return {
    text: query.get('q') ?? '',
    assignee: query.get('assignee'),
    labels: labels ? labels.split(',').filter(Boolean) : [],
    blocked: query.get('blocked') === '1',
    aging: query.get('aging') === '1',
    urgent: query.get('urgent') === '1',
    due: query.get('due') === '1',
    iteration: query.get('iteration'),
  }
}

/**
 * Обратно в адрес. Пустые значения не пишутся: адрес с хвостом
 * `?q=&assignee=&labels=` выглядит как поломка и мешает сравнивать
 * ссылки глазами.
 */
export function filtersToQuery(f: Filters, base?: URLSearchParams): URLSearchParams {
  const query = new URLSearchParams(base)
  const set = (key: string, value: string | null) => {
    if (value) query.set(key, value)
    else query.delete(key)
  }
  set('q', f.text.trim())
  set('assignee', f.assignee)
  set('labels', f.labels.join(','))
  set('blocked', f.blocked ? '1' : null)
  set('aging', f.aging ? '1' : null)
  set('urgent', f.urgent ? '1' : null)
  set('due', f.due ? '1' : null)
  set('iteration', f.iteration)
  return query
}

/** Что нужно знать о карточке сверх её самой, чтобы решить судьбу. */
export type FilterContext = {
  labelsOf: (cardId: string) => string[]
  /** В какой итерации карточка сейчас. Пусто — ни в какой. */
  iterationOf: (cardId: string) => string | undefined
  /** Исполнители карточки: их несколько, и «на мне» значит «я среди них». */
  assigneesOf: (cardId: string) => string[]
  /** Стоит ли хоть одна её часть. Отбор «заблокированные» спрашивает
   *  «что не идёт», а работа не идёт и тогда, когда упёрлась её часть. */
  partsBlocked: (cardId: string) => boolean
  sleDays: number | null
  now?: number
}

export function matches(card: Card, f: Filters, ctx: FilterContext): boolean {
  const text = f.text.trim().toLowerCase()
  if (text) {
    // Номер входит в область поиска первым: за ним сюда и приходят.
    // Человек, которому в переписке прислали ПРО-142, вводит именно
    // это — и ожидает одну карточку, а не пустой экран.
    const haystack = `${card.number}\n${card.title}\n${card.description}`.toLowerCase()
    if (!haystack.includes(text)) return false
  }

  if (f.assignee) {
    const own = ctx.assigneesOf(card.id)
    // «Ни на ком» — тоже ответ на вопрос «чьё это»: работа без
    // исполнителя и есть то, что теряется.
    if (f.assignee === UNASSIGNED ? own.length > 0 : !own.includes(f.assignee)) {
      return false
    }
  }

  if (f.labels.length > 0) {
    const own = new Set(ctx.labelsOf(card.id))
    if (!f.labels.every((id) => own.has(id))) return false
  }

  // Заблокирована сама или стоит её часть: и то и другое — ответ
  // на «что не идёт».
  if (f.blocked && !card.blocked && !ctx.partsBlocked(card.id)) return false

  if (f.aging && !agingLabel(card, ctx.sleDays, ctx.now)) return false

  // «Горит» — это высокий и наивысший: спрашивают про верх шкалы,
  // а не про конкретный уровень.
  if (f.urgent && card.priority !== 'high' && card.priority !== 'highest') return false

  if (f.due && !dueIsHot(card.dueOn, ctx.now === undefined ? undefined : new Date(ctx.now))) {
    return false
  }

  if (f.iteration) {
    const own = ctx.iterationOf(card.id)
    // «Не в итерации» — тоже ответ: незапланированная работа и есть
    // то, что съедает спринт незаметно.
    if (f.iteration === NO_ITERATION ? own !== undefined : own !== f.iteration) return false
  }

  return true
}
