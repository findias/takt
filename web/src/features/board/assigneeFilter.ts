import type { BaseState } from '../../entities/board/model.ts'
import { locale } from '../../shared/i18n/index.ts'

/**
 * Кого предлагать в отборе доски по исполнителю и в каком порядке.
 *
 * Прежде список брал всю организацию: на доске команды из пяти человек
 * выбор по исполнителю листал полторы сотни имён, и большинство из них
 * не могли стоять ни на одной карточке — доски они не видят. Теперь
 * в списке те, кто видит доску, и те, кто уже стоит на её карточках:
 * второе не лазейка, а работа, которую надо уметь найти, — человек мог
 * потерять доступ или уйти из организации, а карточки за ним остались.
 * Отобранный по ссылке тоже остаётся: иначе отбор действует, а в списке
 * его не видно.
 *
 * Загрузка считается по всей доске, а не по показанному: это выбор,
 * кого показать, и число, меняющееся от самого выбора, ничего бы
 * не говорило. Считается незавершённое — как в строке «В работе».
 */

export type AssigneeSort = 'name' | 'cards' | 'weight'

export const SORTS: AssigneeSort[] = ['name', 'cards', 'weight']

export type AssigneeOption = {
  userId: string
  name: string
  /** Незавершённых карточек на нём. */
  cards: number
  /** Сумма оценок незавершённых; неоценённые — в `unestimated`. */
  weight: number
  unestimated: number
  /** Доски не видит: назначить нельзя, но работа за ним есть. */
  noAccess: boolean
}

export function assigneeOptions(base: BaseState, picked: string | null): AssigneeOption[] {
  const done = new Set(
    Object.values(base.columns)
      .filter((c) => c.kind === 'done')
      .map((c) => c.id),
  )
  const rows = new Map<string, AssigneeOption>()
  const row = (userId: string) => {
    let r = rows.get(userId)
    if (!r) {
      r = {
        userId,
        name: base.people[userId] ?? '',
        cards: 0,
        weight: 0,
        unestimated: 0,
        noAccess: !(userId in base.assignable),
      }
      rows.set(userId, r)
    }
    return r
  }

  for (const userId of Object.keys(base.assignable)) row(userId)
  for (const [cardId, people] of Object.entries(base.cardAssignees)) {
    const card = base.cards[cardId]
    if (!card) continue
    const open = card.outcome !== 'done' && !done.has(card.columnId)
    for (const userId of people) {
      const r = row(userId)
      if (!open) continue
      r.cards += 1
      if (card.estimate === null) r.unestimated += 1
      else r.weight += card.estimate
    }
  }
  if (picked && picked in base.people) row(picked)

  // Без имени — человек, которого снимок не знает: показать нечего.
  return [...rows.values()].filter((r) => r.name !== '')
}

/** Новый массив в заданном порядке. При равной загрузке — по имени:
 *  иначе порядок равных менялся бы от перезагрузки к перезагрузке. */
export function sortAssignees(options: AssigneeOption[], sort: AssigneeSort): AssigneeOption[] {
  const byName = (a: AssigneeOption, b: AssigneeOption) => a.name.localeCompare(b.name, locale())
  const compare =
    sort === 'cards'
      ? (a: AssigneeOption, b: AssigneeOption) => b.cards - a.cards || b.weight - a.weight || byName(a, b)
      : sort === 'weight'
        ? (a: AssigneeOption, b: AssigneeOption) => b.weight - a.weight || b.cards - a.cards || byName(a, b)
        : byName
  return [...options].sort(compare)
}

/** Подстрока имени без учёта регистра: так же ищет поиск карточек. */
export function matchAssignee(option: AssigneeOption, text: string): boolean {
  const q = text.trim().toLocaleLowerCase(locale())
  return q === '' || option.name.toLocaleLowerCase(locale()).includes(q)
}
