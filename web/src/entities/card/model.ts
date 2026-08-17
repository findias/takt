// Чистая часть карточки: во что превращаются связи для показа.
//
// Связь приходит парой идентификаторов, и половина этих идентификаторов
// может указывать куда угодно: на соседнюю карточку той же доски, на доску
// другой команды или на доску, которой спрашивающий не видит. Разложить
// это по полкам можно без сети — значит, здесь и раскладываем.

import type { Card, EstimateUnit, Link, LinkKind, LinkedCard } from '../../shared/api/index.ts'
import type { BaseState } from '../board/model.ts'

/** Куда ведёт связь и что об этом известно. */
export type Related = {
  id: string
  kind: LinkKind
  title: string
  /** Где карточка лежит: своя доска, чужая, или неизвестно. */
  where: string
  /** Что с ней происходит у соседей: «В очереди», «В работе», «Готово»,
   *  «Работу не взяли». Пусто для карточек этой доски — их положение
   *  видно на самой доске, и повторять его строкой значит показывать
   *  одно и то же дважды. */
  stage: string | null
  /** Обещание доски исполнителя словами. Ответ на «когда будет», который
   *  не требует ни поля «срок», ни переписки: он уже посчитан, и он
   *  принадлежит той доске, где работа лежит. */
  promise: string | null
  done: boolean
  blocked: boolean
  /** Ложь означает, что карточка есть, но доска недоступна: связь
   *  показываем, содержимое — нет. Скрывать саму связь неправильно:
   *  прогресс родителя её всё равно учитывает. */
  reachable: boolean
  /** Своя карточка открывается на этой же доске. */
  onThisBoard: boolean
}

export type CardDetails = {
  card: Card
  /** Родитель, если карточка — чья-то подзадача. Родитель ровно один:
   *  подзадачи образуют дерево, а не граф. */
  parent: Related | null
  subtasks: Related[]
  /** Блокирующие и смежные связи, в обе стороны. */
  related: Related[]
}

/**
 * Кто чья подзадача: cardId → родитель.
 *
 * Считается один раз на доску, а не отдельно для каждой карточки:
 * на пятистах карточках обход всех связей на каждую карточку — это
 * пятьсот обходов ради строки в две трети сантиметра.
 *
 * Родитель может лежать на другой доске: тогда берётся имя из списка
 * связанных карточек. Показывать «часть неизвестно чего» нельзя —
 * такая подпись хуже её отсутствия.
 */
export function parentsOf(
  base: BaseState,
): Record<string, { id: string; title: string; onThisBoard: boolean }> {
  const parents: Record<string, { id: string; title: string; onThisBoard: boolean }> = {}
  for (const link of base.links) {
    if (link.kind !== 'subtask') continue
    const own = base.cards[link.fromCard]
    const foreign = base.linked[link.fromCard]
    const title = own?.title ?? foreign?.title
    if (!title) continue
    parents[link.toCard] = { id: link.fromCard, title, onThisBoard: Boolean(own) }
  }
  return parents
}

/** Общий пустой список: `?? []` создавал бы новый массив на каждую
 *  отрисовку и ломал бы memo у каждой карточки без подзадач, то есть
 *  почти у всех. */
export const NO_SUBTASKS: Related[] = []

/**
 * Чьи это подзадачи: cardId родителя → его подзадачи.
 *
 * Считается один раз на доску по той же причине, что и parentsOf:
 * cardDetails обходит все связи ради одной карточки, и звать его
 * из отрисовки каждой означало бы пятьсот обходов на доску.
 *
 * Порядок — тот же, что в панели карточки: по названию. Порядок
 * подзадач сам по себе смысла не несёт, а два разных порядка в двух
 * местах читаются как разные списки.
 */
