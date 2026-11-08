import type { Priority } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { priorityLabel, priorityShort } from './model.ts'

/** Приоритет принадлежит самой карточке, а не её родителю.
 * Средний — умолчание; неизвестный у недоступной карточки не выдумываем. */
export function PriorityMark({ priority }: { priority?: Priority | null }) {
  if (!priority || priority === 'medium') return null
  const short = priorityShort(priority)
  const full = priorityLabel(priority)
  return (
    <span className={`priority-mark priority-mark--static priority-mark--${priority}`} title={full}>
      <span aria-hidden="true">{short}</span>
      <span className="sr-only">{t.cardView.priorityOf(short, full.toLowerCase())}</span>
    </span>
  )
}
