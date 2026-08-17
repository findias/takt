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
  childrenOf,
  dateWords,
  dueIsBurning,
  dueIsHot,
  dueLabel,
  priorityLabel,
  priorityShort,
  progressLabel,
  progressRatio,
  rangeWords,
} from './model.ts'
import type { BaseState } from '../board/model.ts'
import type { Card, Link, LinkedCard } from '../../shared/api/index.ts'

function card(id: string, title: string, extra: Partial<Card> = {}): Card {
  return {
    id,
    number: `ДОСК-${id}`,
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
    comments: 0,
    priority: 'medium',
    dueOn: null,
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
    columnName: 'Очередь',
    columnKind: 'queue',
    sleDays: null,
    sleProbability: 85,
    archived: false,
    ...extra,
  }
}

function state(cards: Card[], links: Link[], linked: LinkedCard[] = []): BaseState {
  return {
    info: { id: 'board', name: 'Доска', key: 'ДОСК', version: 1, sleDays: null, sleProbability: 85 },
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
    people: {},
    labels: [],
    cardLabels: {},
    cardAssignees: {},
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

test('подзадачи собираются по родителям одним обходом', () => {
  const base = state(
    [
      card('parent', 'Релиз'),
      card('other', 'Другая работа'),
      card('b', 'Бета'),
      card('a', 'Альфа'),
    ],
    [
      { fromCard: 'parent', toCard: 'b', kind: 'subtask' },
      { fromCard: 'parent', toCard: 'a', kind: 'subtask' },
      { fromCard: 'parent', toCard: 'neighbour', kind: 'subtask' },
      // Связь не-подзадачей в разбиение работы не входит.
      { fromCard: 'parent', toCard: 'other', kind: 'blocks' },
      // Родителя нет на этой доске: раскрывать нечего, и в список
      // он не попадает.
      { fromCard: 'elsewhere', toCard: 'other', kind: 'subtask' },
    ],
    [foreign('neighbour', 'Сборка')],
  )

  const children = childrenOf(base)
  assert.deepEqual(Object.keys(children), ['parent'])
  // Порядок — по названию, тот же, что в панели: два разных порядка
  // в двух местах читались бы как два разных списка.
  assert.deepEqual(
    children.parent.map((s) => s.title),
    ['Альфа', 'Бета', 'Сборка'],
  )
  assert.equal(children.parent.find((s) => s.id === 'neighbour')!.onThisBoard, false)
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

test('про чужую подзадачу видно, взяли ли её и когда ждать', () => {
  const base = state(
    [card('parent', 'Релиз')],
    [
      { fromCard: 'parent', toCard: 'queued', kind: 'subtask' },
      { fromCard: 'parent', toCard: 'doing', kind: 'subtask' },
      { fromCard: 'parent', toCard: 'refused', kind: 'subtask' },
      { fromCard: 'parent', toCard: 'opaque', kind: 'subtask' },
    ],
    [
      foreign('queued', 'Квота', { sleDays: 8, sleProbability: 85 }),
      foreign('doing', 'Окно простоя', {
        columnName: 'В работе',
        columnKind: 'in_progress',
        sleDays: 8,
      }),
      foreign('refused', 'Переезд стенда', { archived: true }),
      foreign('opaque', 'Обновить сертификаты', { columnName: 'Приём заявок' }),
    ],
  )
  const byId = Object.fromEntries(cardDetails(base, 'parent')!.subtasks.map((s) => [s.id, s]))

  // Наше слово — про людей, чужое — про место: они не спорят и не
  // дублируются даже тогда, когда колонка названа «В работе».
  assert.equal(byId.queued.stage, 'Ещё не начали · Очередь')
  assert.equal(byId.doing.stage, 'Уже делают · В работе')
  assert.equal(byId.opaque.stage, 'Ещё не начали · Приём заявок')

  // Ответ на «когда будет» — обещание их доски, а не выдуманный срок.
  assert.equal(byId.queued.promise, 'обычно 8 дней с вероятностью 85%')

  // Отказ — это архивация карточки, и читаться он должен отказом,
  // а не отсутствием доступа: искать недоступную карточку бесполезно,
  // а после отказа идут договариваться.
  assert.equal(byId.refused.stage, 'Работу не взяли')
  assert.equal(byId.refused.reachable, true)
  assert.ok(!byId.refused.where.includes('не видно'))
  // Обещания у неё нет: её никто не обещал делать.
  assert.equal(byId.refused.promise, null)
})

test('у карточки этой доски второй строки нет', () => {
  const base = state(
    [card('parent', 'Релиз'), card('mine', 'Своя работа')],
    [{ fromCard: 'parent', toCard: 'mine', kind: 'subtask' }],
  )
  const [sub] = cardDetails(base, 'parent')!.subtasks
  assert.equal(sub.stage, null)
  assert.equal(sub.promise, null)
})

// Пороги срока. Их два, и разница между ними — то, ради чего доска
// вообще считает обещание: тревога на карточке одна, и если её забирает
// срок «через три дня», старение перестаёт быть видно.

test('срок горит только сегодняшний и просроченный, подходит — за трое суток', () => {
  const now = new Date(2026, 7, 17)
  const дни = (n: number) => {
    const d = new Date(2026, 7, 17 + n)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }

  // Горит: из-за этого звонят сегодня.
  assert.equal(dueIsBurning(дни(-1), now), true)
  assert.equal(dueIsBurning(дни(0), now), true)
  assert.equal(dueIsBurning(дни(1), now), true)
  // А это уже не сегодняшний вопрос — тревогу оно не занимает.
  assert.equal(dueIsBurning(дни(2), now), false)
  assert.equal(dueIsBurning(дни(3), now), false)

  // Подходит: вопрос планирования недели, и порог шире.
  assert.equal(dueIsHot(дни(2), now), true)
  assert.equal(dueIsHot(дни(3), now), true)
  assert.equal(dueIsHot(дни(4), now), false)

  // Пусто — не «дата неизвестна», а «обязательства нет»: ни того,
  // ни другого вопроса к такой карточке нет.
  assert.equal(dueIsBurning(null, now), false)
  assert.equal(dueIsHot(null, now), false)
})

test('срок словами: отсчёт от сегодня — на доске, чистая дата — в журнале', () => {
  const now = new Date(2026, 7, 17)

  // Отсчёт, а не число месяца: вопрос к сроку на доске один —
  // «успеваем ли», и «до 21 авг.» человек всё равно переводит
  // в «через четыре дня», каждый раз заново.
  assert.equal(dueLabel('2026-08-17', now).text, 'сегодня')
  assert.equal(dueLabel('2026-08-18', now).text, 'завтра')
  assert.equal(dueLabel('2026-08-21', now).text, 'через 4 дн.')
  assert.equal(dueLabel('2026-08-16', now).text, 'прошёл вчера')
  assert.equal(dueLabel('2026-08-15', now).text, 'прошёл 2 дн. назад')

  // Год — только чужой.
  assert.equal(dateWords('2026-08-21', now), '21 авг.')
  assert.ok(dateWords('2027-01-09', now).includes('2027'))

  // В журнале отсчёта нет: запись о прошлом не имеет права меняться
  // от того, что прошло время.
  assert.equal(dateWords('2026-08-15', now), '15 авг.')
})

test('промежуток не повторяет месяц, когда он один', () => {
  const now = new Date(2026, 7, 17)
  assert.equal(rangeWords('2026-08-13', '2026-08-19', now), '13—19 авг.')
  assert.equal(rangeWords('2026-08-28', '2026-09-03', now), '28 авг. — 3 сент.')
  // Чужой год виден на обоих концах, иначе непонятно, к какому он.
  assert.ok(rangeWords('2027-01-04', '2027-01-10', now).includes('2027'))
})

test('уровень назван дважды: коротко на доске, полно там, где сравнивают', () => {
  // Слова разные не по прихоти: в плашке место меряется знаками,
  // а в панели и таблице уровни сравнивают друг с другом, и там
  // нужен порядок, который виден в самом слове.
  assert.equal(priorityShort('highest'), 'горит')
  assert.equal(priorityLabel('highest'), 'Наивысший')
  assert.equal(priorityShort('low'), 'фоном')
  assert.equal(priorityLabel('low'), 'Низкий')

  // Незнакомый уровень не роняет ни то, ни другое: разошедшиеся
  // клиент и сервер уже давали белый экран однажды.
  const странный = 'выдуманный' as never
  assert.equal(priorityShort(странный), 'выдуманный')
  assert.equal(priorityLabel(странный), 'выдуманный')
})
