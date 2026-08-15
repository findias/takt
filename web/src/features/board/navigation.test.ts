// Ходьба по доске стрелками.
//
// Проверяется то, что делает её похожей на сетку, а не на список
// кнопок: переход вбок сохраняет строку, пустые колонки пропускаются,
// а у края движение никуда не уводит.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { nextCard } from './navigation.ts'

const COLUMNS = ['очередь', 'работа', 'готово']
const ORDER = {
  очередь: ['о1', 'о2', 'о3'],
  работа: ['р1'],
  готово: ['г1', 'г2'],
}

test('вверх и вниз ходят по колонке', () => {
  assert.equal(nextCard(COLUMNS, ORDER, 'о1', 'down'), 'о2')
  assert.equal(nextCard(COLUMNS, ORDER, 'о2', 'up'), 'о1')
})

test('у края выделение остаётся на месте', () => {
  // Перескакивать в соседнюю колонку по вертикали нельзя: человек
  // просил «ниже», а не «в другую колонку».
  assert.equal(nextCard(COLUMNS, ORDER, 'о1', 'up'), 'о1')
  assert.equal(nextCard(COLUMNS, ORDER, 'о3', 'down'), 'о3')
  assert.equal(nextCard(COLUMNS, ORDER, 'о1', 'left'), 'о1')
  assert.equal(nextCard(COLUMNS, ORDER, 'г1', 'right'), 'г1')
})

test('вбок — напротив, а не в начало колонки', () => {
  // Человек ведёт взгляд по строке и ожидает оказаться напротив.
  assert.equal(nextCard(COLUMNS, ORDER, 'г2', 'left'), 'р1', 'в короткой колонке — последняя')
  assert.equal(nextCard(COLUMNS, ORDER, 'о2', 'right'), 'р1')
  assert.equal(nextCard(COLUMNS, ORDER, 'р1', 'right'), 'г1')
})

test('пустые колонки пропускаются', () => {
  const withGap = { очередь: ['о1'], работа: [], готово: ['г1'] }
  // Остановка в пустоте выглядит как потеря выделения, а не как
  // перемещение.
  assert.equal(nextCard(COLUMNS, withGap, 'о1', 'right'), 'г1')
  assert.equal(nextCard(COLUMNS, withGap, 'г1', 'left'), 'о1')
})

test('без выделения любое движение приводит к первой карточке', () => {
  assert.equal(nextCard(COLUMNS, ORDER, null, 'down'), 'о1')
  // Даже если первые колонки пусты.
  assert.equal(nextCard(COLUMNS, { очередь: [], работа: ['р1'] }, null, 'right'), 'р1')
  // А на пустой доске ходить некуда, и это не ошибка.
  assert.equal(nextCard(COLUMNS, { очередь: [], работа: [] }, null, 'down'), null)
})

test('карточка, которой нет в порядке, не двигает выделение', () => {
  // Так бывает, когда карточку скрыл фильтр: молча прыгать
  // в неизвестное место нельзя.
  assert.equal(nextCard(COLUMNS, ORDER, 'скрытая', 'down'), null)
})
