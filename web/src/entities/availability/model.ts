import type { Availability, Vacation } from '../../shared/api/availability.ts'
import type { Card } from '../../shared/api/index.ts'

const day = (iso: string) => Date.parse(`${iso}T00:00:00Z`) / 86400000
export const shiftDay = (iso: string, days: number) => new Date((day(iso) + days) * 86400000).toISOString().slice(0, 10)
export const overlaps = (a: string, b: string, c: string, d: string) => a <= d && c <= b

export function relevantVacations(data: Availability, card: Pick<Card, 'plannedStartOn' | 'plannedEndOn' | 'dueOn' | 'outcome' | 'doneAt'>, assignees: string[]): Vacation[] {
  if (card.outcome || card.doneAt) return []
  return data.vacations.filter((v) => !v.cancelled && assignees.includes(v.userId) && v.endsOn >= data.today && (
    v.startsOn <= data.today ||
    (card.plannedStartOn && card.plannedEndOn && overlaps(v.startsOn, v.endsOn, card.plannedStartOn, card.plannedEndOn)) ||
    (card.dueOn && card.dueOn >= v.startsOn && card.dueOn <= v.endsOn) ||
    (!card.plannedStartOn || !card.plannedEndOn)
  ))
}

// Объединяем интервалы принятого замещения: два человека в разные дни
// покрывают отпуск, но разрыв хотя бы в день остаётся видимым риском.
export function coverageLevel(data: Availability, vacation: Vacation, cardId: string): 'none' | 'partial' | 'full' {
  const intervals = data.coverages.filter((c) => c.cardId === cardId && c.vacationId === vacation.id && c.vacationVersion === vacation.version && c.status === 'accepted' && c.valid)
    .map((c) => [Math.max(day(c.startsOn), day(vacation.startsOn)), Math.min(day(c.endsOn), day(vacation.endsOn))])
    .filter(([a, b]) => a <= b).sort((a, b) => a[0] - b[0])
  if (!intervals.length) return 'none'
  let next = day(vacation.startsOn)
  for (const [a, b] of intervals) {
    if (a > next) return 'partial'
    next = Math.max(next, b + 1)
  }
  return next > day(vacation.endsOn) ? 'full' : 'partial'
}
