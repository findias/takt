// Модель доски на клиенте.
//
// Клиент — это реплика, а не форма над API. Он хранит подтверждённое
// сервером состояние (base) и очередь ещё не подтверждённых команд (queue).
// То, что видит пользователь, всегда вычисляется как
//
//     отображаемое = base + применённые по порядку команды
//
// а не «в порядке прихода ответов». Благодаря этому повтор, задержка или
// переупорядочивание ответов не могут оставить доску в странном виде.

import type {
  Label,
  BoardInfo,
  Card,
  CardField,
  Column,
  FieldValue,
  Iteration,
  Link,
  LinkedCard,
  Placement,
  Snapshot,
  OperationResult,
} from '../../shared/api/index.ts'

export type BaseState = {
  info: BoardInfo
  columnIds: string[]
  columns: Record<string, Column>
  cards: Record<string, Card>
  /** columnId → упорядоченные id карточек */
  order: Record<string, string[]>
  /** Связи карточек доски — с обеих сторон, включая чужие карточки. */
  links: Link[]
  /** Карточки с других досок, на которые ведут связи: id → карточка.
   *  Связь без этого — голый идентификатор, показать по нему нечего. */
  linked: Record<string, LinkedCard>
  iterations: Iteration[]
  /** cardId → iterationId, только открытые вхождения. */
  cardIterations: Record<string, string>
  fields: CardField[]
  fieldValues: Record<string, FieldValue[]>
  /** userId → имя. Карточка хранит идентификатор, показать надо имя. */
  people: Record<string, string>
  /** Словарь меток и то, что чем помечено. */
  labels: Label[]
  cardLabels: Record<string, string[]>
  /** cardId → исполнители в порядке назначения. Первый — тот, кто взялся
   *  первым: порядок несёт смысл и потому сохраняется. */
  cardAssignees: Record<string, string[]>
}

export type MoveCommand = {
  operationId: string
  cardId: string
  toColumnId: string
  placement: Placement
  /** откуда карточка ушла — нужно, чтобы объяснить действие в отмене и в подсказке */
  fromColumnId: string
}

export function fromSnapshot(snap: Snapshot): BaseState {
  const columns: Record<string, Column> = {}
  const order: Record<string, string[]> = {}
  const sortedColumns = [...snap.columns].sort(byPosition)
  for (const c of sortedColumns) {
    columns[c.id] = c
    order[c.id] = []
  }
  const cards: Record<string, Card> = {}
  for (const card of [...snap.cards].sort(byPosition)) {
    cards[card.id] = card
    // карточка из неизвестной колонки — признак рассогласования; молча
    // терять её нельзя, поэтому заводим список на лету
    ;(order[card.columnId] ??= []).push(card.id)
  }
  const linked: Record<string, LinkedCard> = {}
  for (const card of snap.linked) linked[card.id] = card

  const people: Record<string, string> = {}
  for (const person of snap.people) people[person.userId] = person.name

  return {
    info: snap.board,
    people,
    labels: snap.labels,
    cardLabels: snap.cardLabels,
    cardAssignees: snap.cardAssignees,
    columnIds: sortedColumns.map((c) => c.id),
    columns,
    cards,
    order,
    links: snap.links,
    linked,
    iterations: snap.iterations,
    cardIterations: snap.cardIterations,
    fields: snap.fields,
    fieldValues: snap.fieldValues,
  }
}

function byPosition(a: { position: string }, b: { position: string }) {
  return a.position < b.position ? -1 : a.position > b.position ? 1 : 0
}

/**
 * Применяет к базе патч, пришедший в ответе на операцию.
 *
 * Нетронутые части возвращаются теми же объектами, а не копиями.
 * Это не бережливость ради бережливости: список колонок и порядок
 * карточек уходят в мемоизированные компоненты, и новая ссылка на
 * неизменившиеся данные перерисовывает всю доску. На пятистах
 * карточках такая перерисовка стоила 120 мс на каждую правку.
 */
