import type { Availability } from '../../shared/api/availability.ts'
import type { Card } from '../../shared/api/index.ts'
import { coverageLevel, relevantVacations } from '../../entities/availability/model.ts'
import { t } from '../../shared/i18n/index.ts'

export function VacationWarning({ data, card, assignees, people, warningDays }: { data: Availability; card: Pick<Card, 'id' | 'plannedStartOn' | 'plannedEndOn' | 'dueOn' | 'outcome' | 'doneAt'>; assignees: string[]; people: Record<string, string>; warningDays?: number | null }) {
  const affected = relevantVacations(data, card, assignees, warningDays), s = t.availability
  const allAway = affected.some((v) => coverageLevel(data,v,card.id) !== 'full') && assignees.length > 0 && assignees.every((id) => affected.some((v) => v.userId === id && v.startsOn <= data.today && v.endsOn >= data.today))
  if (!affected.length) return null
  return <div className="vacation-warning stack stack--tight">
    {allAway && <strong>{s.allAway}</strong>}
    {affected.map((v) => <div key={v.id}><span>{s.away(people[v.userId] ?? t.common.someone, v.startsOn, v.endsOn)}</span><span className="small"> · {s[coverageLevel(data, v, card.id)]}</span></div>)}
    {(!card.plannedStartOn || !card.plannedEndOn) && <span className="small muted">{s.noDates}</span>}
  </div>
}
