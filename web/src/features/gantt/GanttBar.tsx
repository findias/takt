import { useRef, useState } from 'react'
import type { Card, CardPlan, SaveCardPlan } from '../../shared/api/index.ts'
import { t } from '../../shared/i18n/index.ts'
import { completion } from '../../entities/card/model.ts'
import { barRange, changePlan, planOf } from './model.ts'

type Edge = 'move' | 'start' | 'end'
export function GanttBar({ card, from, days, dayWidth, canEdit, onSave, onError }: {
  card: Card; from: number; days: number; dayWidth: number; canEdit: boolean
  onSave: SaveCardPlan; onError: (message: string | null) => void
}) {
  const [preview, setPreview] = useState<CardPlan | null>(null)
  const [busy, setBusy] = useState(false)
  const drag = useRef<{ x: number; edge: Edge; original: CardPlan } | null>(null)
  const plan = preview ?? planOf(card)
  const range = barRange(plan, from, days) ?? (preview ? barRange(planOf(card), from, days) : null)
  const done = completion(card)
  const text = `${card.number} ${card.title}: ${t.gantt.range(plan.plannedStartOn ?? '', plan.plannedEndOn ?? '')}${done ? ` · ${done.percent}%` : ''}`
  const save = async (next: CardPlan, expected: CardPlan) => {
    if (next.plannedStartOn === expected.plannedStartOn && next.plannedEndOn === expected.plannedEndOn) { setPreview(null); return }
    setPreview(next); setBusy(true); onError(null)
    try { await onSave(card.id, next, expected) }
    catch (e) { onError(e instanceof Error ? e.message : t.gantt.failed) }
    finally { setBusy(false); setPreview(null) }
  }
  if (!range) return null
  const handle = (edge: Edge, label: string) => (
    <button type="button" className={`gantt-handle gantt-handle--${edge}`} aria-label={`${label}: ${text}`} title={text}
      disabled={busy} aria-busy={busy}
      onPointerDown={(e) => {
        if (e.button !== 0) return
        e.preventDefault(); e.currentTarget.focus(); e.currentTarget.setPointerCapture?.(e.pointerId)
        drag.current = { x: e.clientX, edge, original: planOf(card) }
      }}
      onPointerMove={(e) => {
        const d = drag.current
        if (d) setPreview(changePlan(d.original, Math.round((e.clientX - d.x) / dayWidth), d.edge))
      }}
      onPointerUp={(e) => {
        const d = drag.current
        drag.current = null
        if (!d) return
        void save(changePlan(d.original, Math.round((e.clientX - d.x) / dayWidth), d.edge), d.original)
      }}
      onPointerCancel={() => { drag.current = null; setPreview(null) }}
      onLostPointerCapture={() => { if (drag.current) { drag.current = null; setPreview(null) } }}
      onKeyDown={(e) => {
        if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); drag.current = null; setPreview(null) }
        if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && !busy && !drag.current) {
          e.preventDefault()
          const original = planOf(card)
          void save(changePlan(original, (e.key === 'ArrowLeft' ? -1 : 1) * (e.shiftKey ? 7 : 1), edge), original)
        }
      }}><span className="sr-only">{label}</span></button>
  )
  return (
    <div className={`gantt-bar${card.blocked ? ' gantt-bar--blocked' : ''}${done?.percent === 100 ? ' gantt-bar--done' : ''}`}
      style={{ left: range.start * dayWidth, width: range.length * dayWidth }}>
      <span className="gantt-fill" style={{ transform: `scaleX(${(done?.percent ?? 0) / 100})` }} />
      {canEdit ? <>{handle('move', t.gantt.move)}{handle('start', t.gantt.resizeStart)}{handle('end', t.gantt.resizeEnd)}</> : <span className="sr-only">{text}</span>}
      {preview && <span className="gantt-preview" role="status">{t.gantt.range(plan.plannedStartOn ?? '', plan.plannedEndOn ?? '')}</span>}
    </div>
  )
}