export function applyPatch(base: BaseState, result: OperationResult): BaseState {
  const patchColumns = result.patch.columns ?? []
  const patchCards = result.patch.cards ?? []
  const removed = result.patch.removedCardIds ?? []

  const columns = patchColumns.length > 0 ? { ...base.columns } : base.columns
  let columnIds = base.columnIds
  const cards = patchCards.length > 0 || removed.length > 0 ? { ...base.cards } : base.cards

  // Копируются только те колонки порядка, которых патч касается:
  // глубокая копия всего порядка на доске в пятьсот карточек — это
  // пятьсот строк на каждое чужое изменение.
  const order = { ...base.order }
  const touched = new Set<string>()
  const touch = (columnId: string) => {
    if (touched.has(columnId)) return
    touched.add(columnId)
    order[columnId] = [...(order[columnId] ?? [])]
  }

  for (const column of patchColumns) {
    const isNew = !columns[column.id]
    columns[column.id] = column
    if (isNew) {
      touch(column.id)
      columnIds = [...columnIds, column.id].sort((a, b) => byPosition(columns[a], columns[b]))
    }
  }

  for (const card of patchCards) {
    const previous = cards[card.id]
    if (previous) {
      touch(previous.columnId)
      removeFromOrder(order, previous.columnId, card.id)
    }
    touch(card.columnId)
    cards[card.id] = card
    insertByPosition(order, cards, card)
  }

  for (const id of removed) {
    const card = cards[id]
    if (card) {
      touch(card.columnId)
      removeFromOrder(order, card.columnId, id)
      delete cards[id]
    }
  }

  // Метки приходят списком целиком — «вот как теперь». Такой патч
  // применяется дважды без вреда, и догоняющему клиенту не нужно
  // рассуждать о порядке.
  let cardLabels = base.cardLabels
  if (result.patch.cardLabels) {
    cardLabels = { ...cardLabels, ...result.patch.cardLabels }
  }

  let cardAssignees = base.cardAssignees
  if (result.patch.cardAssignees) {
    cardAssignees = { ...cardAssignees, ...result.patch.cardAssignees }
  }

  return {
    ...base,
    info: { ...base.info, version: result.version },
    columnIds,
    columns,
    cards,
    order: touched.size > 0 ? order : base.order,
    cardLabels,
    cardAssignees,
  }
}

function removeFromOrder(order: Record<string, string[]>, columnId: string, cardId: string) {
  const list = order[columnId]
  if (!list) return
  const at = list.indexOf(cardId)
  if (at >= 0) list.splice(at, 1)
}

function insertByPosition(
  order: Record<string, string[]>,
  cards: Record<string, Card>,
  card: Card,
) {
  const list = (order[card.columnId] ??= [])
  // Позиции — строки, сортируемые как строки: место вставки ищется
  // обычным сравнением, без обращения к серверу.
  let at = list.length
  for (let i = 0; i < list.length; i++) {
    const other = cards[list[i]]
    if (other && card.position < other.position) {
      at = i
      break
    }
  }
  list.splice(at, 0, card.id)
}

/**
 * Отображаемый порядок: база плюс ещё не подтверждённые перемещения.
 * Команды применяются строго по порядку постановки в очередь.
 */
export function renderOrder(base: BaseState, queue: MoveCommand[]): Record<string, string[]> {
  if (queue.length === 0) return base.order
  const order: Record<string, string[]> = {}
  for (const [key, value] of Object.entries(base.order)) order[key] = [...value]
  for (const command of queue) applyMove(order, command)
  return order
}

function applyMove(order: Record<string, string[]>, command: MoveCommand) {
  for (const list of Object.values(order)) {
    const at = list.indexOf(command.cardId)
    if (at >= 0) list.splice(at, 1)
  }
  const target = (order[command.toColumnId] ??= [])
  const p = command.placement
  if (p.place === 'start') {
    target.unshift(command.cardId)
  } else if (p.place === 'end') {
    target.push(command.cardId)
  } else {
    const anchor = target.indexOf(p.afterCardId)
    // якорь исчез — кладём в конец; сервер всё равно ответит конфликтом
    // и пришлёт настоящий порядок
    target.splice(anchor < 0 ? target.length : anchor + 1, 0, command.cardId)
  }
}

/**
 * Пересобирает колонку по порядку, присланному сервером в ответе 409.
 * Возвращает null, если в ответе есть карточки, которых клиент не знает, —
 * тогда нужен полный снимок.
 */
