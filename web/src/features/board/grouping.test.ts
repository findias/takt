// Группировка доски.
//
// Проверяется то, из-за чего она устроена именно так: карточка с двумя
// метками попадает в обе группы (доска отвечает на вопрос «что помечено
// срочным», а не «в какую корзину положить»), а группа «ни на ком»
// остаётся даже пустой — именно там теряется работа.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { GROUPING_NAMES, groupingToQuery, groupsOf, parseGrouping } from './grouping.ts'
import type { BaseState } from '../../entities/board/model.ts'
import type { Card } from '../../shared/api/index.ts'

const COL = 'col'

function card(id: string, over: Partial<Card> = {}): Card {
  return {
    id,
    number: `ДОСК-${id}`,
    columnId: COL,
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
    ...over,
  }
}

function state(cards: Card[], over: Partial<BaseState> = {}): BaseState {
  return {
    info: { id: 'b', name: 'Доска', version: 1, sleDays: null, sleProbability: 85 },
    columnIds: [COL],
    columns: {},
    cards: Object.fromEntries(cards.map((c) => [c.id, c])),
    order: { [COL]: cards.map((c) => c.id) },
    links: [],
    linked: {},
    iterations: [],
    cardIterations: {},
    fields: [],
    fieldValues: {},
    people: { 'u-1': 'Мария Кузнецова' },
    labels: [
      { id: 'l-1', name: 'Срочно', tone: 'rose' },
      { id: 'l-2', name: 'Снаружи', tone: 'blue' },
    ],
    cardLabels: {},
    cardAssignees: {},
    ...over,
  } as BaseState
}

test('адрес: группировка разбирается и собирается', () => {
  assert.equal(parseGrouping(new URLSearchParams('group=assignee')), 'assignee')
  assert.equal(parseGrouping(new URLSearchParams('group=чепуха')), 'none')
  assert.equal(groupingToQuery('label').toString(), 'group=label')
  // Отключённая группировка из адреса убирается, а не пишется словом.
  assert.equal(groupingToQuery('none', new URLSearchParams('group=label')).toString(), '')
})

test('без группировки доска остаётся одной дорожкой', () => {
  const base = state([card('a'), card('b')])
  const groups = groupsOf(base, base.order, 'none')
  assert.equal(groups.length, 1)
  assert.equal(groups[0].count, 2)
  assert.equal(groups[0].order, base.order)
})

test('по исполнителю: своя дорожка и «ни на ком»', () => {
  const base = state([card('моя'), card('ничья')], { cardAssignees: { моя: ['u-1'] } })
  const groups = groupsOf(base, base.order, 'assignee')

  assert.deepEqual(
    groups.map((g) => [g.title, g.count]),
    [
      ['Мария Кузнецова', 1],
      ['Ни на ком', 1],
    ],
  )
  // Пустая группа идёт последней: сначала то, что кем-то ведётся.
  assert.equal(groups.at(-1)?.id, 'none')
})

test('карточку, которую делают вдвоём, видно в дорожке каждого', () => {
  // Иначе один из двоих не найдёт в своей дорожке работу, о которой
  // они договорились вместе, — а дорожка отвечает на вопрос «что на
  // мне», а не «чья это карточка целиком».
  const base = state([card('вместе')], {
    people: { 'u-1': 'Мария Кузнецова', 'u-2': 'Иван Петров' },
    cardAssignees: { вместе: ['u-1', 'u-2'] },
  })
  const groups = groupsOf(base, base.order, 'assignee')

  // Порядок дорожек — по именам, поэтому сравниваем состав.
  assert.deepEqual(
    groups
      .filter((g) => g.count > 0)
      .map((g) => g.title)
      .sort(),
    ['Иван Петров', 'Мария Кузнецова'],
  )
})

test('«ни на ком» остаётся, даже когда она пуста', () => {
  // Иначе исчезнувшая группа читается как «всё разобрано», а на деле
  // её просто нечем показать — и следующая же неназначенная карточка
  // появится там, куда никто не смотрит.
  const base = state([card('моя')], { cardAssignees: { моя: ['u-1'] } })
  const groups = groupsOf(base, base.order, 'assignee')
  assert.equal(groups.some((g) => g.id === 'none'), true)
})

test('по метке: карточка с двумя метками попадает в обе дорожки', () => {
  const base = state([card('обе'), card('без метки')], {
    cardLabels: { обе: ['l-1', 'l-2'] },
  })
  const groups = groupsOf(base, base.order, 'label')

  const byTitle = Object.fromEntries(groups.map((g) => [g.title, g]))
  assert.equal(byTitle['Срочно'].order[COL].includes('обе'), true)
  assert.equal(byTitle['Снаружи'].order[COL].includes('обе'), true)
  assert.deepEqual(byTitle['Без метки'].order[COL], ['без метки'])
})

test('по итерации: вне итерации — тоже ответ', () => {
  const base = state([card('в спринте'), card('вне')], {
    iterations: [
      { id: 'i-1', name: 'Спринт 12', goal: '', startsOn: '2026-08-01', endsOn: '2026-08-14', closedAt: null, cardCount: 1 },
    ],
    cardIterations: { 'в спринте': 'i-1' },
  })
  const groups = groupsOf(base, base.order, 'iteration')

  assert.deepEqual(
    groups.map((g) => g.title),
    ['Спринт 12', 'Вне итерации'],
  )
})

test('у каждой группировки есть человеческое название', () => {
  assert.equal(GROUPING_NAMES.assignee, 'По исполнителю')
  assert.equal(Object.keys(GROUPING_NAMES).length, 4)
})