export function childrenOf(base: BaseState): Record<string, Related[]> {
  const children: Record<string, Related[]> = {}
  for (const link of base.links) {
    if (link.kind !== 'subtask') continue
    // Родителя, которого нет на этой доске, пропускаем: раскрывать
    // подзадачи не у чего, а сама связь видна с карточки-подзадачи.
    if (!base.cards[link.fromCard]) continue
    ;(children[link.fromCard] ??= []).push(resolve(base, link.toCard, 'subtask'))
  }
  for (const list of Object.values(children)) {
    list.sort((a, b) => a.title.localeCompare(b.title, 'ru'))
  }
  return children
}

export function cardDetails(base: BaseState, cardId: string): CardDetails | null {
  const card = base.cards[cardId]
  if (!card) return null

  const details: CardDetails = { card, parent: null, subtasks: [], related: [] }
  for (const link of base.links) {
    if (link.fromCard === cardId) {
      const other = resolve(base, link.toCard, link.kind)
      if (link.kind === 'subtask') details.subtasks.push(other)
      else details.related.push(other)
    } else if (link.toCard === cardId) {
      const other = resolve(base, link.fromCard, link.kind)
      if (link.kind === 'subtask') details.parent = other
      else details.related.push(other)
    }
  }
  details.subtasks.sort((a, b) => a.title.localeCompare(b.title, 'ru'))
  return details
}

function resolve(base: BaseState, id: string, kind: LinkKind): Related {
  const own = base.cards[id]
  if (own) {
    return {
      id,
      kind,
      title: own.title,
      where: 'На этой доске',
      stage: null,
      promise: null,
      done: own.outcome === 'done',
      blocked: Boolean(own.blocked),
      reachable: true,
      onThisBoard: true,
    }
  }

  const foreign: LinkedCard | undefined = base.linked[id]
  if (foreign) {
    return {
      id,
      kind,
      title: foreign.title,
      where: foreign.teamName
        ? `Доска «${foreign.boardName}» · ${foreign.teamName}`
        : `Доска «${foreign.boardName}»`,
      stage: stageOf(foreign),
      // Обещание показывается, пока работа не сделана: обещание сроков
      // на завершённой работе — это ответ на вопрос, который больше
      // никто не задаёт.
      promise: foreign.archived || foreign.outcome ? null : promiseOf(foreign),
      done: foreign.outcome === 'done',
      blocked: foreign.blocked,
      reachable: true,
      onThisBoard: false,
    }
  }

  // Карточка есть — связь на неё существует, — но доска недоступна.
  return {
    id,
    kind,
    title: 'Карточка недоступна',
    where: 'В подразделении, которого вам не видно',
    stage: null,
    promise: null,
    done: false,
    blocked: false,
    reachable: false,
    onThisBoard: false,
  }
}

/**
 * Что делает с работой команда, у которой она лежит.
 *
 * Сказано про людей, а не про место, и это не украшение. Место у соседей
 * уже названо их словом — колонкой, — и назвать его вторым, своим,
 * значило бы написать «В очереди · Очередь». А вопрос, ради которого
 * сюда смотрят, про место и не спрашивает: «третью неделю лежит»
 * и «делают со вчера» — это про то, взялись или нет.
 */
const STAGES: Record<LinkedCard['columnKind'], string> = {
  queue: 'Ещё не начали',
  in_progress: 'Уже делают',
  done: 'Сделали',
}

/**
 * Что происходит с чужой карточкой: взялись ли за неё и где она стоит.
 *
 * Архив первым: убранная карточка — это отказ взять работу, и он важнее
 * того, в какой колонке она при этом стояла.
 */
function stageOf(card: LinkedCard): string {
  if (card.archived) return 'Работу не взяли'
  return `${STAGES[card.columnKind]} · ${card.columnName}`
}

/**
 * Обещание доски исполнителя словами.
 *
 * Это ответ на «когда будет», не требующий поля «срок»: канбан обещает
 * не дату, а распределение, и обещание принадлежит той доске, где работа
 * лежит. Доска без обещания молчит — подставлять ей выдуманный срок
 * значит начинать с неправды.
 */
