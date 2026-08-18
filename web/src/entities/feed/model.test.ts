// Тесты разбора журналов.
//
// Журнал пишется машиной и хранит снимок произошедшего; читают его люди.
// Проверяется именно перевод: снимок колонок в событии перемещения, смысл
// перехода, и то, что незнакомое событие не исчезает молча.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { actorText, auditText, eventText } from './model.ts'
import { WEBHOOK_EVENT_NAMES } from '../../shared/api/events.ts'
import type { AuditEntry, BoardEvent } from '../../shared/api/index.ts'

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

function entry(
  action: AuditEntry['action'],
  subject: string,
  payload: Record<string, unknown> = {},
): AuditEntry {
  return {
    id: 1,
    actor: 'Иван',
    action,
    subject,
    subjectId: null,
    payload,
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

test('своё поле в журнале названо по имени, а не ссылкой', () => {
  // `field_set` с полем `f-17` — машинная строка посреди речи: имя поля
  // лежит в снимке доски, и без него журнал читать нечем.
  const fields = [
    { id: 'f-17', name: 'Заказчик', kind: 'text' as const, options: [] },
    { id: 'f-18', name: 'Приёмка', kind: 'date' as const, options: [] },
    { id: 'f-19', name: 'Согласовано', kind: 'checkbox' as const, options: [] },
  ]
  assert.equal(
    eventText(event('field_set', { fieldId: 'f-17', value: 'ООО «Ромашка»' }), fields),
    '«Заказчик»: ООО «Ромашка»',
  )
  // Дата переводится по виду поля, а не по виду строки: угадывать дату
  // в тексте нельзя — так пишут и обычные строки.
  assert.match(eventText(event('field_set', { fieldId: 'f-18', value: '2026-08-21' }), fields), /21 авг/)
  assert.equal(
    eventText(event('field_set', { fieldId: 'f-19', value: true }), fields),
    '«Согласовано»: да',
  )
  assert.equal(
    eventText(event('field_cleared', { fieldId: 'f-17' }), fields),
    'поле «Заказчик» очищено',
  )
})

test('поле, которого уже нет, не превращает журнал в машинную строку', () => {
  // Поле удалили — событие о нём осталось. Общие слова лучше, чем
  // `field_set`, и лучше, чем пустая строка.
  assert.equal(eventText(event('field_set', { fieldId: 'ушло', value: 1 })), 'заполнено своё поле')
  assert.equal(eventText(event('field_cleared', { fieldId: 'ушло' })), 'своё поле очищено')
})

test('переведены все виды событий, а не почти все', () => {
  // Два вида из двадцати остались непереведёнными и показывались как
  // `field_set` — и заметить это можно было только глазами на нужной
  // карточке. Список видов один на всё приложение: он же объявляет
  // подписки, и сверяться с ним дешевле, чем помнить.
  const raw = Object.keys(WEBHOOK_EVENT_NAMES)
    .map((name) => name.replace(/^card\./, ''))
    .filter((kind) => eventText(event(kind, { fieldId: 'f-1', value: 'что-то' })) === kind)
  assert.deepEqual(raw, [])
})

test('остальные события переводятся по типу', () => {
  assert.equal(eventText(event('created')), 'создана')
  assert.equal(eventText(event('renamed', { title: 'Новое' })), 'переименована в «Новое»')
  assert.equal(eventText(event('archived')), 'убрана с доски')
  assert.equal(eventText(event('blocked', { reason: 'ждём стенд' })), 'заблокирована: ждём стенд')
  assert.equal(eventText(event('unblocked')), 'блокировка снята')
  assert.equal(eventText(event('linked', { kind: 'subtask' })), 'связана: подзадача')
})

test('срок в журнале — словами и без отсчёта от сегодня', () => {
  // Машинная дата посреди речи читается как чужая строка: вся лента
  // вокруг говорит «работа началась» и «блокировка снята».
  assert.equal(eventText(event('committed', { dueOn: '2026-08-21' })), 'обязательство: 21 авг.')

  // И без «просрочено»: запись рассказывает о том, что случилось тогда,
  // и не имеет права меняться от того, что прошло время.
  assert.ok(!eventText(event('committed', { dueOn: '2020-01-09' })).includes('просрочено'))

  // Снятое обязательство — не пустая дата, а другое событие.
  assert.equal(eventText(event('committed')), 'обязательство снято')
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

test('запись называет то, с чем это произошло, и что в нём изменилось', () => {
  // «Доска: изменено» три раза подряд — список таблиц, а не журнал.
  // Имя и обе стороны лежат в снимке, который и так приходит с записью.
  assert.equal(
    auditText(entry('insert', 'boards', { new: { name: 'Поставки', visibility: 'org' } })),
    'Доска «Поставки»: добавлено',
  )
  assert.equal(
    auditText(
      entry('update', 'boards', {
        old: { name: 'Поставки', visibility: 'org' },
        new: { name: 'Поставки', visibility: 'private' },
      }),
    ),
    'Доска «Поставки»: изменено — видимость: всей организации → только вписанным',
  )
  // Несколько изменений — по именам: перечисление переходов не влезает
  // в строку ленты.
  assert.equal(
    auditText(
      entry('update', 'boards', {
        old: { name: 'Поставки', key: 'ПОСТ' },
        new: { name: 'Закупки', key: 'ЗАК' },
      }),
    ),
    'Доска «Закупки»: изменено — название, ключ',
  )
  // Роль меняется у того, у кого имени в снимке нет: строка всё равно
  // отвечает на «что случилось».
  assert.equal(
    auditText(
      entry('update', 'memberships', { old: { role: 'member' }, new: { role: 'viewer' } }),
    ),
    'Участие в организации: изменено — роль: участник → наблюдатель',
  )
  // Служебные поля меняются сами и ничего не рассказывают: запись
  // по ним выглядела бы изменённой, ничем не отличаясь от соседней.
  assert.equal(
    auditText(
      entry('update', 'boards', {
        old: { name: 'Поставки', version: 1 },
        new: { name: 'Поставки', version: 2 },
      }),
    ),
    'Доска «Поставки»: изменено',
  )
  // Старой записи без снимка это не ломает.
  assert.equal(auditText(entry('update', 'boards')), 'Доска: изменено')
})

test('запись про человека называет человека', () => {
  // «Состав подразделения: добавлено» не отвечает на главный вопрос —
  // кого. В снимке идентификатор, а имена лежат на этом же экране.
  const people = { 'u-1': 'Вера Соколова' }
  assert.equal(
    auditText(entry('insert', 'team_members', { new: { user_id: 'u-1' } }), people),
    'Состав подразделения: добавлено · Вера Соколова',
  )
  assert.equal(
    auditText(
      entry('update', 'memberships', {
        old: { user_id: 'u-1', role: 'member' },
        new: { user_id: 'u-1', role: 'viewer' },
      }),
      people,
    ),
    'Участие в организации: изменено — роль: участник → наблюдатель · Вера Соколова',
  )
  // Незнакомый идентификатор не превращается в мусор на экране.
  assert.equal(
    auditText(entry('delete', 'observers', { old: { user_id: 'u-9' } }), people),
    'Наблюдение: удалено',
  )
})

test('действие без установленной личности видно как таковое', () => {
  // Подделать подпись нельзя, а не назваться — можно: миграция и служебная
  // задача пишут в журнал без автора, и это не потеря данных.
  assert.equal(actorText(null), 'без имени')
  assert.equal(actorText('Иван'), 'Иван')
})