export function reconcileColumn(
  base: BaseState,
  columnId: string,
  currentOrder: { id: string; position: string }[],
): BaseState | null {
  const cards = { ...base.cards }
  for (const entry of currentOrder) {
    const known = cards[entry.id]
    if (!known) return null
    cards[entry.id] = { ...known, columnId, position: entry.position }
  }
  const order: Record<string, string[]> = {}
  for (const [key, value] of Object.entries(base.order)) {
    order[key] = value.filter((id) => !currentOrder.some((e) => e.id === id))
  }
  order[columnId] = currentOrder.map((e) => e.id)
  return { ...base, cards, order }
}

/** Как показать счётчик колонки: число или «сколько/лимит», и превышен ли он.
 *
 * Превышение — сигнал команде, а не запрет: мягкий лимит только красит
 * счётчик. Отказывает в переполнении только жёсткий лимит, и делает это
 * сервер.
 */
export function limitLabel(count: number, limit: number | null): { label: string; over: boolean } {
  if (limit === null) return { label: String(count), over: false }
  return { label: `${count}/${limit}`, over: count > limit }
}

/** Что означает введённое в поле лимита значение.
 *
 * Пустая строка снимает лимит — иначе его нельзя было бы убрать. Мусор
 * и значения меньше единицы игнорируются молча: пользователь ещё печатает,
 * ругаться на промежуточное состояние незачем. Совпадение с текущим
 * значением тоже ничего не меняет — лишняя операция не нужна.
 */
export function parseLimitDraft(
  draft: string,
  current: number | null,
): { change: false } | { change: true; limit: number | null } {
  const trimmed = draft.trim()
  if (trimmed === '') {
    return current === null ? { change: false } : { change: true, limit: null }
  }
  const next = Number(trimmed)
  if (!Number.isInteger(next) || next < 1 || next === current) return { change: false }
  return { change: true, limit: next }
}

/** Подписи разметки колонки: что она означает для потока. */
export function flowMarks(column: Column): string[] {
  const marks: string[] = []
  if (column.isStartedPoint) marks.push('начало работы')
  if (column.isFinishedPoint) marks.push('финиш')
  if (column.wipLimitHard && column.wipLimit !== null) marks.push('жёсткий лимит')
  return marks
}

/**
 * Чего не хватает доске, чтобы считались метрики потока.
 *
 * Журнал переходов копится с первого дня, но по нему нельзя посчитать ни
 * время цикла, ни возраст карточки, пока не сказано, какой переход означает
 * «работа началась», а какой — «работа закончена». Разметить колонки можно
 * когда угодно, а ответить за прошлое — нет: события, записанные до
 * разметки, так и останутся без ответа. Поэтому об этом лучше сказать
 * сразу, а не в тот день, когда впервые понадобится отчёт.
 */
export function flowIssues(columns: Column[]): string[] {
  const live = columns.filter((c) => c.kind !== undefined)
  if (live.length === 0) return []

  const issues: string[] = []
  if (!live.some((c) => c.isStartedPoint)) {
    issues.push('Не отмечено начало работы: время цикла и возраст карточек не посчитаются')
  }
  if (!live.some((c) => c.isFinishedPoint)) {
    issues.push('Не отмечен финиш: пропускная способность и время цикла не посчитаются')
  }

  // Финиш левее начала — не запрет, а почти наверняка ошибка разметки:
  // работа считалась бы законченной раньше, чем началась.
  const start = live.findIndex((c) => c.isStartedPoint)
  const finish = live.findIndex((c) => c.isFinishedPoint)
  if (start >= 0 && finish >= 0 && finish < start) {
    issues.push('Финиш стоит раньше начала работы: проверьте разметку колонок')
  }
  return issues
}

/**
 * Сколько карточка идёт.
 *
 * Считается от начала работы, а не от создания: неначатая карточка
 * не стареет, она ждёт, и это разные вещи. Смешать их в одно число
 * значит сказать про месяц в бэклоге то же, что про месяц в работе.
 *
 * Завершённая останавливает счёт на финише: её возраст — уже история,
 * а не растущая величина.
 */
export function ageDays(
  card: Pick<Card, 'startedAt' | 'finishedAt'>,
  now: number = Date.now(),
): number | null {
  if (!card.startedAt) return null
  const end = card.finishedAt ? Date.parse(card.finishedAt) : now
  return (end - Date.parse(card.startedAt)) / 86_400_000
}

