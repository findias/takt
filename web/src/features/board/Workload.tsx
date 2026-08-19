import { useMemo } from 'react'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { agingLabel } from '../../entities/board/model.ts'
import type { BaseState } from '../../entities/board/model.ts'
import { UNIT_SHORT } from '../../entities/card/model.ts'
import type { EstimateUnit } from '../../shared/api/index.ts'

/**
 * Сколько на ком висит.
 *
 * Доска отвечает на вопрос «где работа», но не на вопрос «на ком её
 * много»: чтобы это узнать, приходится считать аватары глазами по всем
 * колонкам. Строка загрузки считает за человека.
 *
 * Три решения, каждое меняет смысл числа.
 *
 * **Считается незавершённое.** Карточка в колонке финиша — не нагрузка,
 * а сделанная работа; сложив её со всем остальным, мы получили бы
 * «сколько человек сделал за всё время», то есть ответ на другой вопрос.
 *
 * **Считается показанное.** Рядом стоит «скрыто N» от фильтра, и числа,
 * посчитанные по всей доске, спорили бы с тем, что человек видит.
 *
 * **Сумма — по оценённым**, а неоценённые названы отдельно: сумма,
 * тихо пропустившая половину карточек, — это меньше, чем есть, и хуже,
 * чем отсутствие суммы.
 */
export function Workload({
  base,
  order,
  unit,
}: {
  base: BaseState
  /** Тот же порядок, что на доске: фильтр уже применён. */
  order: Record<string, string[]>
  unit: EstimateUnit
}) {
  const rows = useMemo(() => {
    const done = new Set(
      Object.values(base.columns)
        .filter((c) => c.kind === 'done')
        .map((c) => c.id),
    )
    const load = new Map<
      string,
      { cards: number; weight: number; unestimated: number; overdue: boolean }
    >()

    for (const ids of Object.values(order)) {
      for (const id of ids) {
        const card = base.cards[id]
        if (!card || card.outcome === 'done' || done.has(card.columnId)) continue
        for (const person of base.cardAssignees[id] ?? []) {
          const row = load.get(person) ?? {
            cards: 0,
            weight: 0,
            unestimated: 0,
            overdue: false,
          }
          row.cards += 1
          if (card.estimate === null) row.unestimated += 1
          else row.weight += card.estimate
          if (agingLabel(card, base.info.sleDays)) row.overdue = true
          load.set(person, row)
        }
      }
    }

    return [...load.entries()]
      .map(([userId, row]) => ({ userId, name: base.people[userId] ?? 'Кто-то', ...row }))
      .sort((a, b) => b.cards - a.cards || a.name.localeCompare(b.name, 'ru'))
  }, [base, order])

  if (rows.length === 0) return null

  return (
    <div className="workload" role="group" aria-label="Сколько на ком висит">
      {rows.map((row) => (
        <span
          key={row.userId}
          className={row.overdue ? 'workload-item workload-item--overdue' : 'workload-item'}
          title={title(row, unit)}
        >
          <Avatar name={row.name} size={18} />
          <span className="small">
            {row.cards}
            {row.weight > 0 && ` · ${number(row.weight)}${row.unestimated > 0 ? '+' : ''}`}
          </span>
        </span>
      ))}
    </div>
  )
}

/** Всё, что не поместилось в две цифры, — словами при наведении:
 *  плюс после суммы иначе читается как опечатка. */
function title(
  row: { name: string; cards: number; weight: number; unestimated: number; overdue: boolean },
  unit: EstimateUnit,
): string {
  const parts = [`${row.name}: ${row.cards} в работе`]
  if (row.weight > 0) parts.push(`${number(row.weight)} ${UNIT_SHORT[unit]}`)
  if (row.unestimated > 0) parts.push(`${row.unestimated} без оценки`)
  if (row.overdue) parts.push('есть карточка дольше обещанного')
  return parts.join(', ')
}

function number(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(1)))
}
