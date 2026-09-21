// Ширина колонки: пределы и хранение (ROADMAP 28.2). Запуск:
//
//     npm test

import assert from 'node:assert/strict'
import { beforeEach, test } from 'node:test'
import {
  COLUMN_WIDTH_DEFAULT,
  COLUMN_WIDTH_MAX,
  COLUMN_WIDTH_MIN,
  clampColumnWidth,
  readColumnWidths,
  writeColumnWidth,
} from './columnWidth.ts'

// Хранилище браузера в node подменяется: проверяется, что лежит
// и что читается, а не как браузер это хранит.
const store = new Map<string, string>()
globalThis.localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
  clear: () => store.clear(),
  key: () => null,
  length: 0,
} as Storage

beforeEach(() => store.clear())

test('пределы: 12rem снизу, 40rem сверху, по умолчанию прежние 17.5rem', () => {
  assert.equal(COLUMN_WIDTH_MIN, 12)
  assert.equal(COLUMN_WIDTH_MAX, 40)
  assert.equal(COLUMN_WIDTH_DEFAULT, 17.5)
  assert.equal(clampColumnWidth(5), 12)
  assert.equal(clampColumnWidth(100), 40)
  assert.equal(clampColumnWidth(20.25), 20.25)
  // Нечисло из испорченного хранилища — ширина по умолчанию, а не NaN
  // в стиле, от которого колонка схлопнется.
  assert.equal(clampColumnWidth(Number.NaN), 17.5)
})

test('ширина хранится по доске и колонке, и чужая доска её не видит', () => {
  writeColumnWidth('доска-1', 'готово', 14)
  writeColumnWidth('доска-1', 'в-работе', 30)
  writeColumnWidth('доска-2', 'готово', 25)
  assert.deepEqual(readColumnWidths('доска-1'), { готово: 14, 'в-работе': 30 })
  assert.deepEqual(readColumnWidths('доска-2'), { готово: 25 })
  assert.deepEqual(readColumnWidths('доска-3'), {})
})

test('сброс убирает запись, а не пишет ширину по умолчанию', () => {
  // Иначе смена умолчания когда-нибудь не дойдёт до тех, кто однажды
  // дважды щёлкнул по ручке.
  writeColumnWidth('доска', 'готово', 14)
  writeColumnWidth('доска', 'готово', null)
  assert.deepEqual(readColumnWidths('доска'), {})
})

test('записанное вне пределов при чтении возвращается в пределы', () => {
  store.set('column-widths', JSON.stringify({ доска: { готово: 3, длинная: 90 } }))
  assert.deepEqual(readColumnWidths('доска'), { готово: 12, длинная: 40 })
})

test('испорченное хранилище — не повод не показать доску', () => {
  store.set('column-widths', '{не json')
  assert.deepEqual(readColumnWidths('доска'), {})
  store.set('column-widths', JSON.stringify({ доска: 'строка' }))
  assert.deepEqual(readColumnWidths('доска'), {})
})
