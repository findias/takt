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
    done: false,
    blocked: false,
    reachable: false,
    onThisBoard: false,
  }
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
