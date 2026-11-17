import assert from 'node:assert/strict'
import { test } from 'node:test'
import { parentPath, parentReturn } from './parent.ts'

test('родитель с другой доски сохраняет отбор задач и точный возврат', () => {
  const start = '/tasks?user=u&done=1&label=l&open=b:s'
  const next = new URL(parentPath(start, 'other', 'p', 'b:s'), 'https://example.test')
  assert.equal(next.pathname, '/tasks')
  assert.equal(next.searchParams.get('open'), 'other:p')
  assert.equal(next.searchParams.get('label'), 'l')
  assert.equal(next.searchParams.get('user'), 'u')
  assert.equal(next.searchParams.get('done'), '1')
  assert.equal(next.searchParams.get('parentReturn'), start)
  assert.equal(next.searchParams.get('parentFrom'), 'b:s')
  const ancestor = new URL(parentPath(next.pathname + next.search, 'root', 'r', 'other:p'), next)
  assert.equal(ancestor.searchParams.get('parentReturn'), start)
})

test('переход между досками не переносит чужой отбор', () => {
  const local = new URL(parentPath('/board/b/card/s?label=l', 'b', 'p', 'b:s'), 'https://example.test')
  assert.equal(local.searchParams.get('label'), 'l')
  const other = new URL(parentPath('/board/b/card/s?label=l', 'other', 'p', 'b:s'), local)
  assert.equal(other.pathname, '/board/other/card/p')
  assert.equal(other.searchParams.has('label'), false)
  assert.equal(parentReturn(other.searchParams.get('parentReturn')), '/board/b/card/s?label=l')
})

test('возврат не принимает внешние адреса и посторонние страницы', () => {
  for (const value of [null, '', '//example.test', 'https://example.test', '/team', '/tasks-no', '/tasks\\evil', '/tasks#bad']) assert.equal(parentReturn(value), null)
  assert.equal(parentReturn('/tasks'), '/tasks')
  assert.equal(parentReturn('/board/b/card/c'), '/board/b/card/c')
})
