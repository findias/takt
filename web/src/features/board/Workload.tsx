import { useMemo } from 'react'
import { Avatar } from '../../shared/ui/Avatar.tsx'
import { agingLabel } from '../../entities/board/model.ts'
import type { BaseState } from '../../entities/board/model.ts'
import { UNIT_SHORT } from '../../entities/card/model.ts'
import type { EstimateUnit } from '../../shared/api/index.ts'
import { locale, t } from '../../shared/i18n/index.ts'

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
 *
 * Человек назван именем, а не одной буквой (владелец 22.09.2026: «зачем
 * это?» — на доске после переноса стояли две «А» с разными числами),
 * и нажатие по нему отбирает доску по нему же: число на ком-то —
 * вопрос «что это за карточки», и ответ на него должен быть в одно
 * нажатие.
 */
export function Workload({
  base,
  order,
  unit,
  picked,
  onPick,
}: {
  base: BaseState
  /** Тот же порядок, что на доске: фильтр уже применён. */
  order: Record<string, string[]>
  unit: EstimateUnit
  /** Кем сейчас отобрана доска; null — никем. */
  picked: string | null
  /** Отобрать доску по человеку; null — снять отбор. */
  onPick: (userId: string | null) => void
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
      .map(([userId, row]) => ({ userId, name: base.people[userId] ?? t.common.someone, ...row }))
      .sort((a, b) => b.cards - a.cards || a.name.localeCompare(b.name, locale()))
  }, [base, order])

  if (rows.length === 0) return null

  // Имя — первым словом; тёзкам — целиком, иначе две «Анны» на доске
  // опять неразличимы.
  const first = (name: string) => name.split(/\s+/)[0] ?? name
  const namesake = new Set(
    rows.map((r) => first(r.name)).filter((n, i, all) => all.indexOf(n) !== i),
  )

  return (
    <div className="workload" role="group" aria-label={t.board.workloadLabel}>
      <span className="workload-caption small muted">{t.board.workloadCaption}</span>
      {rows.map((row) => (
        <button
          type="button"
          key={row.userId}
          className={`btn btn--quiet workload-item${row.overdue ? ' workload-item--aging' : ''}`}
          title={title(row, unit)}
          aria-pressed={picked === row.userId}
          aria-label={t.board.workloadPick(title(row, unit))}
          onClick={() => onPick(picked === row.userId ? null : row.userId)}
        >
          {/* Полный размер, а не второй план: в кружок восемнадцати
              две заглавные не входят, и мелкий кружок берёт одну
              букву — а здесь букв нужно две. Сводка отвечает
              на вопрос «сколько на ком», и одна буква на доске
              с двумя Борисами отвечает неверно. */}
          <Avatar name={row.name} />
          <span className="small">
            {namesake.has(first(row.name)) ? row.name : first(row.name)}{' '}
            <strong>{row.cards}</strong>
            {row.weight > 0 && ` · ${number(row.weight)}${row.unestimated > 0 ? '+' : ''}`}
          </span>
        </button>
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
  const parts = [t.board.workloadInWork(row.name, row.cards)]
  if (row.weight > 0) parts.push(`${number(row.weight)} ${UNIT_SHORT[unit]}`)
  if (row.unestimated > 0) parts.push(t.board.workloadUnestimated(row.unestimated))
  if (row.overdue) parts.push(t.board.workloadOverdue)
  return parts.join(', ')
}

function number(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(1)))
}
