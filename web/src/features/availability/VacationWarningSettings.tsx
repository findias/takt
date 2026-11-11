import { useState } from 'react'
import { availabilityAPI } from '../../shared/api/availability.ts'
import { t } from '../../shared/i18n/index.ts'
import { Field, FormError, useFormErrors } from '../../shared/ui/Field.tsx'
import { Button } from '../../shared/ui/Button.tsx'

export function VacationWarningSettings({ boardId, days, canEdit, onSaved }: {
  boardId: string; days: number | null; canEdit: boolean; onSaved: () => void
}) {
  const [value, setValue] = useState(days === null ? '' : String(days))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const form = useFormErrors(), s = t.availability
  return <form className="stack stack--tight" aria-label={s.warningSettings} noValidate ref={form.ref} onSubmit={async (e) => {
    e.preventDefault()
    if (busy || !canEdit) return
    const next = value.trim() === '' ? null : Number(value)
    const found = form.check(e.currentTarget)
    if (next !== null && (!Number.isInteger(next) || next < 0 || next > 365)) found.days = s.warningInvalid
    if (Object.keys(found).length) { form.report(found); return }
    form.clear(); setBusy(true); setError(null); setSaved(false)
    try { await availabilityAPI.warningDays(boardId, next); setSaved(true); onSaved() }
    catch (e) { setError(e instanceof Error ? e.message : t.common.notDone) }
    finally { setBusy(false) }
  }}>
    <h3 className="section-title">{s.warningSettings}</h3>
    <Field label={s.warningDays} hint={s.warningHint} {...form.field('days')}>{(bind) =>
      <input {...bind} name="days" type="number" min={0} max={365} step={1} value={value} disabled={busy || !canEdit}
        onChange={(e) => { setValue(e.target.value); setError(null); setSaved(false) }} />
    }</Field>
    <FormError>{error}</FormError>
    {canEdit && <div className="row"><Button type="submit" busy={busy}>{busy ? s.pending : s.warningSave}</Button></div>}
    <span className="small muted" role="status">{saved ? s.warningSaved : ''}</span>
  </form>
}
