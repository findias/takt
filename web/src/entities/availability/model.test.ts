import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { Availability, Coverage, Vacation } from '../../shared/api/availability.ts'
import type { Card } from '../../shared/api/index.ts'
import { coverageLevel, overlaps, relevantVacations, shiftDay } from './model.ts'
const vacation: Vacation = { id: 'v', userId: 'u', startsOn: '2026-11-10', endsOn: '2026-11-20', version: 1, cancelled: false, note: '' }
const data: Availability = { today: '2026-11-08', timeZone: 'UTC', vacations: [vacation], coverages: [], eligible: [] }
const card = { plannedStartOn: '2026-11-09', plannedEndOn: '2026-11-12', dueOn: null, outcome: null, doneAt: null } as Card
const coverage = (start: string, end: string): Coverage => ({ id: 'c', vacationId: 'v', vacationVersion: 1, cardId: 'a', status: 'accepted', valid: true, startsOn: start, endsOn: end } as Coverage)
test('предупреждение начинается за заданное число календарных дней и сохраняется до конца отпуска', () => {
  const affected = (today: string, days: number | null) => relevantVacations({ ...data, today }, card, ['u'], days)
  assert.deepEqual(affected('2026-11-02', 7), [])
  assert.deepEqual(affected('2026-11-03', 7), [vacation])
  assert.deepEqual(affected('2026-11-09', 0), [])
  assert.deepEqual(affected('2026-11-10', 0), [vacation])
  assert.deepEqual(affected('2026-11-20', 0), [vacation])
  assert.deepEqual(affected('2026-11-21', 7), [])
  assert.deepEqual(affected('2025-12-01', null), [vacation])
  // Порог не превращает отпуск вне плана в пересечение.
  const later = { ...card, plannedStartOn: '2026-12-01', plannedEndOn: '2026-12-10' }
  assert.deepEqual(relevantVacations(data, later, ['u'], 7), [])
  assert.deepEqual(relevantVacations(data, { ...card, plannedEndOn: null }, ['u'], 1), [])
  assert.deepEqual(relevantVacations(data, { ...card, plannedEndOn: null }, ['u'], 2), [vacation])
  const leap = { ...data, today: '2024-02-28', vacations: [{ ...vacation, startsOn: '2024-03-01', endsOn: '2024-03-02' }] }
  assert.deepEqual(relevantVacations(leap, { ...card, plannedEndOn: null }, ['u'], 1), [])
  assert.equal(relevantVacations(leap, { ...card, plannedEndOn: null }, ['u'], 2).length, 1)
})
test('отпуск пересекается с планом и обязательством; завершённая и чужая работа не предупреждает', () => {
  assert.equal(shiftDay('2024-02-28', 2), '2024-03-01')
  assert.equal(overlaps('2026-11-01', '2026-11-10', '2026-11-10', '2026-11-20'), true)
  assert.equal(overlaps('2026-11-01', '2026-11-09', '2026-11-10', '2026-11-20'), false)
  assert.deepEqual(relevantVacations(data, card, ['u']), [vacation])
  assert.deepEqual(relevantVacations(data, card, ['other']), [])
  assert.deepEqual(relevantVacations(data, { ...card, outcome: 'done' }, ['u']), [])
  assert.deepEqual(relevantVacations(data, { ...card, doneAt: 'now' }, ['u']), [])
  const later = { ...card, plannedStartOn: '2026-12-01', plannedEndOn: '2026-12-10' }
  assert.deepEqual(relevantVacations(data, later, ['u']), [])
  assert.deepEqual(relevantVacations(data, { ...later, dueOn: '2026-11-20' }, ['u']), [vacation])
  assert.deepEqual(relevantVacations(data, { ...card, plannedStartOn: null, plannedEndOn: null }, ['u']), [vacation])
  assert.deepEqual(relevantVacations(data, { ...card, plannedEndOn: null }, ['u']), [vacation])
  assert.deepEqual(relevantVacations({ ...data, today: '2026-11-10' }, later, ['u']), [vacation])
  assert.deepEqual(relevantVacations({ ...data, today: '2026-11-21' }, card, ['u']), [])
  assert.deepEqual(relevantVacations({ ...data, vacations: [{ ...vacation, cancelled: true }] }, card, ['u']), [])
})
test('замещение объединяет соседние периоды, но замечает разрыв и недействительное согласие', () => {
  const level = (coverages: Coverage[]) => coverageLevel({ ...data, coverages }, vacation, 'a')
  assert.equal(level([]), 'none')
  assert.equal(level([coverage('2026-11-10', '2026-11-20')]), 'full')
  assert.equal(level([coverage('2026-11-10', '2026-11-12')]), 'partial')
  assert.equal(level([coverage('2026-11-11', '2026-11-20')]), 'partial')
  assert.equal(level([coverage('2026-11-16', '2026-11-20'), coverage('2026-11-10', '2026-11-15')]), 'full')
  assert.equal(level([coverage('2026-11-16', '2026-11-20'), coverage('2026-11-10', '2026-11-14')]), 'partial')
  assert.equal(level([coverage('2026-11-01', '2026-11-09')]), 'none')
  for (const patch of [{ cardId: 'b' }, { vacationId: 'other' }, { vacationVersion: 2 }, { status: 'proposed' as const }, { valid: false }]) {
    assert.equal(level([{ ...coverage('2026-11-10', '2026-11-20'), ...patch }]), 'none')
  }
})
