// Тесты клиентской модели доски.
//
// Здесь живёт вся логика, из-за которой доска может «поехать»: порядок
// применения команд, слияние патчей и пересборка после конфликта. Запуск:
//
//     npm test

import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  agingLabel,
  applyPatch,
  flowIssues,
  flowMarks,
  fromSnapshot,
  limitLabel,
  parseLimitDraft,
  reconcileColumn,
  renderOrder,
} from './model.ts'
import type { MoveCommand } from './model.ts'
import type { Card, Column, ColumnKind, Snapshot } from '../../shared/api/index.ts'
import { initials } from '../../shared/lib/initials.ts'

const COL_A = 'col-a'
const COL_B = 'col-b'

function card(id: string, columnId: string, position: string, title = id): Card {
  return {
    id,
    number: `ДОСК-${id}`,
    columnId,
    position,
    title,
    description: '',
    version: 1,
    columnEnteredAt: '2026-08-14T12:00:00Z',
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
  }
}

// Порядок карточек не зависит от семантики колонки, поэтому здесь она
// заполняется значениями по умолчанию — интересны только id и position.
function column(id: string, name: string, position: string, kind: ColumnKind): Column {
  return {
    id,
    name,
    position,
    kind,
    isStartedPoint: false,
    isFinishedPoint: false,
    policy: '',
    wipLimit: null,
    wipLimitHard: false,
  }
}

function snapshot(cards: Card[]): Snapshot {
  return {
    board: { id: 'board', name: 'Доска', key: 'ДОСК', version: 1, sleDays: null, sleProbability: 85 },
    links: [],
    linked: [],
    iterations: [],
    cardIterations: {},
    fields: [],
    fieldValues: {},
    people: [],
    labels: [],
    cardLabels: {},
    cardAssignees: {},
    columns: [
      column(COL_A, 'Очередь', 'a0', 'queue'),
      column(COL_B, 'В работе', 'a1', 'in_progress'),
    ],
    cards,
  }
}

function move(cardId: string, toColumnId: string, placement: MoveCommand['placement']): MoveCommand {
  return {
    operationId: `op-${cardId}-${Math.random()}`,
    cardId,
    toColumnId,
    placement,
    fromColumnId: COL_A,
  }
}

test('снимок раскладывается по колонкам в порядке позиций', () => {
  const base = fromSnapshot(
    snapshot([card('3', COL_A, 'a2'), card('1', COL_A, 'a0'), card('2', COL_A, 'a1')]),
  )
  assert.deepEqual(base.order[COL_A], ['1', '2', '3'])
  assert.deepEqual(base.order[COL_B], [])
  assert.deepEqual(base.columnIds, [COL_A, COL_B])
})

test('неподтверждённые команды применяются поверх базы по порядку', () => {
  const base = fromSnapshot(
    snapshot([card('1', COL_A, 'a0'), card('2', COL_A, 'a1'), card('3', COL_A, 'a2')]),
  )
  const order = renderOrder(base, [
    move('3', COL_A, { place: 'start' }),
    move('1', COL_A, { place: 'end' }),
  ])
  assert.deepEqual(order[COL_A], ['3', '2', '1'])
  // база не меняется: она — то, что подтвердил сервер
  assert.deepEqual(base.order[COL_A], ['1', '2', '3'])
})

test('перемещение в другую колонку убирает карточку из исходной', () => {
  const base = fromSnapshot(snapshot([card('1', COL_A, 'a0'), card('2', COL_A, 'a1')]))
  const order = renderOrder(base, [move('1', COL_B, { place: 'end' })])
  assert.deepEqual(order[COL_A], ['2'])
  assert.deepEqual(order[COL_B], ['1'])
})

test('place = after ставит карточку сразу за якорем', () => {
  const base = fromSnapshot(
    snapshot([card('1', COL_A, 'a0'), card('2', COL_A, 'a1'), card('3', COL_A, 'a2')]),
  )
  const order = renderOrder(base, [move('3', COL_A, { place: 'after', afterCardId: '1' })])
  assert.deepEqual(order[COL_A], ['1', '3', '2'])
})

