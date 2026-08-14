// Тесты клиентской модели доски.
//
// Здесь живёт вся логика, из-за которой доска может «поехать»: порядок
// применения команд, слияние патчей и пересборка после конфликта. Запуск:
//
//     npm test

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { applyPatch, fromSnapshot, reconcileColumn, renderOrder } from './boardModel.ts'
import type { MoveCommand } from './boardModel.ts'
import type { Card, Snapshot } from './api.ts'

const COL_A = 'col-a'
const COL_B = 'col-b'

function card(id: string, columnId: string, position: string, title = id): Card {
  return { id, columnId, position, title, description: '', version: 1 }
}

function snapshot(cards: Card[]): Snapshot {
  return {
    board: { id: 'board', name: 'Доска', version: 1 },
    columns: [
      { id: COL_A, name: 'Очередь', position: 'a0', wipLimit: null },
      { id: COL_B, name: 'В работе', position: 'a1', wipLimit: null },
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
    patch: { columns: [{ id: 'col-mid', name: 'Проверка', position: 'a0V', wipLimit: null }] },
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
