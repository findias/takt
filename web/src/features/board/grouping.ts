import { priorityLabel, priorityRank } from '../../entities/card/model.ts'
import type { BaseState } from '../../entities/board/model.ts'
import type { Priority } from '../../shared/api/index.ts'

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
export type Grouping = 'none' | 'assignee' | 'label' | 'iteration' | 'priority'

export const GROUPING_NAMES: Record<Grouping, string> = {
  none: 'Без группировки',
  assignee: 'По исполнителю',
  label: 'По метке',
  iteration: 'По итерации',
  priority: 'По приоритету',
}

export function parseGrouping(query: URLSearchParams): Grouping {
  const value = query.get('group')
  return value === 'assignee' || value === 'label' || value === 'iteration' || value === 'priority'
    ? value
    : 'none'
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
    grouping === 'assignee' ? 'Ни на ком' : grouping === 'label' ? 'Без метки' : 'Вне итерации'
  if (grouping !== 'priority') ensure('none', emptyTitle)

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
        for (const id of own) keys.push([id, base.people[id] ?? 'Кто-то'])
      } else if (grouping === 'label') {
        const own = base.cardLabels[cardId] ?? []
        if (own.length === 0) keys.push(['none', emptyTitle])
        for (const labelId of own) {
          const label = base.labels.find((l) => l.id === labelId)
          if (label) keys.push([label.id, label.name])
        }
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
  return list.sort((a, b) => {
    if (a.id === 'none') return 1
    if (b.id === 'none') return -1
    return a.title.localeCompare(b.title, 'ru')
  })
}
