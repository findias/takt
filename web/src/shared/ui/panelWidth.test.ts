// Ширина боковой панели: пределы и хранение. Запуск:
//
//     npm test

import assert from 'node:assert/strict'
import { beforeEach, test } from 'node:test'
import {
  PANEL_WIDTH_DEFAULT,
  PANEL_WIDTH_MAX,
  PANEL_WIDTH_MIN,
  clampPanelWidth,
  readPanelWidth,
  writePanelWidth,
} from './panelWidth.ts'

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

test('пределы: 20rem снизу, 64rem сверху, мусор — прежние 26rem', () => {
  assert.equal(clampPanelWidth(5), PANEL_WIDTH_MIN)
  assert.equal(clampPanelWidth(500), PANEL_WIDTH_MAX)
  assert.equal(clampPanelWidth(Number.NaN), PANEL_WIDTH_DEFAULT)
  assert.equal(clampPanelWidth(30), 30)
})

test('не трогали — ширины нет, а не умолчание', () => {
  assert.equal(readPanelWidth(), null)
})

test('записанное читается, сброс убирает запись', () => {
  writePanelWidth(40)
  assert.equal(readPanelWidth(), 40)
  writePanelWidth(null)
  assert.equal(store.has('panel-side-width'), false)
})

test('записанное вне пределов и испорченное возвращаются в пределы', () => {
  store.set('panel-side-width', '200')
  assert.equal(readPanelWidth(), PANEL_WIDTH_MAX)
  store.set('panel-side-width', 'широко')
  assert.equal(readPanelWidth(), null)
})
