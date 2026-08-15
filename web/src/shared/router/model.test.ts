// Разбор адреса. Проверяется то, ради чего адрес и заводится: ссылка
// на доску и на карточку, присланная коллеге, открывает ровно то, что
// у отправителя.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { boardPath, parseRoute, routePath } from './model.ts'

test('адрес доски и карточки разбирается и собирается обратно', () => {
  assert.deepEqual(parseRoute('/board/b-1'), {
    name: 'board',
    boardId: 'b-1',
    cardId: null,
  })
  assert.deepEqual(parseRoute('/board/b-1/card/c-2'), {
    name: 'board',
    boardId: 'b-1',
    cardId: 'c-2',
  })

  // Собранное обратно совпадает с исходным: иначе переход по своей же
  // ссылке уводил бы не туда.
  assert.equal(boardPath('b-1'), '/board/b-1')
  assert.equal(boardPath('b-1', 'c-2'), '/board/b-1/card/c-2')
  assert.equal(routePath(parseRoute('/board/b-1/card/c-2')), '/board/b-1/card/c-2')
})

test('вкладки и приглашение', () => {
  assert.deepEqual(parseRoute('/'), { name: 'boards' })
  assert.deepEqual(parseRoute('/team'), { name: 'team' })
  assert.deepEqual(parseRoute('/structure'), { name: 'structure' })
  assert.deepEqual(parseRoute('/invite/секрет-123'), { name: 'invite', token: 'секрет-123' })
})

test('непонятный адрес ведёт к списку досок, а не к пустому экрану', () => {
  // Опечатка в ссылке или старый адрес не должны заканчиваться белым
  // экраном: человек попадает туда, откуда сможет дойти сам.
  assert.deepEqual(parseRoute('/чего-то-нет'), { name: 'boards' })
  assert.deepEqual(parseRoute('/board'), { name: 'boards' })
  assert.deepEqual(parseRoute(''), { name: 'boards' })
})

test('лишние слеши не меняют смысла', () => {
  assert.deepEqual(parseRoute('//board//b-1//'), {
    name: 'board',
    boardId: 'b-1',
    cardId: null,
  })
})
