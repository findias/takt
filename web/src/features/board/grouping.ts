import { priorityLabel, priorityRank, progressLabel } from '../../entities/card/model.ts'
import type { BaseState } from '../../entities/board/model.ts'
import type { Priority } from '../../shared/api/index.ts'
import { live, locale, t } from '../../shared/i18n/index.ts'

/**
 * Группировка доски по горизонтали — то, что в канбане называют
 * дорожками.
 *
 * В правилах дизайна записано, что дорожками обычно подменяют
 * недостающие поля карточки, и это верно: доска без исполнителя
 * и меток заводит «дорожку Пети» вместо поля «исполнитель». У нас поля
 * уже есть, поэтому дорожка здесь — способ посмотреть на те же данные
 * под другим углом, а не костыль вместо модели. Отсюда и правило:
 * группировка всегда выводится из полей, своей сущности «дорожка»
 * в схеме нет и не будет.
 *
 * Группировка — состояние адреса, как и фильтры: сгруппированный вид
 * посылают ссылкой.
 */
export type Grouping = 'none' | 'assignee' | 'label' | 'iteration' | 'priority' | 'parent' | 'root'

const GROUPINGS: Grouping[] = ['assignee', 'label', 'iteration', 'priority', 'parent', 'root']

/** Группировки по дереву работы: в них подзадача стоит в дорожке
 *  своего родителя, а не прячется внутри его карточки. */
export function byTree(grouping: Grouping): boolean {
  return grouping === 'parent' || grouping === 'root'
}

/** Предел подъёма к корню — тот же, что у сервера (MaxSubtaskDepth):
 *  глубже дерево не бывает, а обрыв цикла здесь страхует от связей,
 *  пришедших патчем посреди правки. */
const MAX_DEPTH = 5

export const GROUPING_NAMES = live(() => t.board.grouping) as Record<Grouping, string>

export function parseGrouping(query: URLSearchParams): Grouping {
  const value = query.get('group') as Grouping | null
  return value && GROUPINGS.includes(value) ? value : 'none'
}

export function groupingToQuery(grouping: Grouping, base?: URLSearchParams): URLSearchParams {
  const query = new URLSearchParams(base)
  if (grouping === 'none') query.delete('group')
  else query.set('group', grouping)
  return query
}

export type Group = {
  id: string
  title: string
  /** Приписка к заголовку: прогресс родителя или чья это доска. */
  note?: string
  /** Карточка этой доски, по которой названа дорожка, — её открывают
   *  с заголовка. */
  cardId?: string
  /** columnId → упорядоченные id карточек этой группы. */
  order: Record<string, string[]>
  count: number
}

/**
 * Разложить порядок карточек по группам.
 *
 * Карточка попадает в несколько групп, если у неё несколько меток, —
 * и это правильно: доска отвечает на вопрос «что помечено срочным»,
 * а не «в какую единственную корзину положить». Для исполнителя
 * и итерации групп всегда одна.
 *
 * Пустые группы не показываются, кроме одной: «без исполнителя»
 * и «без итерации» остаются, потому что именно там теряется работа.
 * У приоритета такой группы нет вовсе: уровень есть у каждой карточки,
 * и «без уровня» — не состояние, а пустое место в списке дорожек.
 */
