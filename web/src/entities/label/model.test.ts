// Тесты модели меток: подписи происхождения и группы. Запуск:
//
//     npm test

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chipClass, dotClass, groupByOrigin, labelOrigin, labelTitle, pickerItems } from './model.ts'
import type { BoardLabel, Label, LabelPlace } from '../../shared/api/index.ts'

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

function onBoard(name: string, extra: Partial<BoardLabel> = {}): BoardLabel {
  return { ...label(name, 'org'), offered: true, applies: true, ...extra }
}

const PLACES: LabelPlace[] = [
  { scope: 'org', name: '' },
  { scope: 'team', id: 't1', name: 'Разработка' },
  { scope: 'team', id: 't2', name: 'Платформа' },
  { scope: 'board', id: 'b1', name: 'Платформа' },
]

const kinds = (items: ReturnType<typeof pickerItems>) =>
  items.map((i) =>
    i.kind === 'create' ? `create:${i.place.scope}:${i.place.name}` : `${i.kind}:${i.label.name}`,
  )

test('без набранного — существующие, висящие отмечены, заводить нечего', () => {
  const items = pickerItems('', [onBoard('Срочно'), onBoard('Риск')], ['Срочно'], PLACES)
  assert.deepEqual(kinds(items), ['toggle:Срочно', 'toggle:Риск'])
  assert.equal(items[0].kind === 'toggle' && items[0].checked, true)
})

test('совпадение в другом регистре предлагает существующую, а не вторую', () => {
  assert.deepEqual(kinds(pickerItems('  срочно ', [onBoard('Срочно')], [], PLACES)), ['toggle:Срочно'])
})

test('новое название — заведение последним, от узкого места к широкому', () => {
  assert.deepEqual(kinds(pickerItems('Сроч дело', [onBoard('Срочно')], [], PLACES)), [
    'create:board:Платформа',
    'create:team:Платформа',
    'create:team:Разработка',
    'create:org:',
  ])
  // Подстрока находит существующую, и заведение стоит после неё.
  assert.deepEqual(kinds(pickerItems('сроч', [onBoard('Срочно')], [], PLACES)).slice(0, 2), [
    'toggle:Срочно',
    'create:board:Платформа',
  ])
})

test('название убранной — вернуть из архива, завести не предлагается', () => {
  const old = onBoard('Ждём', { archived: true, offered: false })
  assert.deepEqual(kinds(pickerItems('ждём', [old], [], PLACES)), ['restore:Ждём'])
})

test('убранная, которая здесь не действует, не возвращается отсюда', () => {
  const foreign = onBoard('Ждём', { archived: true, offered: false, applies: false })
  assert.deepEqual(kinds(pickerItems('ждём', [foreign], [], PLACES)).at(-1), 'create:org:')
})

test('висящую чужую можно снять, а не повесить заново — у массового её нет', () => {
  const foreign = onBoard('Чужая', { offered: false, applies: false })
  assert.deepEqual(kinds(pickerItems('', [foreign], ['Чужая'], PLACES)), ['toggle:Чужая'])
  assert.deepEqual(kinds(pickerItems('', [foreign], null, PLACES)), [])
})

test('без прав заводить — только существующие', () => {
  assert.deepEqual(kinds(pickerItems('Новая', [onBoard('Срочно')], [], [])), [])
})

// Метка человека из переноса рисуется контуром, а не своим тоном:
// тон у неё есть (колонка обязательная), но он ничего не значит.
test('метка человека рисуется контуром, обычная — своим тоном', () => {
  assert.equal(chipClass({ tone: 'green' }), 'chip chip--green')
  assert.equal(chipClass({ tone: 'slate', kind: 'person' }), 'chip chip--person')
  assert.equal(dotClass({ tone: 'slate', kind: 'person' }), 'label-dot label-dot--person')
  assert.equal(dotClass({ tone: 'rose', kind: 'regular' }), 'label-dot label-dot--rose')
})
