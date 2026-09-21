// Тесты модели меток: подписи происхождения и группы. Запуск:
//
//     npm test

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { groupByOrigin, labelOrigin, labelTitle } from './model.ts'
import type { Label } from '../../shared/api/index.ts'

function label(name: string, scope: Label['scope'], scopeId?: string, scopeName?: string): Label {
  return { id: name, name, tone: 'slate', scope, scopeId, scopeName, archived: false }
}

test('происхождение называет подразделение и доску по имени', () => {
  assert.equal(labelOrigin(label('Срочно', 'org')), 'вся организация')
  assert.equal(labelOrigin(label('Инфра', 'team', 't1', 'Платформа')), 'подразделение «Платформа»')
  assert.equal(labelOrigin(label('Своя', 'board', 'b1', 'Склад')), 'доска «Склад»')
})

test('подсказка говорит, что метка убрана', () => {
  const old = { ...label('Ждём', 'org'), archived: true }
  assert.equal(labelTitle(old), 'Ждём — вся организация, в архиве')
})

test('группы идут в порядке сервера, одноимённые подразделения не сливаются', () => {
  const groups = groupByOrigin(
    [
      label('Срочно', 'org'),
      label('Инфра', 'team', 't1', 'Платформа'),
      label('Релиз', 'team', 't1', 'Платформа'),
      label('Прочее', 'team', 't2', 'Платформа'),
      label('Своя', 'board', 'b1', 'Склад'),
    ],
  )
  assert.deepEqual(
    groups.map((g) => [g.title, g.labels.map((l) => l.name)]),
    [
      ['Вся организация', ['Срочно']],
      ['Подразделение «Платформа»', ['Инфра', 'Релиз']],
      ['Подразделение «Платформа»', ['Прочее']],
      ['Доска «Склад»', ['Своя']],
    ],
  )
})
