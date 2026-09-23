// Отбор вкладки «Задачи»: чистые функции, проверяются без браузера.
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { filterTasks, isFiltered, labelsOf, parseTaskFilter } from './tasksFilter.ts'
import type { Label, Task } from '../shared/api/index.ts'

const NOW = new Date(2026, 8, 23, 12)
const label = (id: string, name: string) => ({ id, name }) as Label
const task = (id: string, over: Partial<Task> = {}): Task => ({
  id,
  number: id,
  title: id,
  boardId: 'b',
  boardName: 'Доска',
  column: 'Колонка',
  columnKind: 'in_progress',
  priority: 'medium',
  dueOn: null,
  startedAt: null,
  columnEnteredAt: '2026-09-01T00:00:00Z',
  outcome: null,
  blocked: false,
  labels: [],
  ...over,
})
const ids = (list: Task[]) => list.map((t) => t.id)

const tasks = [
  task('очередь', { columnKind: 'queue', dueOn: '2026-09-20', labels: [label('l1', 'Склад')] }),
  task('делают', { dueOn: '2026-09-25' }),
  task('стоит', { blocked: true, dueOn: '2026-10-30', labels: [label('l2', 'Архив'), label('l1', 'Склад')] }),
  task('готово', { columnKind: 'done' }),
]

test('без отбора видно всё, и отбор знает, что он пустой', () => {
  const f = parseTaskFilter(new URLSearchParams())
  assert.equal(isFiltered(f), false)
  assert.deepEqual(ids(filterTasks(tasks, f, NOW)), ids(tasks))
})

test('чужое значение в адресе не отсеивает всё подряд', () => {
  const f = parseTaskFilter(new URLSearchParams('status=закрыта&due=вчера'))
  assert.equal(isFiltered(f), false)
})

test('статус — вид колонки, а заблокированные — отдельный пункт', () => {
  const by = (q: string) => ids(filterTasks(tasks, parseTaskFilter(new URLSearchParams(q)), NOW))
  assert.deepEqual(by('status=queue'), ['очередь'])
  assert.deepEqual(by('status=in_progress'), ['делают', 'стоит'])
  assert.deepEqual(by('status=done'), ['готово'])
  assert.deepEqual(by('status=blocked'), ['стоит'])
})

test('срок: прошёл, ближайшие три дня, без срока — не пересекаются', () => {
  const by = (q: string) => ids(filterTasks(tasks, parseTaskFilter(new URLSearchParams(q)), NOW))
  assert.deepEqual(by('due=overdue'), ['очередь'])
  assert.deepEqual(by('due=soon'), ['делают'])
  assert.deepEqual(by('due=none'), ['готово'])
})

test('метка и прочий отбор складываются через «и»', () => {
  const by = (q: string) => ids(filterTasks(tasks, parseTaskFilter(new URLSearchParams(q)), NOW))
  assert.deepEqual(by('label=l1'), ['очередь', 'стоит'])
  assert.deepEqual(by('label=l1&status=blocked'), ['стоит'])
})

test('в выборе меток — только метки этих задач, по имени и без повторов', () => {
  assert.deepEqual(
    labelsOf(tasks).map((l) => l.name),
    ['Архив', 'Склад'],
  )
})