/**
 * Возраст словами — тихая пометка, которая стоит у каждой начатой
 * карточки.
 *
 * Прежде возраст показывался только у перешагнувших обещание, и довод
 * был такой: постоянный счётчик — ещё одно поле, которое перестают
 * замечать через день. Довод неверный. Возраст работы — главное число
 * канбана, и показывать его только тогда, когда стало поздно, значит
 * лишить человека единственного способа заметить, что дело идёт к тому.
 * «Восемь дней при обещанных десяти» — это разговор на планёрке,
 * а «одиннадцать при десяти» — уже разбор.
 *
 * Незаметность решается не отсутствием, а тишиной: возраст стоит
 * последним в ряду пометок, самым мелким и приглушённым. Кричит
 * по-прежнему только превышение — своей пометкой и своим цветом.
 */
export function ageLabel(
  card: Pick<Card, 'startedAt' | 'finishedAt'>,
  now: number = Date.now(),
): string | null {
  const days = ageDays(card, now)
  if (days === null) return null
  // До десяти дней — с десятой долей: разница между «полдня» и «два
  // дня» в начале работы важнее, чем между 24 и 25 днями в конце.
  return `${days < 10 ? days.toFixed(1) : Math.round(days)} дн.`
}

/**
 * Возраст карточки против обещания доски.
 *
 * Kanban Guide требует не давать работе стареть незаметно и сравнивать
 * возраст именно с обещанием: «ensuring work items do not age
 * unnecessarily, using the SLE as a reference». Сравнивать с текущим
 * процентилем нельзя — он считается по завершённым и едет вместе с ними,
 * то есть меряет тем же тестом, из которого сделан.
 */
export function agingLabel(
  card: Pick<Card, 'startedAt' | 'finishedAt'>,
  sleDays: number | null,
  now: number = Date.now(),
): string | null {
  if (!sleDays || !card.startedAt || card.finishedAt) return null
  const days = (now - Date.parse(card.startedAt)) / 86_400_000
  if (!(days > sleDays)) return null
  return `Идёт ${Math.floor(days)} дн. — дольше обещанных ${sleDays}`
}

/**
 * Порядок колонок без тех карточек, которые видны как чьи-то части.
 *
 * Подзадача у нас — обычная карточка: только так её можно отдать другой
 * команде, оценить, обсудить и провести по потоку. Но на доске родителя
 * она стояла дважды — своей строкой в колонке и списком внутри
 * родителя, — и колонка из трёх задач выглядела колонкой из десяти.
 * Часть перестала читаться частью.
 *
 * Прячется только та часть, чей родитель на этом же экране виден:
 * иначе работа исчезла бы совсем. Отбор, спрятавший родителя, возвращает
 * часть в колонку — она уже никуда не вложена, и её строка называет
 * родителя сама.
 *
 * Счётчик колонки и лимит одновременной работы считают по-прежнему всё:
 * спрятанная часть — идущая работа, и вычесть её из ограничения значило
 * бы сломать главный механизм канбана ради вида.
 */
export function withoutParts(
  base: BaseState,
  order: Record<string, string[]>,
): {
  order: Record<string, string[]>
  parts: Record<string, number>
  /** Сами спрятанные части, колонка → id. Нужны дорожкам: части
   *  раскладываются по ним так же, как карточки, иначе счётчик каждой
   *  дорожки показывал бы все части доски разом. */
  partIds: Record<string, string[]>
} {
  const parentOf = new Map<string, string>()
  for (const link of base.links) {
    if (link.kind === 'subtask' && base.cards[link.toCard]) {
      parentOf.set(link.toCard, link.fromCard)
    }
  }
  if (parentOf.size === 0) return { order, parts: {}, partIds: {} }

  const shown = new Set<string>()
  for (const ids of Object.values(order)) for (const id of ids) shown.add(id)

  const next: Record<string, string[]> = {}
  const parts: Record<string, number> = {}
  const partIds: Record<string, string[]> = {}
  for (const [columnId, ids] of Object.entries(order)) {
    const kept = ids.filter((id) => {
      const parent = parentOf.get(id)
      return parent === undefined || !shown.has(parent)
    })
    next[columnId] = kept
    if (kept.length !== ids.length) {
      parts[columnId] = ids.length - kept.length
      partIds[columnId] = ids.filter((id) => !kept.includes(id))
    }
  }
  return { order: next, parts, partIds }
}
