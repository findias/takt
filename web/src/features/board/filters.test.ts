// Фильтры доски.
//
// Проверяется то, ради чего они живут в адресе: отфильтрованный вид
// собирается обратно в ссылку и разбирается из неё без потерь. И то,
// ради чего фильтры вообще нужны: показать ровно нужное, не соврав
// ни в одну сторону.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  EMPTY,
  UNASSIGNED,
  filtersToQuery,
  isEmpty,
  matches,
  parseFilters,
} from './filters.ts'
import type { Filters } from './filters.ts'
import type { Card } from '../../shared/api/index.ts'

function card(over: Partial<Card> = {}): Card {
  return {
    id: 'c1',
    columnId: 'col',
    position: 'a0',
    title: 'Согласовать смету',
    description: 'с подрядчиком по второму цеху',
    version: 1,
    columnEnteredAt: '2026-08-01T10:00:00Z',
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
    assigneeId: null,
    ...over,
  }
}

const ctx = { labelsOf: () => [] as string[], sleDays: null }

test('пустой фильтр никого не отсеивает и знает, что он пустой', () => {
  assert.equal(isEmpty(EMPTY), true)
  assert.equal(matches(card(), EMPTY, ctx), true)
})

test('адрес переживает круг: разобрали, собрали, снова разобрали', () => {
  const filters: Filters = {
    text: 'смета',
    assignee: 'u-1',
    labels: ['l-1', 'l-2'],
    blocked: true,
    aging: true,
  }
  const query = filtersToQuery(filters)
  assert.deepEqual(parseFilters(query), filters)

  // Пустые значения в адрес не пишутся: хвост «?q=&assignee=» выглядит
  // как поломка и мешает сравнивать ссылки глазами.
  const empty = filtersToQuery(EMPTY)
  assert.equal(empty.toString(), '')
})

test('чужие параметры адреса не теряются', () => {
  // В адресе живут не только фильтры: сборка обязана сохранять
  // остальное, иначе фильтр будет сбрасывать группировку и обратно.
  const base = new URLSearchParams('group=assignee')
  const query = filtersToQuery({ ...EMPTY, text: 'смета' }, base)
  assert.equal(query.get('group'), 'assignee')
  assert.equal(query.get('q'), 'смета')
})

test('поиск идёт и по названию, и по описанию', () => {
  // Поиск подстрочный, словоформы он не знает: «смета» не найдёт
  // «смету», а «смет» найдёт оба. Морфология здесь была бы отдельным
  // продуктом, а подстроки хватает — так же ищут Jira и Linear.
  const f = { ...EMPTY, text: 'СМЕТ' }
  assert.equal(matches(card(), f, ctx), true, 'регистр не должен мешать')
  assert.equal(matches(card({ title: 'Другое' }), f, ctx), false)
  assert.equal(
    matches(card({ title: 'Другое', description: 'смета внутри описания' }), f, ctx),
    true,
  )
})

test('исполнитель: конкретный и «ни на ком»', () => {
  const mine = { ...EMPTY, assignee: 'u-1' }
  assert.equal(matches(card({ assigneeId: 'u-1' }), mine, ctx), true)
  assert.equal(matches(card({ assigneeId: 'u-2' }), mine, ctx), false)
  assert.equal(matches(card(), mine, ctx), false)

  // Работа без исполнителя и есть то, что теряется, — её надо уметь
  // спросить отдельно.
  const nobody = { ...EMPTY, assignee: UNASSIGNED }
  assert.equal(matches(card(), nobody, ctx), true)
  assert.equal(matches(card({ assigneeId: 'u-1' }), nobody, ctx), false)
})

test('метки складываются по И, а не по ИЛИ', () => {
  const withLabels = (ids: string[]) => ({ labelsOf: () => ids, sleDays: null })
  const f = { ...EMPTY, labels: ['срочно', 'снаружи'] }

  assert.equal(matches(card(), f, withLabels(['срочно', 'снаружи', 'ещё'])), true)
  // «Срочно ИЛИ снаружи» почти всегда означает, что человек хотел
  // первое и промахнулся.
  assert.equal(matches(card(), f, withLabels(['срочно'])), false)
})

test('блокировка и возраст сверх обещания', () => {
  const blocked = { ...EMPTY, blocked: true }
  assert.equal(matches(card(), blocked, ctx), false)
  assert.equal(
    matches(
      card({ blocked: { id: 'b', reason: 'ждём ответа', blockedAt: '2026-08-01T10:00:00Z' } }),
      blocked,
      ctx,
    ),
    true,
  )

  const now = Date.parse('2026-08-15T12:00:00Z')
  const started = (days: number) => new Date(now - days * 86_400_000).toISOString()
  const aging = { ...EMPTY, aging: true }
  const withSle = { labelsOf: () => [] as string[], sleDays: 8, now }

  assert.equal(matches(card({ startedAt: started(9) }), aging, withSle), true)
  assert.equal(matches(card({ startedAt: started(3) }), aging, withSle), false)
  // Без обещания сравнивать не с чем — и «стареющих» не бывает.
  assert.equal(
    matches(card({ startedAt: started(40) }), aging, { labelsOf: () => [], sleDays: null, now }),
    false,
  )
})
