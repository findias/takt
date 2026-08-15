// Тесты разбора журналов.
//
// Журнал пишется машиной и хранит снимок произошедшего; читают его люди.
// Проверяется именно перевод: снимок колонок в событии перемещения, смысл
// перехода, и то, что незнакомое событие не исчезает молча.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { actorText, auditText, eventText } from './feedModel.ts'
import type { AuditEntry, BoardEvent } from './api.ts'

function event(type: string, payload: Record<string, unknown> = {}): BoardEvent {
  return {
    id: 1,
    cardId: 'card',
    cardTitle: 'Задача',
    actor: 'Иван',
    type,
    payload,
    at: '2026-08-15T10:00:00Z',
  }
}

function entry(action: AuditEntry['action'], subject: string): AuditEntry {
  return {
    id: 1,
    actor: 'Иван',
    action,
    subject,
    subjectId: null,
    payload: {},
    at: '2026-08-15T10:00:00Z',
  }
}

test('перемещение читается по снимку колонок из самого события', () => {
  // В событии лежат названия, а не ссылки: переименование колонки не
  // должно переписывать историю задним числом.
  const moved = event('moved', { from: { name: 'Очередь' }, to: { name: 'В работе' } })
  assert.equal(eventText(moved), 'из «Очередь» в «В работе»')
})

test('пересечение границы потока называется словами', () => {
  const started = event('moved', {
    from: { name: 'Очередь' },
    to: { name: 'В работе' },
    crossedStart: true,
  })
  assert.equal(eventText(started), 'из «Очередь» в «В работе» — работа началась')

  const finished = event('moved', {
    from: { name: 'В работе' },
    to: { name: 'Готово' },
    crossedFinish: true,
  })
  assert.ok(eventText(finished).endsWith('работа закончена'))
})

test('перемещение без снимка колонок не превращается в пустую строку', () => {
  assert.equal(eventText(event('moved')), 'перемещена')
  assert.equal(eventText(event('moved', { to: { name: 'Готово' } })), 'в «Готово»')
})

test('остальные события переводятся по типу', () => {
  assert.equal(eventText(event('created')), 'создана')
  assert.equal(eventText(event('renamed', { title: 'Новое' })), 'переименована в «Новое»')
  assert.equal(eventText(event('archived')), 'убрана с доски')
  assert.equal(eventText(event('blocked', { reason: 'ждём стенд' })), 'заблокирована: ждём стенд')
  assert.equal(eventText(event('unblocked')), 'блокировка снята')
  assert.equal(eventText(event('linked', { kind: 'subtask' })), 'связана: подзадача')
})

test('незнакомое событие показывается как есть, а не исчезает', () => {
  // Событие уже случилось: молчать о нём хуже, чем показать непонятно.
  assert.equal(eventText(event('приснилось')), 'приснилось')
})

test('административные записи называют предмет по-русски', () => {
  assert.equal(auditText(entry('insert', 'teams')), 'Подразделение: добавлено')
  assert.equal(auditText(entry('update', 'memberships')), 'Участие в организации: изменено')
  assert.equal(auditText(entry('delete', 'observers')), 'Наблюдение: удалено')
  // Незнакомая таблица тоже показывается, а не прячется.
  assert.equal(auditText(entry('insert', 'нечто')), 'нечто: добавлено')
})

test('действие без установленной личности видно как таковое', () => {
  // Подделать подпись нельзя, а не назваться — можно: миграция и служебная
  // задача пишут в журнал без автора, и это не потеря данных.
  assert.equal(actorText(null), 'без имени')
  assert.equal(actorText('Иван'), 'Иван')
})
