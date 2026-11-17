import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { fromSnapshot } from '../../entities/board/model.ts'
import type { Card, Snapshot, TreeNode } from '../../shared/api/index.ts'
import { buildTree } from './treeModel.ts'

export function card(id: string, title = id): Card {
  return { id, title, number: `T-${id}`, columnId: 'q', position: id, description: '', version: 1,
    columnEnteredAt: '2026-01-01T00:00:00Z', startedAt: null, finishedAt: null, outcome: null,
    estimate: null, comments: 0, priority: 'medium', dueOn: null, doneAt: null }
}
export function base(cards: Card[]) {
  const snapshot: Snapshot = { board: { id: 'board', name: 'Доска', key: 'T', version: 1, sleDays: null, sleProbability: 85 },
    columns: [{ id: 'q', name: 'Очередь', position: 'a', kind: 'queue', isStartedPoint: false, isFinishedPoint: false, policy: '', wipLimit: null, wipLimitHard: false }],
    cards, links: [], linked: [], iterations: [], cardIterations: {}, fields: [], fieldValues: {}, cardRefs: {}, people: [], labels: [], cardLabels: {}, cardAssignees: {} }
  return fromSnapshot(snapshot)
}

describe('Данные дерева', () => {
  it('отделяет одиночные карточки и берёт свежие локальные поля', () => {
    const b = base([card('1'), card('2'), card('3')])
    b.cards['2'].outcome = 'done'
    b.cards['2'].doneAt = '2026-01-02'
    b.links = [{ fromCard: '1', toCard: '2', kind: 'subtask' }, { fromCard: '1', toCard: '2', kind: 'subtask' }, { fromCard: '1', toCard: '3', kind: 'relates' }]
    const tree = buildTree(b, new Map())
    assert.deepEqual(tree.alone.map((n) => n.id), ['3'])
    assert.equal(tree.roots[0].children.length, 1)
    assert.equal(tree.roots[0].children[0].done, true)
    assert.equal(tree.roots[0].where, 'Очередь')
  })
  it('достраивает чужие ветви и сохраняет закрытые узлы без названий', () => {
    const b = base([card('2')])
    b.links = [{ fromCard: '1', toCard: '2', kind: 'subtask' }]
    b.linked['1'] = { id: '1', title: 'Эпик', boardId: 'other', boardName: 'Портфель', teamName: null, outcome: null, done: false, blocked: false, columnName: '', columnKind: 'queue', sleDays: null, sleProbability: 85, archived: false }
    const remote = new Map<string, TreeNode>([
      ['1', { id: '1', visible: true, number: 'P-1', title: 'Старое название', boardId: 'other', boardName: 'Портфель', columnName: 'В работе', done: false, blocked: false }],
      ['3', { id: '3', parentId: '1', visible: false, done: false, blocked: false }],
      ['4', { id: '4', parentId: '3', visible: true, title: 'Часть', boardId: 'next', done: true, blocked: true }],
    ])
    const root = buildTree(b, remote).roots[0]
    assert.equal(root.title, 'Эпик')
    assert.equal(root.number, 'P-1')
    assert.equal(root.foreign, true)
    assert.equal(root.boardId, 'other')
    const hidden = root.children.find((n) => n.id === '3')!
    assert.equal(hidden.boardId, null)
    assert.ok(hidden.title)
    assert.equal(hidden.children[0].done, true)
    assert.equal(hidden.children[0].blocked, true)
    // Ответ старого запроса не переписывает части своей карточки.
    remote.set('5', { id: '5', parentId: '2', visible: true, done: false, blocked: false })
    assert.equal(buildTree(b, remote).roots[0].children.find((n) => n.id === '2')!.children.length, 0)
  })
  it('не делает бесконечный обход повреждённых связей', () => {
    const b = base([card('1')])
    b.links = [{ fromCard: 'x', toCard: '1', kind: 'subtask' }, { fromCard: 'x', toCard: 'y', kind: 'subtask' }, { fromCard: 'y', toCard: 'x', kind: 'subtask' }]
    assert.doesNotThrow(() => buildTree(b, new Map()))
    const empty = base([])
    assert.deepEqual(buildTree(empty, new Map()), { roots: [], alone: [] })
  })
})