test('исчезнувший якорь не роняет отрисовку', () => {
  // Сервер всё равно ответит конфликтом и пришлёт настоящий порядок,
  // но до этого момента доска обязана остаться отрисуемой.
  const base = fromSnapshot(snapshot([card('1', COL_A, 'a0'), card('2', COL_A, 'a1')]))
  const order = renderOrder(base, [move('1', COL_A, { place: 'after', afterCardId: 'призрак' })])
  assert.deepEqual(order[COL_A], ['2', '1'])
})

test('патч с перемещением обновляет базу и версию доски', () => {
  const base = fromSnapshot(
    snapshot([card('1', COL_A, 'a0'), card('2', COL_A, 'a1'), card('3', COL_A, 'a2')]),
  )
  const next = applyPatch(base, {
    version: 7,
    patch: { cards: [{ ...card('3', COL_A, 'Zz'), version: 2 }] },
  })
  assert.deepEqual(next.order[COL_A], ['3', '1', '2'])
  assert.equal(next.info.version, 7)
  assert.equal(next.cards['3'].position, 'Zz')
})

test('патч с новой карточкой вставляет её по позиции', () => {
  const base = fromSnapshot(snapshot([card('1', COL_A, 'a0'), card('3', COL_A, 'a2')]))
  const next = applyPatch(base, { version: 2, patch: { cards: [card('2', COL_A, 'a1')] } })
  assert.deepEqual(next.order[COL_A], ['1', '2', '3'])
})

test('патч с удалением убирает карточку отовсюду', () => {
  const base = fromSnapshot(snapshot([card('1', COL_A, 'a0'), card('2', COL_A, 'a1')]))
  const next = applyPatch(base, { version: 3, patch: { removedCardIds: ['1'] } })
  assert.deepEqual(next.order[COL_A], ['2'])
  assert.equal(next.cards['1'], undefined)
})

test('патч с новой колонкой добавляет её в правильное место', () => {
  const base = fromSnapshot(snapshot([]))
  const next = applyPatch(base, {
    version: 2,
    patch: { columns: [column('col-mid', 'Проверка', 'a0V', 'in_progress')] },
  })
  assert.deepEqual(next.columnIds, [COL_A, 'col-mid', COL_B])
  assert.deepEqual(next.order['col-mid'], [])
})

test('конфликт пересобирает колонку из порядка, присланного сервером', () => {
  const base = fromSnapshot(
    snapshot([card('1', COL_A, 'a0'), card('2', COL_A, 'a1'), card('3', COL_A, 'a2')]),
  )
  const next = reconcileColumn(base, COL_A, [
    { id: '2', position: 'a1' },
    { id: '1', position: 'a1V' },
    { id: '3', position: 'a2' },
  ])
  assert.ok(next)
  assert.deepEqual(next.order[COL_A], ['2', '1', '3'])
  assert.equal(next.cards['1'].position, 'a1V')
})

test('конфликт с незнакомой карточкой требует полного снимка', () => {
  const base = fromSnapshot(snapshot([card('1', COL_A, 'a0')]))
  const next = reconcileColumn(base, COL_A, [
    { id: '1', position: 'a0' },
    { id: 'чужая', position: 'a1' },
  ])
  // null — сигнал вызывающему коду перезагрузить доску целиком
  assert.equal(next, null)
})

test('карточка, перенесённая в другую колонку, не задваивается при пересборке', () => {
  const base = fromSnapshot(snapshot([card('1', COL_A, 'a0'), card('2', COL_B, 'a0')]))
  const next = reconcileColumn(base, COL_A, [
    { id: '1', position: 'a0' },
    { id: '2', position: 'a1' },
  ])
  assert.ok(next)
  assert.deepEqual(next.order[COL_A], ['1', '2'])
  assert.deepEqual(next.order[COL_B], [])
})

// --- лимит колонки ---

test('счётчик показывает лимит и краснеет только при превышении', () => {
  assert.deepEqual(limitLabel(3, null), { label: '3', over: false })
  assert.deepEqual(limitLabel(2, 3), { label: '2/3', over: false })
  assert.deepEqual(limitLabel(3, 3), { label: '3/3', over: false })
  assert.deepEqual(limitLabel(4, 3), { label: '4/3', over: true })
})

