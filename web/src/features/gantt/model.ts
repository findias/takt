import type { Card, CardPlan, Link } from '../../shared/api/index.ts'

const DAY = 86_400_000
// Календарная арифметика через номер дня не зависит от часового пояса
// браузера и длительности суток в день перевода часов.
export function dayNumber(iso: string): number {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(iso) || iso < '0001-01-01') return NaN
  const value = Date.parse(`${iso}T00:00:00Z`)
  return Number.isFinite(value) && new Date(value).toISOString().slice(0, 10) === iso ? value / DAY : NaN
}
export function dayISO(day: number): string {
  return new Date(Math.max(dayNumber('0001-01-01'), Math.min(dayNumber('9999-12-31'), day)) * DAY).toISOString().slice(0, 10)
}
export function todayISO(now = new Date()): string {
  return `${String(now.getFullYear()).padStart(4, '0')}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
}
export function planOf(card: Card): CardPlan {
  return { plannedStartOn: card.plannedStartOn ?? null, plannedEndOn: card.plannedEndOn ?? null }
}
export function validPlan(plan: CardPlan): boolean {
  const a = plan.plannedStartOn, b = plan.plannedEndOn
  return (!a || Number.isFinite(dayNumber(a))) && (!b || Number.isFinite(dayNumber(b))) && (!a || !b || a <= b)
}
export function changePlan(plan: CardPlan, delta: number, edge: 'move' | 'start' | 'end'): CardPlan {
  if (!plan.plannedStartOn || !plan.plannedEndOn) return plan
  let a = dayNumber(plan.plannedStartOn), b = dayNumber(plan.plannedEndOn)
  const low = dayNumber('0001-01-01'), high = dayNumber('9999-12-31')
  if (edge === 'move') { delta = Math.max(low - a, Math.min(high - b, delta)); a += delta; b += delta }
  else if (edge === 'start') a = Math.min(b, Math.max(low, a + delta))
  else b = Math.max(a, Math.min(high, b + delta))
  return { plannedStartOn: dayISO(a), plannedEndOn: dayISO(b) }
}
export function barRange(plan: CardPlan, from: number, days: number): { start: number; length: number } | null {
  if (!plan.plannedStartOn || !plan.plannedEndOn) return null
  const a = Math.max(0, dayNumber(plan.plannedStartOn) - from)
  const b = Math.min(days, dayNumber(plan.plannedEndOn) - from + 1)
  return b > a ? { start: a, length: b - a } : null
}
export type GanttRow = { card: Card; depth: number; context: boolean; parentId: string | null; childCount: number }
export function ganttRows(cards: Record<string, Card>, ids: string[], links: Link[]): GanttRow[] {
  const selected = new Set(ids), included = new Set(ids)
  const parents = new Map<string, string>()
  for (const l of links) if (l.kind === 'subtask' && cards[l.fromCard] && cards[l.toCard]) parents.set(l.toCard, l.fromCard)
  for (const id of ids) {
    const seen = new Set<string>([id])
    let parent = parents.get(id)
    while (parent && !seen.has(parent)) { seen.add(parent); included.add(parent); parent = parents.get(parent) }
  }
  const children = new Map<string, string[]>()
  const roots: string[] = []
  for (const id of included) {
    const parent = parents.get(id)
    if (parent && included.has(parent)) children.set(parent, [...(children.get(parent) ?? []), id])
    else roots.push(id)
  }
  const out: GanttRow[] = [], seen = new Set<string>()
  const visit = (id: string, depth: number, parentId: string | null = null) => {
    if (seen.has(id) || !cards[id]) return
    seen.add(id); out.push({ card: cards[id], depth, context: !selected.has(id), parentId, childCount: children.get(id)?.length ?? 0 })
    for (const child of children.get(id) ?? []) visit(child, depth + 1, id)
  }
  for (const id of roots) visit(id, 0)
  // Защита от повреждённого графа: карточки всё равно видны.
  for (const id of included) visit(id, 0)
  return out
}

/** Свёрнутая ветка скрывает строки целиком: названия и полосы остаются
 * в одной и той же последовательности. Вложенные свёртки сохраняются. */
export function visibleGanttRows(rows: GanttRow[], folded: ReadonlySet<string>): GanttRow[] {
  const shown: GanttRow[] = []
  let hiddenBelow: number | null = null
  for (const row of rows) {
    if (hiddenBelow !== null && row.depth > hiddenBelow) continue
    hiddenBelow = null
    shown.push(row)
    if (folded.has(row.card.id)) hiddenBelow = row.depth
  }
  return shown
}
