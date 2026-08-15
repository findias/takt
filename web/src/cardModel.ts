// Чистая часть карточки: во что превращаются связи для показа.
//
// Связь приходит парой идентификаторов, и половина этих идентификаторов
// может указывать куда угодно: на соседнюю карточку той же доски, на доску
// другой команды или на доску, которой спрашивающий не видит. Разложить
// это по полкам можно без сети — значит, здесь и раскладываем.

import type { Card, Link, LinkKind, LinkedCard } from './api.ts'
import type { BaseState } from './boardModel.ts'

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

/**
 * Подпись прогресса. Считается по количеству подзадач — и это временно:
 * без веса задачи «три из пяти» врут в любой команде, где задачи разного
 * размера. Вес заложен в план отдельным пунктом.
 */
export function progressLabel(card: Card): string | null {
  if (!card.progress || card.progress.total === 0) return null
  return `${card.progress.done} из ${card.progress.total}`
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
