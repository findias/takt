// Тесты модели структуры организации.
//
// Здесь проверяется то, из-за чего экран может предложить невозможное:
// сборка дерева, правила переноса и подсчёт глубины поддерева. Запуск:
//
//     npm test

import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  allowedParents,
  buildTree,
  canNestInside,
  counters,
  height,
  subtreeIds,
} from './structureModel.ts'
import type { TreeNode } from './structureModel.ts'
import type { Team } from './api.ts'

function team(id: string, name: string, parentId: string | null, depth: number): Team {
  return { id, name, parentId, depth, members: 0, boards: 0 }
}

// Компания
//   ├── Продажи
//   └── Разработка
//         └── Платформа
//               └── Ядро
const FLAT: Team[] = [
  team('company', 'Компания', null, 1),
  team('dev', 'Разработка', 'company', 2),
  team('sales', 'Продажи', 'company', 2),
  team('platform', 'Платформа', 'dev', 3),
  team('core', 'Ядро', 'platform', 4),
]

function find(nodes: TreeNode[], id: string): TreeNode {
  for (const node of nodes) {
    if (node.id === id) return node
    const inside = node.children.find((child) => child.id === id)
    if (inside) return inside
    for (const child of node.children) {
      const deep = tryFind(child.children, id)
      if (deep) return deep
    }
  }
  throw new Error(`узел ${id} не найден`)
}

function tryFind(nodes: TreeNode[], id: string): TreeNode | undefined {
  for (const node of nodes) {
    if (node.id === id) return node
    const inside = tryFind(node.children, id)
    if (inside) return inside
  }
  return undefined
}

test('дерево собирается из плоского списка, ветви — по алфавиту', () => {
  const tree = buildTree(FLAT)
  assert.equal(tree.length, 1)
  assert.equal(tree[0].id, 'company')
  assert.deepEqual(
    tree[0].children.map((n) => n.name),
    ['Продажи', 'Разработка'],
  )
  assert.equal(find(tree, 'core').children.length, 0)
})

test('узел без видимого родителя показывается корневым, а не теряется', () => {
  // Родителя может быть не видно спрашивающему — это не ошибка данных.
  const tree = buildTree([team('orphan', 'Сирота', 'невидимый', 3)])
  assert.equal(tree.length, 1)
  assert.equal(tree[0].id, 'orphan')
})

test('высота поддерева считается по самой длинной ветви', () => {
  const tree = buildTree(FLAT)
  assert.equal(height(find(tree, 'core')), 1)
  assert.equal(height(find(tree, 'dev')), 3)
  assert.equal(height(tree[0]), 4)
})

test('поддерево перечисляется вместе с корнем', () => {
  const ids = subtreeIds(find(buildTree(FLAT), 'dev'))
  assert.deepEqual(ids.sort(), ['core', 'dev', 'platform'])
})

test('перенос внутрь себя не предлагается', () => {
  const tree = buildTree(FLAT)
  const allowed = allowedParents(tree, find(tree, 'dev')).map((n) => n.id)
  assert.ok(!allowed.includes('dev'), 'узел предложен сам себе в родители')
  assert.ok(!allowed.includes('platform'), 'предложен собственный потомок')
  assert.ok(allowed.includes('sales'), 'соседняя ветвь должна быть доступна')
})

test('перенос считается по высоте поддерева, а не по самому узлу', () => {
  const tree = buildTree(FLAT)
  // «Разработка» тащит за собой два уровня: под «Продажами» (глубина 2)
  // получилось бы 2 + 3 = 5 — ровно предел, значит можно.
  assert.ok(allowedParents(tree, find(tree, 'dev')).some((n) => n.id === 'sales'))

  // А вот если у «Продаж» появится свой отдел, места уже не останется.
  const deeper = buildTree([...FLAT, team('presale', 'Пресейл', 'sales', 3)])
  const allowed = allowedParents(deeper, find(deeper, 'dev')).map((n) => n.id)
  assert.ok(!allowed.includes('presale'), 'предложен родитель, за которым не хватит глубины')
})

test('внутрь узла на предельной глубине вкладывать нельзя', () => {
  const tree = buildTree(FLAT)
  assert.equal(canNestInside(find(tree, 'dev')), true)
  assert.equal(canNestInside({ ...team('deep', 'Пятый', 'x', 5), children: [] }), false)
})

test('подписи склоняются по числу и опускают нули', () => {
  const withCounts = buildTree([{ ...team('a', 'А', null, 1), members: 1, boards: 0 }])
  assert.equal(counters(withCounts[0]), '1 человек')

  const many = buildTree([{ ...team('b', 'Б', null, 1), members: 5, boards: 2 }])
  assert.equal(counters(many[0]), '5 человек · 2 доски')

  const teens = buildTree([{ ...team('c', 'В', null, 1), members: 12, boards: 21 }])
  assert.equal(counters(teens[0]), '12 человек · 21 доска')

  const empty = buildTree([team('d', 'Г', null, 1)])
  assert.equal(counters(empty[0]), '')
})
