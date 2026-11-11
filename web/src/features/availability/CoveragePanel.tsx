import { useState } from 'react'
import type { BaseState } from '../../entities/board/model.ts'
import type { Card } from '../../shared/api/index.ts'
import type { Availability } from '../../shared/api/availability.ts'
import { availabilityAPI } from '../../shared/api/availability.ts'
import { overlaps } from '../../entities/availability/model.ts'
import { t } from '../../shared/i18n/index.ts'
import { Field, FormError, ScreenError, useFormErrors } from '../../shared/ui/Field.tsx'
import { useAvailability } from './useAvailability.ts'
import { CoverageList } from './CoverageList.tsx'
import { VacationWarning } from './VacationWarning.tsx'

export function CoveragePanel({ base, card, boardId, meId, canEdit }: { base: BaseState; card: Card; boardId: string; meId: string; canEdit: boolean }) {
  const { data, error, refresh } = useAvailability(boardId, base.info?.version)
  const assignees = base.cardAssignees[card.id] ?? []
  const s = t.availability
  const vacations = data?.vacations.filter((v) => !v.cancelled && v.endsOn >= data.today && assignees.includes(v.userId)) ?? []
  if (data && !vacations.length && !data.coverages.some((c) => c.cardId === card.id)) return null
  return <section className="stack" aria-label={s.coverage}>
    <h3 className="section-title">{s.coverage}</h3><ScreenError>{error}</ScreenError>
    {error && <button type="button" className="link" onClick={refresh}>{s.retry}</button>}
    {data && <>
      <VacationWarning data={data} card={card} assignees={assignees} people={base.people} warningDays={base.info?.vacationWarningDays} />
      <CoverageList entries={data.coverages.filter((c) => c.cardId === card.id && c.endsOn >= data.today && c.status !== 'cancelled' && c.status !== 'declined')} people={base.people} meId={meId} today={data.today} onChange={refresh} />
      <details><summary>{s.history}</summary><CoverageList entries={data.coverages.filter((c) => c.cardId === card.id && (c.endsOn < data.today || c.status === 'cancelled' || c.status === 'declined'))} people={base.people} meId={meId} today={data.today} onChange={refresh} /></details>
      {canEdit && vacations.length > 0 && !card.doneAt && !card.outcome && <CoverageForm key={`${card.id}:${vacations.map((v) => `${v.id}:${v.version}`).join(',')}`} vacations={vacations} data={data} card={card} people={base.people} onSaved={refresh} />}
    </>}
  </section>
}


function CoverageForm({ vacations, data, card, people, onSaved }: {
  vacations: Availability['vacations']; data: Availability; card: Card; people: Record<string, string>; onSaved: () => void
}) {
  const [vacationID, setVacationID] = useState(vacations[0].id)
  const vacation = vacations.find((v) => v.id === vacationID) ?? vacations[0]
  const [sub, setSub] = useState(''), [start, setStart] = useState(vacation.startsOn), [end, setEnd] = useState(vacation.endsOn)
  const [busy, setBusy] = useState(false), [error, setError] = useState<string | null>(null)
  const [requestID, setRequestID] = useState(() => crypto.randomUUID())
  const form = useFormErrors()
  const s = t.availability
  const eligible = data.eligible.filter((id) => id !== vacation.userId && !data.vacations.some((v) => v.userId === id && !v.cancelled && overlaps(start, end, v.startsOn, v.endsOn)))
  const changed = () => { setRequestID(crypto.randomUUID()); setError(null) }
  return <form className="stack stack--tight" aria-label={s.propose} noValidate ref={form.ref} onSubmit={async (e) => {
    e.preventDefault(); if (busy) return
    const found: Record<string,string> = form.check(e.currentTarget)
    if (!start || start < vacation.startsOn || start > vacation.endsOn) found.start = s.datesInvalid
    if (!end || end < start || end > vacation.endsOn) found.end = s.datesInvalid
    if (!eligible.includes(sub)) found.sub = s.selectPerson
    if (Object.keys(found).length) { form.report(found); return }
    form.clear(); setBusy(true); setError(null)
    try {
      await availabilityAPI.propose({ id: requestID, vacationId: vacation.id, vacationVersion: vacation.version, cardId: card.id, substituteId: sub, startsOn: start, endsOn: end })
      setRequestID(crypto.randomUUID()); setSub(''); onSaved()
    } catch (e) { setError(e instanceof Error ? e.message : t.common.notDone); onSaved() }
    finally { setBusy(false) }
  }}>
    <Field label={s.vacation}>{(bind) => <select {...bind} value={vacation.id} disabled={busy} onChange={(e) => { const v = vacations.find((v) => v.id === e.target.value)!; setVacationID(v.id); setStart(v.startsOn); setEnd(v.endsOn); setSub(''); changed() }}>{vacations.map((v) => <option key={v.id} value={v.id}>{s.away(people[v.userId] ?? t.common.someone, v.startsOn, v.endsOn)}</option>)}</select>}</Field>
    <div className="plan-dates">
      <Field label={s.coverageStart} {...form.field('start')}>{(bind) => <input {...bind} name="start" type="date" required min={vacation.startsOn} max={vacation.endsOn} value={start} disabled={busy} onChange={(e) => { setStart(e.target.value); changed() }} />}</Field>
      <Field label={s.coverageEnd} {...form.field('end')}>{(bind) => <input {...bind} name="end" type="date" required min={start || vacation.startsOn} max={vacation.endsOn} value={end} disabled={busy} onChange={(e) => { setEnd(e.target.value); changed() }} />}</Field>
    </div>
    <Field label={s.substitute} {...form.field('sub')}>{(bind) => <select {...bind} name="sub" required value={eligible.includes(sub) ? sub : ''} disabled={busy} onChange={(e) => { setSub(e.target.value); changed() }}><option value="">{s.selectPerson}</option>{eligible.map((id) => <option key={id} value={id}>{people[id] ?? t.common.someone}</option>)}</select>}</Field>
    {!eligible.length && <p className="muted small">{s.noEligible}</p>}
    <FormError>{error}</FormError><button type="submit" className="btn" disabled={busy || !eligible.length}>{busy ? s.pending : s.propose}</button>
  </form>
}