test('пустое поле снимает лимит, а мусор ничего не меняет', () => {
  assert.deepEqual(parseLimitDraft('', 3), { change: true, limit: null })
  // Снимать нечего — операция не нужна.
  assert.deepEqual(parseLimitDraft('   ', null), { change: false })
  assert.deepEqual(parseLimitDraft('5', 3), { change: true, limit: 5 })
  assert.deepEqual(parseLimitDraft(' 5 ', null), { change: true, limit: 5 })

  for (const draft of ['0', '-2', '1.5', 'три', '2e3и']) {
    assert.deepEqual(parseLimitDraft(draft, 3), { change: false }, `черновик ${draft}`)
  }
  // Повтор текущего значения — тоже ничего.
  assert.deepEqual(parseLimitDraft('3', 3), { change: false })
})

test('разметка колонки описывается подписями', () => {
  const plain = column(COL_A, 'Очередь', 'a0', 'queue')
  assert.deepEqual(flowMarks(plain), [])

  const work = { ...column(COL_B, 'В работе', 'a1', 'in_progress'), isStartedPoint: true }
  assert.deepEqual(flowMarks(work), ['начало работы'])

  // Жёсткий лимит без самого лимита ничего не значит и не показывается.
  assert.deepEqual(flowMarks({ ...work, wipLimitHard: true }), ['начало работы'])
  assert.deepEqual(flowMarks({ ...work, wipLimitHard: true, wipLimit: 3 }), [
    'начало работы',
    'жёсткий лимит',
  ])
})

test('доска без точек потока сообщает, чего в ней не посчитать', () => {
  const columns = [
    column(COL_A, 'Очередь', 'a0', 'queue'),
    column(COL_B, 'В работе', 'a1', 'in_progress'),
  ]
  const issues = flowIssues(columns)
  assert.equal(issues.length, 2, `ожидались две жалобы, получено: ${issues.join('; ')}`)
  assert.ok(issues.some((i) => i.includes('начало работы')))
  assert.ok(issues.some((i) => i.includes('финиш')))

  const marked = [
    columns[0],
    { ...columns[1], isStartedPoint: true },
    { ...column('col-c', 'Готово', 'a2', 'done'), isFinishedPoint: true },
  ]
  assert.deepEqual(flowIssues(marked), [])
})

test('финиш раньше начала — почти наверняка ошибка разметки', () => {
  const columns = [
    { ...column(COL_A, 'Готово', 'a0', 'done'), isFinishedPoint: true },
    { ...column(COL_B, 'В работе', 'a1', 'in_progress'), isStartedPoint: true },
  ]
  const issues = flowIssues(columns)
  assert.equal(issues.length, 1)
  assert.ok(issues[0].includes('раньше начала'))
})

// Возраст против обещания: метка появляется только у той работы, которая
// идёт дольше обещанного, — и только пока она идёт.
test('метка возраста появляется, когда карточка перешагнула обещание', () => {
  const now = Date.parse('2026-08-15T12:00:00Z')
  const started = (days: number) =>
    new Date(now - days * 86_400_000).toISOString()

  assert.equal(agingLabel({ startedAt: started(3), finishedAt: null }, 8, now), null)
  assert.match(
    agingLabel({ startedAt: started(9), finishedAt: null }, 8, now) ?? '',
    /Идёт 9 дн\. — дольше обещанных 8/,
  )
})

test('без обещания, до начала и после конца метки нет', () => {
  const now = Date.parse('2026-08-15T12:00:00Z')
  const long = new Date(now - 40 * 86_400_000).toISOString()

  // Обещания нет — сравнивать не с чем.
  assert.equal(agingLabel({ startedAt: long, finishedAt: null }, null, now), null)
  // Работа ещё не начиналась: возраст считается от начала, а не от заведения.
  assert.equal(agingLabel({ startedAt: null, finishedAt: null }, 8, now), null)
  // Работа доведена до конца — стареть уже нечему.
  assert.equal(
    agingLabel({ startedAt: long, finishedAt: new Date(now).toISOString() }, 8, now),
    null,
  )
})

// Инициалы: то, чем подписана карточка.
test('инициалы собираются из имени, а не из пробелов', () => {
  assert.equal(initials('Мария Кузнецова'), 'МК')
  assert.equal(initials('Иван'), 'ИВ')
  // Лишние пробелы и второе имя не должны ломать подпись.
  assert.equal(initials('  Пётр   Сергеевич Иванов '), 'ПС')
  // Пустое имя бывает у неполных данных — показываем вопрос, а не пустоту.
  assert.equal(initials('   '), '?')
})
