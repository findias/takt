// Тесты модели карточки.
//
// Проверяется главное свойство связей: подзадача может лежать где угодно —
// на этой доске, на доске другой команды или на доске, которой мы не видим.
// Все три случая должны быть различимы на экране, и ни один не должен
// исчезать молча.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  candidatesForSubtask,
  cardDetails,
  progressLabel,
  progressRatio,
} from './model.ts'
import type { BaseState } from '../board/model.ts'
import type { Card, Link, LinkedCard } from '../../shared/api/index.ts'

function card(id: string, title: string, extra: Partial<Card> = {}): Card {
  return {
    id,
    columnId: 'col',
    position: 'a0',
    title,
    description: '',
    version: 1,
    columnEnteredAt: '2026-08-15T09:00:00Z',
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
    ...extra,
  }
}

function foreign(id: string, title: string, extra: Partial<LinkedCard> = {}): LinkedCard {
  return {
    id,
    title,
    boardId: 'other-board',
    boardName: 'Соседняя',
    teamName: 'Платформа',
    outcome: null,
    blocked: false,
    ...extra,
  }
}

function state(cards: Card[], links: Link[], linked: LinkedCard[] = []): BaseState {
  return {
    info: { id: 'board', name: 'Доска', version: 1, sleDays: null, sleProbability: 85 },
    columnIds: ['col'],
    columns: {},
    cards: Object.fromEntries(cards.map((c) => [c.id, c])),
    order: { col: cards.map((c) => c.id) },
    links,
    linked: Object.fromEntries(linked.map((c) => [c.id, c])),
    iterations: [],
    cardIterations: {},
    fields: [],
    fieldValues: {},
  } as BaseState
}

test('подзадачи разбираются по трём случаям: своя, чужая, недоступная', () => {
  const base = state(
    [card('parent', 'Релиз'), card('own', 'Своя подзадача', { outcome: 'done' })],
    [
      { fromCard: 'parent', toCard: 'own', kind: 'subtask' },
      { fromCard: 'parent', toCard: 'neighbour', kind: 'subtask' },
      { fromCard: 'parent', toCard: 'hidden', kind: 'subtask' },
    ],
    [foreign('neighbour', 'Сборка', { blocked: true })],
  )

  const details = cardDetails(base, 'parent')
  assert.ok(details)
  assert.equal(details.subtasks.length, 3)

  const own = details.subtasks.find((s) => s.id === 'own')!
  assert.equal(own.where, 'На этой доске')
  assert.equal(own.done, true)
  assert.equal(own.onThisBoard, true)

  const neighbour = details.subtasks.find((s) => s.id === 'neighbour')!
  assert.equal(neighbour.title, 'Сборка')
  assert.equal(neighbour.where, 'Доска «Соседняя» · Платформа')
  assert.equal(neighbour.blocked, true)
  assert.equal(neighbour.onThisBoard, false)

  // Недоступную карточку не выбрасываем: связь существует, и прогресс
  // родителя её учитывает — молча спрятать её значит соврать про прогресс.
  const hidden = details.subtasks.find((s) => s.id === 'hidden')!
  assert.equal(hidden.reachable, false)
  assert.ok(hidden.where.includes('не видно'))
})

test('родитель находится по обратной стороне связи и он один', () => {
  const base = state(
    [card('child', 'Подзадача')],
    [{ fromCard: 'parent', toCard: 'child', kind: 'subtask' }],
    [foreign('parent', 'Релиз')],
  )
  const details = cardDetails(base, 'child')!
  assert.equal(details.parent?.id, 'parent')
  assert.equal(details.parent?.title, 'Релиз')
  assert.equal(details.subtasks.length, 0)
})

test('блокирующие и смежные связи собираются в обе стороны', () => {
  const base = state(
    [card('a', 'А'), card('b', 'Б'), card('c', 'В')],
    [
      { fromCard: 'a', toCard: 'b', kind: 'blocks' },
      { fromCard: 'c', toCard: 'a', kind: 'relates' },
    ],
  )
  const details = cardDetails(base, 'a')!
  assert.equal(details.subtasks.length, 0)
  assert.deepEqual(
    details.related.map((r) => `${r.kind}:${r.id}`).sort(),
    ['blocks:b', 'relates:c'],
  )
})

test('карточки, которой нет, не существует и деталей', () => {
  assert.equal(cardDetails(state([], []), 'нет'), null)
})

test('прогресс показывается только когда есть подзадачи', () => {
  assert.equal(progressLabel(card('a', 'А')), null)
  assert.equal(
    progressLabel(card('a', 'А', { progress: { done: 0, total: 0, byWeight: false } })),
    null,
  )
  assert.equal(
    progressLabel(card('a', 'А', { progress: { done: 1, total: 3, byWeight: false } })),
    '1 из 3',
  )
  assert.equal(
    progressRatio(card('a', 'А', { progress: { done: 1, total: 4, byWeight: false } })),
    0.25,
  )
  assert.equal(progressRatio(card('a', 'А')), 0)
})

test('прогресс по весу называет единицу и склоняет её', () => {
  // Разница не косметическая: «3 из 5» и «12 из 20 очков» — разные
  // утверждения о том, сколько работы сделано.
  const weighted = (done: number, total: number) =>
    card('a', 'А', { progress: { done, total, byWeight: true } })

  assert.equal(progressLabel(weighted(12, 20), 'points'), '12 из 20 очков')
  assert.equal(progressLabel(weighted(1, 1), 'hours'), '1 из 1 час')
  assert.equal(progressLabel(weighted(0, 3), 'days'), '0 из 3 дня')
  assert.equal(progressLabel(weighted(0, 13), 'points'), '0 из 13 очков')

  // Дробные оценки существуют, но «2.00» на карточке не нужно никому.
  assert.equal(progressLabel(weighted(0.5, 2.5), 'hours'), '0.5 из 2.5 часа')

  // Без единицы подпись остаётся честной, просто без названия.
  assert.equal(progressLabel(weighted(12, 20)), '12 из 20')
})

test('в подзадачи не предлагается ни сама карточка, ни занятые', () => {
  const base = state(
    [
      card('parent', 'Релиз'),
      card('mine', 'Уже моя'),
      card('free', 'Свободная'),
      card('taken', 'Чужая подзадача'),
      card('other', 'Другой родитель'),
    ],
    [
      { fromCard: 'parent', toCard: 'mine', kind: 'subtask' },
      { fromCard: 'other', toCard: 'taken', kind: 'subtask' },
    ],
  )
  const details = cardDetails(base, 'parent')!
  const ids = candidatesForSubtask(base, details).map((c) => c.id)

  assert.deepEqual(ids.sort(), ['free', 'other'])
  // 'mine' уже подзадача этой карточки, 'taken' — чужая: второй родитель
  // невозможен, и предлагать его значит обещать невыполнимое.
})
