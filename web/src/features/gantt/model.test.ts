import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { Card } from '../../shared/api/index.ts'
import { barRange, changePlan, dayISO, dayNumber, ganttRows, planOf, todayISO, validPlan, visibleGanttRows } from './model.ts'
const plan = { plannedStartOn: '2026-10-31', plannedEndOn: '2026-11-02' }

test('Гант считает календарные дни, включая високосные годы и перевод часов', () => {
  for (const iso of ['0001-01-01', '2024-02-29', '2026-03-29', '2026-10-25', '9999-12-31']) assert.equal(dayISO(dayNumber(iso)), iso)
  for (const iso of ['', '0000-01-01', '2026-2-01', '2026-02-30', '2025-02-29', '2026-13-01', 'xxxx']) assert.ok(Number.isNaN(dayNumber(iso)))
  assert.equal(dayNumber('2026-03-30') - dayNumber('2026-03-29'), 1)
  assert.equal(dayISO(-10000000), '0001-01-01')
  assert.equal(dayISO(10000000), '9999-12-31')
  assert.equal(todayISO(new Date(2026, 10, 5, 23)), '2026-11-05')
  assert.match(todayISO(), /^\d{4}-\d{2}-\d{2}$/)
})
test('план отличает неизвестные даты и разрешает один день', () => {
  assert.deepEqual(planOf({} as Card), { plannedStartOn: null, plannedEndOn: null })
  assert.deepEqual(planOf(plan as Card), plan)
  for (const p of [plan, { plannedStartOn: null, plannedEndOn: null }, { plannedStartOn: null, plannedEndOn: '2026-11-02' }, { plannedStartOn: '2026-11-02', plannedEndOn: null }, { plannedStartOn: '2026-11-02', plannedEndOn: '2026-11-02' }]) assert.ok(validPlan(p))
  for (const p of [{ ...plan, plannedStartOn: '2026-11-03' }, { ...plan, plannedStartOn: 'не дата' }, { ...plan, plannedEndOn: '2026-02-30' }]) assert.equal(validPlan(p), false)
})
test('перенос сохраняет длительность, края не пересекаются и не выходят за допустимые годы', () => {
  assert.deepEqual(changePlan(plan, 2, 'move'), { plannedStartOn: '2026-11-02', plannedEndOn: '2026-11-04' })
  assert.deepEqual(changePlan(plan, 10, 'start'), { ...plan, plannedStartOn: plan.plannedEndOn })
  assert.deepEqual(changePlan(plan, -10, 'end'), { ...plan, plannedEndOn: plan.plannedStartOn })
  const low = { plannedStartOn: '0001-01-01', plannedEndOn: '0001-01-02' }
  const high = { plannedStartOn: '9999-12-30', plannedEndOn: '9999-12-31' }
  assert.deepEqual(changePlan(low, -10, 'move'), low)
  assert.deepEqual(changePlan(high, 10, 'move'), high)
  assert.deepEqual(changePlan(low, -10, 'start'), low)
  assert.deepEqual(changePlan(high, 10, 'end'), high)
  const empty = { plannedStartOn: null, plannedEndOn: null }
  assert.equal(changePlan(empty, 5, 'move'), empty)
  const partial = { ...plan, plannedEndOn: null }
  assert.equal(changePlan(partial, 5, 'move'), partial)
})
test('полоса обрезается окном, конец включён, вне окна и без периода полосы нет', () => {
  const from = dayNumber('2026-11-01')
  assert.deepEqual(barRange(plan, from, 5), { start: 0, length: 2 })
  assert.deepEqual(barRange(plan, dayNumber('2026-10-30'), 2), { start: 1, length: 1 })
  assert.equal(barRange(plan, dayNumber('2026-12-01'), 5), null)
  assert.equal(barRange(plan, dayNumber('2026-09-01'), 5), null)
  assert.equal(barRange({ ...plan, plannedStartOn: null }, from, 5), null)
  assert.equal(barRange({ ...plan, plannedEndOn: null }, from, 5), null)
  assert.deepEqual(barRange({ plannedStartOn: '2026-11-01', plannedEndOn: '2026-11-01' }, from, 5), { start: 0, length: 1 })
})
test('иерархия сохраняет родителей как контекст, не включает чужие доски и переживает цикл', () => {
  const cards = Object.fromEntries(['a','b','c','d'].map(id => [id, { id } as Card]))
  const links = [{ fromCard: 'a', toCard: 'b', kind: 'subtask' as const }, { fromCard: 'b', toCard: 'c', kind: 'subtask' as const }, { fromCard: 'b', toCard: 'd', kind: 'blocks' as const }, { fromCard: 'a', toCard: 'foreign', kind: 'subtask' as const }]
  assert.deepEqual(ganttRows(cards, ['c','d'], links).map(r => [r.card.id,r.depth,r.context]), [['d',0,false],['a',0,true],['b',1,true],['c',2,false]])
  assert.equal(ganttRows(cards, ['c','b','a'], links).length, 3)
  assert.equal(ganttRows(cards, ['missing'], []).length, 0)
  assert.equal(ganttRows(cards, ['a'], [...links, {fromCard:'c',toCard:'a',kind:'subtask'}]).length, 3)
  assert.deepEqual(ganttRows(cards, [], links), [])
})

test('свёртка Ганта скрывает всё поддерево, сохраняя соседей и вложенные свёртки', () => {
  const cards = Object.fromEntries(['a','b','c','d'].map(id => [id, { id } as Card]))
  const rows = ganttRows(cards, ['a','b','c','d'], [
    { fromCard: 'a', toCard: 'b', kind: 'subtask' },
    { fromCard: 'b', toCard: 'c', kind: 'subtask' },
  ])
  assert.deepEqual(rows.map(r => [r.card.id,r.parentId,r.childCount]), [['a',null,1],['b','a',1],['c','b',0],['d',null,0]])
  assert.deepEqual(visibleGanttRows(rows, new Set(['a','b'])).map(r => r.card.id), ['a','d'])
  assert.deepEqual(visibleGanttRows(rows, new Set(['b'])).map(r => r.card.id), ['a','b','d'])
  assert.deepEqual(visibleGanttRows(rows, new Set()).map(r => r.card.id), ['a','b','c','d'])
  assert.deepEqual(visibleGanttRows([], new Set(['a'])), [])
})
