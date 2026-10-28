// Отбор по исполнителю: кого предлагать и в каком порядке.
//
// Список прежде брал всю организацию — на доске из пяти человек выбор
// листал полторы сотни имён тех, кто доски не видит. Проверяется, что
// в нём видящие доску и стоящие на её карточках, а загрузка — по всей
// доске и только незавершённое.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { assigneeOptions, matchAssignee, sortAssignees } from './assigneeFilter.ts'
import type { BaseState } from '../../entities/board/model.ts'
import type { Card } from '../../shared/api/index.ts'

function card(id: string, over: Partial<Card> = {}): Card {
  return {
    id,
    number: `ДОСК-${id}`,
    columnId: 'work',
    position: 'a0',
    title: id,
    description: '',
    version: 1,
    columnEnteredAt: '2026-08-01T10:00:00Z',
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
    comments: 0,
    priority: 'medium',
    dueOn: null,
    doneAt: null,
    ...over,
  }
}

function state(cards: Card[], assignees: Record<string, string[]>): BaseState {
  return {
    columns: {
      work: { id: 'work', kind: 'in_progress' },
      done: { id: 'done', kind: 'done' },
    },
    cards: Object.fromEntries(cards.map((c) => [c.id, c])),
    people: {
      anna: 'Анна',
      boris: 'Борис',
      vera: 'Вера',
      gleb: 'Глеб',
      stranger: 'Чужой',
    },
    // Глеб доски не видит; Чужой — тоже, и карточек за ним нет.
    assignable: { anna: 'Анна', boris: 'Борис', vera: 'Вера' },
    cardAssignees: assignees,
  } as unknown as BaseState
}

const base = state(
  [
    card('1', { estimate: 3 }),
    card('2', { estimate: 5 }),
    card('3'),
    card('4', { columnId: 'done', estimate: 8 }),
    card('5', { outcome: 'done', estimate: 13 }),
    card('6', { estimate: 1 }),
  ],
  {
    '1': ['anna', 'boris'],
    '2': ['boris'],
    '3': ['vera'],
    '4': ['vera'],
    '5': ['anna'],
    '6': ['gleb'],
  },
)

test('в списке видящие доску и стоящие на её карточках, но не вся организация', () => {
  const names = assigneeOptions(base, null).map((o) => o.name).sort()
  assert.deepEqual(names, ['Анна', 'Борис', 'Вера', 'Глеб'])
  const gleb = assigneeOptions(base, null).find((o) => o.userId === 'gleb')
  assert.equal(gleb?.noAccess, true)
  assert.equal(assigneeOptions(base, null).find((o) => o.userId === 'anna')?.noAccess, false)
})

test('отобранный по ссылке остаётся в списке, даже если доски не видит', () => {
  assert.ok(assigneeOptions(base, 'stranger').some((o) => o.userId === 'stranger'))
  // Неизвестного снимку показать нечем.
  assert.ok(!assigneeOptions(base, 'nobody').some((o) => o.userId === 'nobody'))
})

test('загрузка — только незавершённое: колонка финиша и сделанное не в счёт', () => {
  const by = Object.fromEntries(assigneeOptions(base, null).map((o) => [o.userId, o]))
  assert.deepEqual([by.anna.cards, by.anna.weight], [1, 3])
  assert.deepEqual([by.boris.cards, by.boris.weight], [2, 8])
  assert.deepEqual([by.vera.cards, by.vera.weight, by.vera.unestimated], [1, 0, 1])
})

test('порядок: по имени, по карточкам, по оценке; равные — по имени', () => {
  const options = assigneeOptions(base, null)
  const names = (sort: Parameters<typeof sortAssignees>[1]) => sortAssignees(options, sort).map((o) => o.name)
  assert.deepEqual(names('name'), ['Анна', 'Борис', 'Вера', 'Глеб'])
  assert.deepEqual(names('cards'), ['Борис', 'Анна', 'Глеб', 'Вера'])
  assert.deepEqual(names('weight'), ['Борис', 'Анна', 'Глеб', 'Вера'])
})

test('поиск — подстрока имени без учёта регистра', () => {
  const anna = assigneeOptions(base, null).find((o) => o.userId === 'anna')!
  assert.ok(matchAssignee(anna, 'ан'))
  assert.ok(matchAssignee(anna, '  '))
  assert.ok(!matchAssignee(anna, 'бор'))
})
