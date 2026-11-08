import { VacationWarning } from '../availability/VacationWarning.tsx'
import { useAvailability } from '../availability/useAvailability.ts'
import { PriorityMark } from '../../entities/card/PriorityMark.tsx'
import { useMemo, useState } from 'react'
import type { CSSProperties } from 'react'
import type { BaseState } from '../../entities/board/model.ts'
import type { SaveCardPlan } from '../../shared/api/index.ts'
import { completion } from '../../entities/card/model.ts'
import { locale, t } from '../../shared/i18n/index.ts'
import { ChevronDownIcon, ChevronRightIcon } from '../../shared/ui/icons.tsx'
import { ScreenError } from '../../shared/ui/Field.tsx'
import { useRenderWindow } from '../../shared/lib/useRenderWindow.ts'
import { GanttBar } from './GanttBar.tsx'
import { barRange, dayISO, dayNumber, ganttRows, planOf, todayISO, visibleGanttRows } from './model.ts'

export function GanttView({ base, order, query, onQuery, onOpenCard, canEdit, onSave }: {
  base: BaseState; order: Record<string, string[]>; query: URLSearchParams
  onQuery: (query: URLSearchParams) => void; onOpenCard: (id: string) => void
  canEdit: boolean; onSave: SaveCardPlan
}) {
  const availability = useAvailability(base.info?.id, base.info?.version)
  const s = t.gantt
  const today = todayISO()
  const asked = dayNumber(query.get('ganttFrom') ?? '')
  const initial = Number.isFinite(asked) ? asked : dayNumber(today) - 7
  const month = query.get('ganttScale') === 'month'
  const days = month ? 93 : 56, dayWidth = month ? 12 : 32
  const from = Math.max(dayNumber('0001-01-01'), Math.min(dayNumber('9999-12-31') - days + 1, initial))
  const [error, setError] = useState<string | null>(null)
  const [folded, setFolded] = useState<Set<string>>(() => new Set())
  const toggle = (id: string) => setFolded((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
  const foreign = useMemo(() => new Set(base.links.filter((l) => l.kind === 'subtask' && base.linked[l.toCard]).map((l) => l.fromCard)), [base.links, base.linked])
  const rows = useMemo(() => ganttRows(base.cards, Object.values(order).flat(), base.links), [base.cards, base.links, order])
  // Ветка остаётся вместе: родитель без дат не уезжает под своих детей.
  const branches: typeof rows[] = []
  for (const row of rows) {
    if (row.depth === 0 || branches.length === 0) branches.push([])
    branches[branches.length - 1].push(row)
  }
  const hasPlan = (branch: typeof rows) => branch.some(({ card }) => card.plannedStartOn && card.plannedEndOn)
  const planned = visibleGanttRows(branches.filter(hasPlan).flat(), folded)
  const unplanned = visibleGanttRows(branches.filter((branch) => !hasPlan(branch)).flat(), folded)
  const sorted = [...planned, ...unplanned]
  const window_ = useRenderWindow<HTMLDivElement>(sorted.length)
  const setPeriod = (day: number) => { const q = new URLSearchParams(query); q.set('ganttFrom', dayISO(day)); onQuery(q) }
  const dayText = (day: number, long = false) => new Intl.DateTimeFormat(locale(), { timeZone: 'UTC', day: 'numeric', month: long ? 'long' : 'short', ...(long ? { year: 'numeric' } : {}) }).format(new Date(day * 86_400_000))
  const ticks = Array.from({ length: Math.ceil(days / 7) }, (_, i) => from + i * 7)
  const todayOffset = dayNumber(today) - from
  const style = { '--gantt-days': days, '--gantt-day': `${dayWidth}px`, '--gantt-width': `${days * dayWidth}px` } as CSSProperties
  return (
    <section className="gantt-view stack" aria-label={s.title} style={style}>
      <div className="gantt-tools row">
        <button type="button" className="btn btn--quiet" aria-label={s.previous} onClick={() => setPeriod(from - 28)}>←</button>
        <button type="button" className="btn btn--quiet" onClick={() => setPeriod(dayNumber(today) - 7)}>{s.today}</button>
        <button type="button" className="btn btn--quiet" aria-label={s.next} onClick={() => setPeriod(from + 28)}>→</button>
        <label className="row row--tight"><span className="sr-only">{s.from}</span><input aria-label={s.from} type="date" min="0001-01-01" max="9999-09-01" value={dayISO(from)} onChange={(e) => { const day = dayNumber(e.target.value); if (Number.isFinite(day)) setPeriod(day) }} /></label>
        <select aria-label={s.scale} value={month ? 'month' : 'week'} onChange={(e) => { const q = new URLSearchParams(query); q.set('ganttScale', e.target.value); onQuery(q) }}>
          <option value="week">{s.weeks}</option><option value="month">{s.months}</option>
        </select>
      </div>
      <ScreenError>{error || availability.error}</ScreenError>
      <p className="muted small gantt-hint">{s.hint}</p>
      {rows.length === 0 ? <p className="muted">{s.empty}</p> : (
        <div className="gantt-scroll" tabIndex={0} role="region" aria-label={`${s.title}: ${dayText(from, true)} — ${dayText(from + days - 1, true)}`}>
          <div className="gantt-grid">
            <div className="gantt-head gantt-row">
              <div className="gantt-name">{s.task}</div>
              <div className="gantt-axis">{ticks.map((day) => <span key={day} style={{ left: (day - from) * dayWidth }}>
                {dayText(day)}
              </span>)}</div>
            </div>
            {sorted.slice(0, window_.limit).map(({ card, depth, context, parentId, childCount }, i) => {
              const plan = planOf(card), range = barRange(plan, from, days), done = completion(card)
              const due = card.dueOn ? dayNumber(card.dueOn) - from : NaN
              const overdue = !!plan.plannedEndOn && plan.plannedEndOn < today && done?.percent !== 100 && card.outcome !== 'discarded'
              const people = (base.cardAssignees[card.id] ?? []).map((id) => base.people[id]).filter(Boolean).join(', ')
              return <div key={card.id}>
                {i === planned.length && <div className="gantt-section">{s.unplanned}</div>}
                <div className={`gantt-row${context ? ' gantt-row--context' : ''}${parentId ? ' gantt-row--child' : ''}${childCount ? ' gantt-row--parent' : ''}`} data-card-id={card.id} data-parent-id={parentId ?? undefined}>
                  <div className="gantt-name">
                    <div className="gantt-name-content" style={{ '--gantt-depth': Math.min(depth, 5) } as CSSProperties}>
                      {parentId && <span className="gantt-child-label">{s.subtask}<span className="sr-only">{s.parentIs(base.cards[parentId]?.title ?? '')}</span></span>}
                      {childCount > 0 && <button
                        type="button" className="gantt-branch-toggle" aria-expanded={!folded.has(card.id)}
                        aria-label={s.toggleChildren(card.title, !folded.has(card.id))}
                        aria-describedby={`gantt-children-${card.id}`}
                        onClick={() => toggle(card.id)}>
                        {folded.has(card.id) ? <ChevronRightIcon size={16} /> : <ChevronDownIcon size={16} />}
                        <span id={`gantt-children-${card.id}`}>{s.children(childCount)}</span>
                      </button>}
                      <button type="button" className="link gantt-title" onClick={() => onOpenCard(card.id)}><span className="card-number">{card.number}</span> {card.title}</button>
                      <PriorityMark priority={card.priority} />
                      {availability.data && <VacationWarning data={availability.data} card={card} assignees={base.cardAssignees[card.id] ?? []} people={base.people} />}
                      <div className="muted small">{people}{done ? ` · ${done.percent}%` : ''}</div>
                      {foreign.has(card.id) && <span className="muted small">{s.incomplete}</span>}
                      {context && <span className="muted small">{s.context}</span>}
                      {card.blocked && <span className="mark mark--alarm">{s.blocked}</span>}
                      {overdue && <span className="mark mark--alarm">{s.overdue}</span>}
                    </div>
                  </div>
                  <div className="gantt-track">
                    {todayOffset >= 0 && todayOffset < days && <span className="gantt-today" style={{ left: todayOffset * dayWidth }} aria-label={s.today} />}
                    {Number.isFinite(due) && due >= 0 && due < days && <span className="gantt-due" style={{ left: (due + 0.5) * dayWidth }} title={`${s.due}: ${card.dueOn}`} role="img" aria-label={`${s.due}: ${card.dueOn}`}>◆</span>}
                    {range ? <GanttBar card={card} from={from} days={days} dayWidth={dayWidth} canEdit={canEdit} onSave={onSave} onError={setError} /> : (
                      <button type="button" className="link gantt-no-plan" onClick={() => plan.plannedStartOn && plan.plannedEndOn ? setPeriod(dayNumber(plan.plannedStartOn) - 7) : onOpenCard(card.id)}>
                        {plan.plannedStartOn && plan.plannedEndOn ? s.outside : plan.plannedStartOn || plan.plannedEndOn ? s.range(plan.plannedStartOn ?? '', plan.plannedEndOn ?? '') : s.noDates}
                      </button>
                    )}
                  </div>
                </div>
              </div>
            })}
            {window_.rest > 0 && <div ref={window_.tail}><button type="button" className="btn btn--quiet" onClick={window_.more}>{t.column.moreCards(window_.rest)}</button></div>}
          </div>
        </div>
      )}
    </section>
  )
}
