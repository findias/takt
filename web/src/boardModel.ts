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
  BoardInfo,
  Card,
  Column,
  Link,
  LinkedCard,
  Placement,
  Snapshot,
  OperationResult,
} from './api'

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

  return {
    info: snap.board,
    columnIds: sortedColumns.map((c) => c.id),
    columns,
    cards,
    order,
    links: snap.links,
    linked,
  }
}

function byPosition(a: { position: string }, b: { position: string }) {
  return a.position < b.position ? -1 : a.position > b.position ? 1 : 0
}

/** Применяет к базе патч, пришедший в ответе на операцию. */
export function applyPatch(base: BaseState, result: OperationResult): BaseState {
  const columns = { ...base.columns }
  let columnIds = base.columnIds
  const cards = { ...base.cards }
  const order: Record<string, string[]> = {}
  for (const [key, value] of Object.entries(base.order)) order[key] = [...value]

  for (const column of result.patch.columns ?? []) {
    const isNew = !columns[column.id]
    columns[column.id] = column
    if (isNew) {
      order[column.id] ??= []
      columnIds = [...columnIds, column.id].sort((a, b) =>
        byPosition(columns[a], columns[b]),
      )
    }
  }

  for (const card of result.patch.cards ?? []) {
    const previous = cards[card.id]
    if (previous) removeFromOrder(order, previous.columnId, card.id)
    cards[card.id] = card
    insertByPosition(order, cards, card)
  }

  for (const id of result.patch.removedCardIds ?? []) {
    const card = cards[id]
    if (card) {
      removeFromOrder(order, card.columnId, id)
      delete cards[id]
    }
  }

  return { ...base, info: { ...base.info, version: result.version }, columnIds, columns, cards, order }
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
export function limitLabel(
  count: number,
  limit: number | null,
): { label: string; over: boolean } {
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