export function groupsOf(
  base: BaseState,
  order: Record<string, string[]>,
  grouping: Grouping,
): Group[] {
  if (grouping === 'none') {
    const count = Object.values(order).reduce((sum, ids) => sum + ids.length, 0)
    return [{ id: 'all', title: '', order, count }]
  }

  const groups = new Map<string, Group>()
  const ensure = (id: string, title: string): Group => {
    let group = groups.get(id)
    if (!group) {
      group = { id, title, order: {}, count: 0 }
      for (const columnId of base.columnIds) group.order[columnId] = []
      groups.set(id, group)
    }
    return group
  }

  // Пустая группа заводится заранее: работа без исполнителя, без метки
  // и вне итерации — то, ради чего на группировку и смотрят.
  const emptyTitle =
    grouping === 'assignee'
      ? t.board.nobody
      : grouping === 'label'
        ? t.board.noLabel
        : byTree(grouping)
          ? t.board.noParent
          : t.board.noIteration
  if (grouping !== 'priority') ensure('none', emptyTitle)

  // Родитель каждой карточки — одним обходом связей, как childrenOf:
  // спрашивать по карточке значило бы обходить связи пятьсот раз.
  const parentOf = new Map<string, string>()
  if (byTree(grouping)) {
    for (const link of base.links) if (link.kind === 'subtask') parentOf.set(link.toCard, link.fromCard)
  }
  const laneOf = (cardId: string): string | undefined => {
    let parent = parentOf.get(cardId)
    if (grouping === 'root') {
      // Корень — самый верхний, кого доска знает: над родителем с чужой
      // доски связей в снимке нет, и он для этой доски и есть вершина.
      for (let depth = 1; parent && depth < MAX_DEPTH; depth++) {
        const above = parentOf.get(parent)
        if (!above || above === cardId) break
        parent = above
      }
    }
    return parent
  }
  const lane = (parentId: string) => {
    const own = base.cards[parentId]
    if (own) {
      const group = ensure(parentId, `${own.number} ${own.title}`)
      group.cardId = parentId
      // Прогресс словами «готово …»: голое «0 из 3» рядом со счётчиком
      // дорожки читалось бы одним числом с ним.
      const progress = progressLabel(own)
      group.note = progress ? t.board.parentProgress(progress) : undefined
      return group
    }
    const foreign = base.linked[parentId]
    const group = ensure(parentId, foreign ? foreign.title : t.board.unknownParent)
    // Чья доска — словами: дорожку чужой карточки не открыть здесь,
    // и без приписки она читалась бы как своя, потерянная.
    if (foreign) group.note = t.board.parentOnBoard(foreign.boardName)
    return group
  }

  for (const [columnId, ids] of Object.entries(order)) {
    for (const cardId of ids) {
      const card = base.cards[cardId]
      if (!card) continue

      const keys: [string, string][] = []
      if (grouping === 'assignee') {
        // Исполнителей может быть несколько — тогда карточка попадает
        // в дорожку каждого. Так же ведёт себя группировка по меткам,
        // и по той же причине: дорожка отвечает на вопрос «что на мне»,
        // а не «чья это карточка целиком».
        const own = base.cardAssignees[cardId] ?? []
        if (own.length === 0) keys.push(['none', emptyTitle])
        for (const id of own) keys.push([id, base.people[id] ?? t.common.someone])
      } else if (grouping === 'label') {
        const own = base.cardLabels[cardId] ?? []
        if (own.length === 0) keys.push(['none', emptyTitle])
        for (const labelId of own) {
          const label = base.labels.find((l) => l.id === labelId)
          if (label) keys.push([label.id, label.name])
        }
      } else if (byTree(grouping)) {
        const parentId = laneOf(cardId)
        keys.push(parentId ? [lane(parentId).id, ''] : ['none', emptyTitle])
      } else if (grouping === 'priority') {
        // Уровень назван полно, как в таблице: дорожки сравнивают друг
        // с другом, и в заголовке нужен порядок, который виден в самом
        // слове. Короткое «горит» живёт в плашке на карточке.
        keys.push([card.priority, priorityLabel(card.priority)])
      } else {
        const iterationId = base.cardIterations[cardId]
        const iteration = base.iterations.find((i) => i.id === iterationId)
        keys.push(iteration ? [iteration.id, iteration.name] : ['none', emptyTitle])
      }

      for (const [id, title] of keys) {
        const group = ensure(id, title)
        group.order[columnId] = [...(group.order[columnId] ?? []), cardId]
        group.count += 1
      }
    }
  }

  const list = [...groups.values()].filter((g) => g.count > 0 || g.id === 'none')

  // Уровни — своим порядком, а не по алфавиту: по алфавиту «Высокий»
  // встал бы выше «Наивысшего», и дорожки перестали бы читаться сверху
  // вниз как шкала. Ради этого порядка на группировку по уровню
  // и смотрят: сверху то, что горит.
  if (grouping === 'priority') {
    return list.sort((a, b) => priorityRank(a.id as Priority) - priorityRank(b.id as Priority))
  }

  // Пустая группа — последней: сначала то, что кем-то ведётся.
  // Номера в названиях дорожек по дереву — числом: «ПОСТ-4» выше
  // «ПОСТ-11», как их и заводили.
  return list.sort((a, b) => {
    if (a.id === 'none') return 1
    if (b.id === 'none') return -1
    return a.title.localeCompare(b.title, locale(), { numeric: true })
  })
}
