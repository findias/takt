import { useRef, useState } from 'react'
import { ApiError } from '../../shared/api/index.ts'
import type { Card, CardPlan, SaveCardPlan } from '../../shared/api/index.ts'
import { Field, FormError } from '../../shared/ui/Field.tsx'
import { t } from '../../shared/i18n/index.ts'
import { planOf, validPlan } from './model.ts'

export function PlanEditor({ card, canEdit, onSave }: { card: Card; canEdit: boolean; onSave: SaveCardPlan }) {
  const [draft, setDraft] = useState<CardPlan | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const original = useRef<CardPlan | null>(null)
  const value = draft ?? planOf(card)
  const commit = async (next = draft) => {
    if (!next || busy) return
    if (!validPlan(next)) { setError(t.gantt.invalid); return }
    const expected = original.current ?? planOf(card)
    if (next.plannedStartOn === expected.plannedStartOn && next.plannedEndOn === expected.plannedEndOn) { setDraft(null); return }
    setBusy(true)
    try {
      await onSave(card.id, next, expected)
      setDraft(null); original.current = null; setError(null)
    } catch (e) {
      if (e instanceof ApiError && e.body?.code === 'plan_changed') { setDraft(null); original.current = null }
      setError(e instanceof Error ? e.message : t.gantt.failed)
    } finally { setBusy(false) }
  }
  if (!canEdit) return <p className="muted small">{t.gantt.plan}: {value.plannedStartOn || value.plannedEndOn ? t.gantt.range(value.plannedStartOn ?? '', value.plannedEndOn ?? '') : t.gantt.noDates}</p>
  return (
    <section className="stack stack--tight plan-editor" aria-label={t.gantt.plan} aria-busy={busy}
      onKeyDown={(e) => {
        if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLElement).blur() }
        if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setDraft(null); original.current = null; setError(null) }
      }}>
      <h3 className="section-title">{t.gantt.plan}</h3>
      <div className="plan-dates" onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) void commit() }}>
        {(['plannedStartOn', 'plannedEndOn'] as const).map((key) => (
          <Field key={key} label={key === 'plannedStartOn' ? t.gantt.start : t.gantt.end} error={key === 'plannedEndOn' ? error : null}>
            {(bind) => <input {...bind} type="date" min="0001-01-01" max="9999-12-31" value={value[key] ?? ''} disabled={busy}
              onChange={(e) => {
                if (!draft || error) original.current = planOf(card)
                setDraft({ ...value, [key]: e.target.value || null }); setError(null)
              }} />}
          </Field>
        ))}
      </div>
      <FormError>{busy ? t.gantt.saving : null}</FormError>
      {(value.plannedStartOn || value.plannedEndOn) && <button type="button" className="link" disabled={busy}
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => {
          original.current = planOf(card)
          const next = { plannedStartOn: null, plannedEndOn: null }
          setDraft(next); void commit(next)
        }}>{t.gantt.clear}</button>}
    </section>
  )
}