function promiseOf(card: LinkedCard): string | null {
  if (card.sleDays === null) return null
  return `обычно ${card.sleDays} ${plural(card.sleDays, UNITS.days)} с вероятностью ${card.sleProbability}%`
}

// Названия единиц оценки живут здесь, а не в клиенте API: модель берёт
// оттуда только типы, и они стираются при сборке.
const UNITS: Record<EstimateUnit, [string, string, string]> = {
  points: ['очко', 'очка', 'очков'],
  hours: ['час', 'часа', 'часов'],
  days: ['день', 'дня', 'дней'],
}

/**
 * Подпись прогресса.
 *
 * Считается двумя способами, и подпись обязана их различать. По штукам —
 * «3 из 5»; по весу — «12 из 20 очков». Разница не косметическая: три
 * мелкие правки из пяти задач не означают, что работа сделана на
 * шестьдесят процентов, и подпись не должна это скрывать.
 */
export function progressLabel(card: Card, unit?: EstimateUnit): string | null {
  if (!card.progress || card.progress.total === 0) return null
  const { done, total, byWeight } = card.progress
  const base = `${number(done)} из ${number(total)}`
  if (!byWeight || !unit) return base
  return `${base} ${plural(total, UNITS[unit])}`
}

// Единицы называются коротко: подпись стоит вплотную к числу, и
// «очков» в каждой строке таблицы повторять незачем.
export const UNIT_SHORT: Record<EstimateUnit, string> = {
  points: 'очк.',
  hours: 'ч',
  days: 'дн.',
}

/**
 * Оценка словами.
 *
 * Пусто — значит не оценена, и это не ноль: неоценённая карточка молча
 * выпадает из веса, а нулевая обещает, что работы в ней нет.
 */
export function estimateLabel(value: number | null, unit: EstimateUnit): string | null {
  return value === null ? null : `${number(value)} ${UNIT_SHORT[unit]}`
}

/** «5 карточек» — счётом, а не числом рядом со словом: сообщение
 *  об отмене и подпись таблицы обязаны читаться вслух как речь. */
export function cardsLabel(n: number): string {
  return `${n} ${plural(n, ['карточка', 'карточки', 'карточек'])}`
}

/** Дробные оценки существуют, но «2.00» на карточке не нужно никому. */
function number(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(2)))
}

function plural(n: number, forms: [string, string, string]): string {
  // Дробное число склоняется как «часа»: 1.5 часа, 2.5 часа.
  if (!Number.isInteger(n)) return forms[1]
  const mod100 = n % 100
  const mod10 = n % 10
  if (mod100 >= 11 && mod100 <= 14) return forms[2]
  if (mod10 === 1) return forms[0]
  if (mod10 >= 2 && mod10 <= 4) return forms[1]
  return forms[2]
}

/** Доля выполненного от нуля до единицы — для полоски. */
export function progressRatio(card: Card): number {
  if (!card.progress || card.progress.total === 0) return 0
  return card.progress.done / card.progress.total
}

/**
 * Кого можно предложить в подзадачи: карточки этой доски, кроме самой
 * карточки, её текущих подзадач и её родителя.
 *
 * Второй родитель у подзадачи невозможен — база откажет, — и предлагать
 * такое значит обещать невыполнимое.
 */
export function candidatesForSubtask(base: BaseState, details: CardDetails): Card[] {
  const taken = new Set<string>([details.card.id])
  for (const s of details.subtasks) taken.add(s.id)
  if (details.parent) taken.add(details.parent.id)

  const hasParent = new Set(
    base.links.filter((l: Link) => l.kind === 'subtask').map((l) => l.toCard),
  )

  return Object.values(base.cards)
    .filter((c) => !taken.has(c.id) && !hasParent.has(c.id))
    .sort((a, b) => a.title.localeCompare(b.title, 'ru'))
}
