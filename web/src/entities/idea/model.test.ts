// Вкладка «Идеи»: порядок и похожее — чистые функции, без браузера.
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { isSettled, parseIdeaSort, similarIdeas, sortIdeas, stems, visibleIdeas, votersLine } from './model.ts'
import type { Idea } from '../../shared/api/index.ts'

const idea = (id: string, title: string, votes: number, createdAt: string): Idea => ({
  id,
  title,
  body: '',
  authorId: null,
  author: null,
  teamId: null,
  team: null,
  votes,
  voters: [],
  voted: false,
  createdAt,
  decision: null,
  canDecide: false,
})
const decided = (i: Idea, kind: 'taken' | 'declined' | 'duplicate' | 'later'): Idea => ({
  ...i,
  decision: { id: 'd', kind, note: '', authorId: null, author: null, at: i.createdAt },
})
const ids = (list: Idea[]) => list.map((i) => i.id)

const list = [
  idea('a', 'Шаблон карточки для типовой поставки', 3, '2026-10-18T10:00:00Z'),
  idea('b', 'Общий чат с дежурным смежников', 2, '2026-10-21T10:00:00Z'),
  idea('c', 'Отмечать карточки, которые ждут приёмки', 4, '2026-10-25T10:00:00Z'),
  idea('d', 'Сверять остатки по понедельникам', 0, '2026-10-29T10:00:00Z'),
]

test('порядок из адреса: всё, кроме votes, — новые', () => {
  assert.equal(parseIdeaSort(new URLSearchParams('sort=votes')), 'votes')
  assert.equal(parseIdeaSort(new URLSearchParams('sort=чушь')), 'new')
  assert.equal(parseIdeaSort(new URLSearchParams()), 'new')
})

test('новые — по дате, поддержанные — по голосам, исходный список не трогается', () => {
  assert.deepEqual(ids(sortIdeas(list, 'new')), ['d', 'c', 'b', 'a'])
  assert.deepEqual(ids(sortIdeas(list, 'votes')), ['c', 'a', 'b', 'd'])
  assert.deepEqual(ids(list), ['a', 'b', 'c', 'd'])
})

test('при равных голосах выше новая', () => {
  const tie = [idea('x', 'Одно', 2, '2026-10-01T00:00:00Z'), idea('y', 'Другое', 2, '2026-10-02T00:00:00Z')]
  assert.deepEqual(ids(sortIdeas(tie, 'votes')), ['y', 'x'])
})

test('основы слов: короткие не считаются, длинные режутся, знаки отбрасываются', () => {
  assert.deepEqual([...stems('Чат с дежурным: «шаблоны»!')], ['дежур', 'шабло'])
  assert.deepEqual([...stems('на для про')], [])
})

test('похожее находится по основе, ранжируется по совпадениям и голосам', () => {
  // «карточки» есть у a и c; у c голосов больше — она первая.
  assert.deepEqual(ids(similarIdeas(list, 'Карточка для поставок')), ['a', 'c'])
  assert.deepEqual(ids(similarIdeas(list, 'карточки')), ['c', 'a'])
  // Слишком мало набрано — ничего, а не всё подряд.
  assert.deepEqual(similarIdeas(list, 'чат'), [])
  assert.deepEqual(similarIdeas(list, ''), [])
  // Предел.
  assert.equal(similarIdeas(list, 'карточки поставки чат остатки', 2).length, 2)
})

test('строка поддержавших: первые трое именами, остальные числом', () => {
  assert.deepEqual(votersLine(['Анна', 'Борис']), { names: 'Анна, Борис', more: 0 })
  assert.deepEqual(votersLine(['Анна', 'Борис', 'Вера', 'Глеб', 'Дмитрий']), {
    names: 'Анна, Борис, Вера',
    more: 2,
  })
  assert.deepEqual(votersLine([]), { names: '', more: 0 })
})

test('решённые насовсем прячутся, «позже» и нерешённые остаются', () => {
  const mixed = [
    decided(list[0], 'taken'),
    decided(list[1], 'later'),
    decided(list[2], 'declined'),
    list[3],
    decided(idea('e', 'Пятая', 0, '2026-10-30T10:00:00Z'), 'duplicate'),
  ]
  assert.deepEqual(ids(visibleIdeas(mixed, false)), ['b', 'd'])
  assert.deepEqual(ids(visibleIdeas(mixed, true)), ['a', 'b', 'c', 'd', 'e'])
  assert.equal(isSettled(mixed[1]), false)
  assert.equal(isSettled(mixed[0]), true)
})
