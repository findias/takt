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
    doneAt: null,
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
    cardRefs: {},
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

test('по уровню: дорожки идут шкалой, а не по алфавиту', () => {
  // По алфавиту «Высокий» встал бы выше «Наивысшего» — и дорожки
  // перестали бы читаться сверху вниз. Ради этого порядка на такую
  // группировку и смотрят: сверху то, что горит.
  const base = state([
    card('фоновая', { priority: 'low' }),
    card('обычная'),
    card('срочная', { priority: 'highest' }),
    card('важная', { priority: 'high' }),
  ])
  const groups = groupsOf(base, base.order, 'priority')

  assert.deepEqual(
    groups.map((g) => g.title),
    ['Наивысший', 'Высокий', 'Средний', 'Низкий'],
  )
})

test('по уровню: пустой дорожки «без уровня» нет', () => {
  // Уровень есть у каждой карточки — значит «без уровня» не состояние,
  // а пустое место в списке дорожек. Пустых уровней тоже нет: дорожка
  // «Наивысший» без карточек ничего не сообщает, кроме своей ширины.
  const base = state([card('обычная')])
  const groups = groupsOf(base, base.order, 'priority')

  assert.deepEqual(
    groups.map((g) => [g.id, g.count]),
    [['medium', 1]],
  )
})

test('у каждой группировки есть человеческое название', () => {
  assert.equal(GROUPING_NAMES.assignee, 'По исполнителю')
  assert.equal(GROUPING_NAMES.priority, 'По приоритету')
  assert.equal(Object.keys(GROUPING_NAMES).length, 7)
})

// Дерево: эпик → две фичи → задачи; одна фича на доске соседей.
function tree() {
  const cards = [
    card('эпик', {
      title: 'Переезд склада',
      progress: { done: 1, total: 2, byWeight: false },
      subtree: { done: 1, total: 4, byWeight: false, stuck: 0 },
    }),
    card('фича'),
    card('задача-1'),
    card('задача-2'),
    card('задача-3'),
    card('сама-по-себе'),
  ]
  const sub = (fromCard: string, toCard: string) => ({ fromCard, toCard, kind: 'subtask' as const })
  return state(cards, {
    links: [
      sub('эпик', 'фича'),
      sub('эпик', 'чужая-фича'),
      sub('фича', 'задача-1'),
      sub('фича', 'задача-2'),
      sub('чужая-фича', 'задача-3'),
    ],
    linked: {
      'чужая-фича': { id: 'чужая-фича', title: 'Упаковка', boardId: 'b2', boardName: 'Соседи' } as never,
    },
  })
}
const lanes = (groups: ReturnType<typeof groupsOf>) =>
  Object.fromEntries(groups.map((g) => [g.id, g.order[COL]]))

test('по родителю: дорожка у каждого родителя, и сам родитель в ней не повторяется', () => {
  const base = tree()
  const groups = groupsOf(base, base.order, 'parent')
  const by = lanes(groups)
  assert.deepEqual(by['фича'], ['задача-1', 'задача-2'])
  assert.deepEqual(by['эпик'], ['фича'])
  assert.deepEqual(by['чужая-фича'], ['задача-3'])
  assert.deepEqual(by['none'], ['эпик', 'сама-по-себе'])
  const epic = groups.find((g) => g.id === 'эпик')!
  assert.equal(epic.title, 'ДОСК-эпик Переезд склада')
  assert.equal(epic.cardId, 'эпик')
  // Одна полоса — по листьям всего поддерева (этап 33.4).
  assert.equal(epic.note, 'готово 1 из 4')
})

test('родитель с чужой доски — дорожка есть, и сказано, чья это доска', () => {
  const base = tree()
  const foreign = groupsOf(base, base.order, 'parent').find((g) => g.id === 'чужая-фича')!
  assert.equal(foreign.title, 'Упаковка')
  assert.equal(foreign.note, 'на доске «Соседи»')
  assert.equal(foreign.cardId, undefined)
})

test('по эпику: дорожка — эпик с портфеля, названный сервером; без эпика — своя дорожка', () => {
  const epic = { id: 'э-1', title: 'Переезд склада', boardId: 'портфель' }
  const base = state([card('фича', { epic }), card('задача', { epic }), card('сама-по-себе')])
  const groups = groupsOf(base, base.order, 'epic')
  const by = lanes(groups)
  assert.deepEqual(by['э-1'], ['фича', 'задача'])
  assert.deepEqual(by['none'], ['сама-по-себе'])
  assert.equal(groups.find((g) => g.id === 'э-1')!.title, 'Переезд склада')
  assert.equal(groups.find((g) => g.id === 'none')!.title, 'Без эпика')
})

test('«Без родителя» держится и пустой — там теряется работа', () => {
  const base = state([card('a'), card('b')], {
    links: [{ fromCard: 'a', toCard: 'b', kind: 'subtask' }],
    order: { [COL]: ['b'] },
  })
  const groups = groupsOf(base, base.order, 'parent')
  assert.deepEqual(
    groups.map((g) => [g.id, g.count]),
    [['a', 1], ['none', 0]],
  )
})

test('группировка живёт в адресе; старая «по корню» открывается «по эпику»', () => {
  assert.equal(parseGrouping(new URLSearchParams('group=root')), 'epic')
  assert.equal(parseGrouping(new URLSearchParams('group=epic')), 'epic')
  assert.equal(groupingToQuery('parent', new URLSearchParams('label=l-1')).toString(), 'label=l-1&group=parent')
  assert.equal(GROUPING_NAMES.epic, 'По эпику')
})
