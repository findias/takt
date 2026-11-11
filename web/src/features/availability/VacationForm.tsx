import { useState } from 'react'
import type { Vacation } from '../../shared/api/availability.ts'
import { availabilityAPI } from '../../shared/api/availability.ts'
import { t } from '../../shared/i18n/index.ts'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { Button } from '../../shared/ui/Button.tsx'

export function VacationForm({ value, onSaved, onClose }: { value: Vacation; onSaved: () => void; onClose: () => void }) {
  const [draft, setDraft] = useState(value)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const form = useFormErrors()
  const s = t.availability
  return <form className="stack vacation-form" aria-label={value.version ? s.edit : s.add} noValidate ref={form.ref} onSubmit={async (e) => {
    e.preventDefault(); if (busy) return
    const found: Record<string,string> = form.check(e.currentTarget)
    if (!draft.startsOn) found.start = s.datesInvalid
    if (!draft.endsOn || draft.endsOn < draft.startsOn) found.end = s.datesInvalid
    if (Object.keys(found).length) { form.report(found); return }
    form.clear(); setBusy(true); setError(null)
    try { await availabilityAPI.save(draft); onSaved() }
    catch (e) { setError(e instanceof Error ? e.message : t.common.notDone) }
    finally { setBusy(false) }
  }}>
    <h3>{value.version ? s.edit : s.add}</h3>
    <div className="plan-dates">
      <Field label={s.start} {...form.field('start')}>{(bind) => <input {...bind} name="start" type="date" min="0001-01-01" max="9999-12-31" required value={draft.startsOn} disabled={busy} onChange={(e) => { setDraft({ ...draft, startsOn: e.target.value }); setError(null) }} />}</Field>
      <Field label={s.end} {...form.field('end')}>{(bind) => <input {...bind} name="end" type="date" min={draft.startsOn || '0001-01-01'} max="9999-12-31" required value={draft.endsOn} disabled={busy} onChange={(e) => { setDraft({ ...draft, endsOn: e.target.value }); setError(null) }} />}</Field>
    </div>
    <Field label={s.note} hint={s.noteHint}>{(bind) => <textarea {...bind} name="note" maxLength={500} value={draft.note} disabled={busy} onChange={(e) => setDraft({ ...draft, note: e.target.value })} />}</Field>
    <FormError>{error}</FormError>
    <div className="row"><Button type="submit" disabled={busy}>{busy ? s.pending : s.save}</Button><Button kind="quiet" onClick={onClose}>{t.common.cancel}</Button></div>
  </form>
}
