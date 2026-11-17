import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { branchIds, treeColumns, treeRows } from './treeRows.ts'
import type { WorkNode } from './treeModel.ts'

function node(id: string, title = id, children: WorkNode[] = []): WorkNode {
  return { id, title, number: `T-${id}`, children, own: null, where: '', boardId: null, done: false, blocked: false, foreign: false }
}

describe('Иерархическая таблица', () => {
  it('разбирает только известные столбцы и отличает пустой набор от умолчания', () => {
    assert.deepEqual(treeColumns(null), ['where', 'priority', 'progress'])
    assert.deepEqual(treeColumns('progress,unknown,progress,where'), ['where', 'progress'])
    assert.deepEqual(treeColumns('none'), [])
  })
  it('сортирует соседей по номеру естественно, не разрывая ветки', () => {
    const roots = [node('10'), node('2', 'Второй', [node('12'), node('3')])]
    assert.deepEqual(treeRows(roots, new Set(), '', 'number').map((r) => [r.node.id, r.depth]), [['2', 0], ['3', 1], ['12', 1], ['10', 0]])
    assert.deepEqual(roots.map((n) => n.id), ['10', '2'])
  })
  it('сворачивает ветку, но не соседние задачи; собирает только ветви', () => {
    const roots = [node('1', 'Родитель', [node('2', 'Часть', [node('3')])]), node('4')]
    assert.deepEqual(branchIds(roots), ['1', '2'])
    assert.deepEqual(treeRows(roots, new Set(['1']), '', '').map((r) => r.node.id), ['1', '4'])
  })
  it('находит заголовок и номер без учёта регистра, сохраняя предков', () => {
    const roots = [node('1', 'Эпик', [node('2', 'Поставка', [node('3', 'Склад')]), node('4', 'Другое')])]
    const rows = treeRows(roots, new Set(['1', '2']), '  СКЛАД ', 'title')
    assert.deepEqual(rows.map((r) => [r.node.id, r.context, r.open]), [['1', true, true], ['2', true, true], ['3', false, true]])
    assert.deepEqual(treeRows(roots, new Set(), 't-2', '').map((r) => r.node.id), ['1', '2', '3'])
    assert.deepEqual(treeRows(roots, new Set(), 'несуществующее', ''), [])
  })
  it('одинаковые подписи упорядочивает по id', () => {
    const a = node('a', 'Одинаково'); a.number = null
    const b = node('b', 'Одинаково'); b.number = null
    assert.deepEqual(treeRows([b, a], new Set(), '', 'title').map((r) => r.node.id), ['a', 'b'])
    assert.deepEqual(treeRows([b, a], new Set(), '', 'number').map((r) => r.node.id), ['a', 'b'])
  })
})
