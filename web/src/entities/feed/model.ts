// Превращение записей журнала в человеческие строки.
//
// Журнал пишется машиной и хранит ровно то, что произошло: тип события
// и снимок затронутого. Читают его люди, и разбирать jsonb глазами
// им незачем.

import { dateWords, priorityLabel } from '../card/model.ts'
import { ROLE_NAMES, VISIBILITY_NAMES } from '../../shared/api/names.ts'
import type { AuditEntry, BoardEvent, CardField, Priority } from '../../shared/api/index.ts'

/**
 * Что произошло с карточкой.
 *
 * Своё поле названо по имени, а не по ссылке: `field_set` с полем
 * `f-17` — это машинная строка посреди речи, и читать её так же нечем,
 * как сырой jsonb. Имена приходят из снимка доски; если их не передали
 * (лента доски, куда снимок не доехал), событие называется общими
 * словами, но остаётся понятным.
 */
export function eventText(event: BoardEvent, fields: CardField[] = []): string {
  const p = event.payload ?? {}
  switch (event.type) {
    case 'created':
      return 'создана'
    case 'moved': {
      // В событии лежит снимок колонок на момент перехода, а не ссылки:
      // переименование колонки не должно переписывать историю.
      const to = name(p.to)
      const from = name(p.from)
      const where = from && to ? `из «${from}» в «${to}»` : to ? `в «${to}»` : 'перемещена'
      if (p.crossedStart === true) return `${where} — работа началась`
      if (p.crossedFinish === true) return `${where} — работа закончена`
      return where
    }
    case 'renamed':
      return typeof p.title === 'string' ? `переименована в «${p.title}»` : 'переименована'
    case 'described':
      return 'изменено описание'
    case 'archived':
      return 'убрана с доски'
    case 'linked':
      return `связана: ${linkKind(p.kind)}`
    case 'unlinked':
      return `связь снята: ${linkKind(p.kind)}`
    case 'commented':
      return 'написано в обсуждении'
    case 'iteration_added':
      return 'добавлена в итерацию'
    case 'iteration_removed':
      return 'убрана из итерации'
    case 'estimated':
      return typeof p.estimate === 'number' ? `оценена в ${p.estimate}` : 'оценка изменена'
    case 'restored':
      return 'возвращена на доску'
    case 'committed':
      // Словами, как и вся остальная лента: одна машинная дата посреди
      // речи читается как чужая строка. Но без отсчёта от сегодня —
      // здесь не «просрочено», а «поставили тогда-то вот такой срок»:
      // запись о прошлом не имеет права меняться от того, что прошло
      // время.
      return typeof p.dueOn === 'string'
        ? `обязательство: ${dateWords(p.dueOn)}`
        : 'обязательство снято'
    case 'prioritised':
      return typeof p.priority === 'string'
        ? `приоритет: ${priorityLabel(p.priority as Priority).toLowerCase()}`
        : 'изменён приоритет'
    // «Отмечена», а не «сделана»: слово называет действие человека,
    // а не факт о работе. Поток о ней по-прежнему судит по колонке
    // финиша, и ставить эти два события в один ряд нельзя.
    case 'done':
      return 'отмечена сделанной'
    case 'undone':
      return 'отметка «сделана» снята'
    case 'blocked':
      return typeof p.reason === 'string' ? `заблокирована: ${p.reason}` : 'заблокирована'
    case 'unblocked':
      return 'блокировка снята'
    case 'field_set': {
      const field = fields.find((f) => f.id === p.fieldId)
      if (!field) return 'заполнено своё поле'
      return `«${field.name}»: ${fieldValueText(p.value, field.kind)}`
    }
    case 'field_cleared': {
      const field = fields.find((f) => f.id === p.fieldId)
      return field ? `поле «${field.name}» очищено` : 'своё поле очищено'
    }
    default:
      // Неизвестный тип показываем как есть. Событие уже случилось,
      // и молчать о нём хуже, чем показать непонятно.
      return event.type
  }
}

/**
 * Значение своего поля словами.
 *
 * Дата переводится словами по виду поля, а не по виду строки: угадывать
 * дату в тексте нельзя — «2026-08-18» бывает и обычной строкой, которую
 * человек так и написал.
 */
function fieldValueText(value: unknown, kind: CardField['kind']): string {
  if (typeof value === 'boolean') return value ? 'да' : 'нет'
  if (typeof value === 'number') return String(value)
  if (typeof value !== 'string') return 'значение изменено'
  if (kind === 'date') return dateWords(value)
  // Длинное значение обрывается: строка журнала — одна строка, и абзац
  // из своего поля вытесняет из неё всё остальное.
  return value.length > 60 ? `${value.slice(0, 60)}…` : value
}

/**
 * Что произошло в организации.
 *
 * Названо и то, с чем это произошло. «Доска: изменено» три раза подряд —
 * это не журнал, а список таблиц: по нему нельзя ответить ни на один
 * вопрос, ради которого журнал заводят. Имя объекта и то, что в нём
 * изменилось, лежат в снимке, который и так приходит с записью, —
 * до сих пор их просто выбрасывали.
 */
export function auditText(entry: AuditEntry, people: Record<string, string> = {}): string {
  const what = SUBJECTS[entry.subject] ?? entry.subject
  const named = subjectName(entry)
  const about = named ? `${what} «${named}»` : what
  // Записи про людей имени в снимке не хранят — там идентификатор.
  // Имена лежат на этом же экране, и без них «Состав подразделения:
  // добавлено» не отвечает на главный вопрос: кого.
  const who = personName(entry, people)
  const tail = who ? ` · ${who}` : ''
  switch (entry.action) {
    case 'insert':
      return `${about}: добавлено${tail}`
    case 'update': {
      const changed = changeText(entry)
      return changed ? `${about}: изменено — ${changed}${tail}` : `${about}: изменено${tail}`
    }
    case 'delete':
      return `${about}: удалено${tail}`
    default:
      return `${about}: ${entry.action}${tail}`
  }
}

function personName(entry: AuditEntry, people: Record<string, string>): string {
  const id = field(side(entry, 'new'), 'user_id') || field(side(entry, 'old'), 'user_id')
  return id ? (people[id] ?? '') : ''
}

function field(row: Record<string, unknown> | null, key: string): string {
  const value = row?.[key]
  return typeof value === 'string' ? value : ''
}

/** Имя затронутого, если оно у него есть: у подразделения и доски есть,
 *  у участия и наблюдения — нет, там объект называют по-другому. */
function subjectName(entry: AuditEntry): string {
  return name(side(entry, 'new')) || name(side(entry, 'old'))
}

/**
 * Что именно изменилось.
 *
 * Одно изменившееся поле называется вместе с обеими сторонами, если
 * значение из известного набора: «видимость» без «было → стало» —
 * это половина ответа, а ради второй половины и приходят в журнал.
 * Нескольких хватает по именам: строка в ленте одна, и перечисление
 * переходов её переполнит.
 */
function changeText(entry: AuditEntry): string {
  const before = side(entry, 'old')
  const after = side(entry, 'new')
  if (!before || !after) return ''

  const changed = Object.keys(after).filter(
    (key) => !NOISE.has(key) && JSON.stringify(before[key]) !== JSON.stringify(after[key]),
  )
  if (changed.length === 0) return ''

  const named = changed.map((key) => FIELDS[key] ?? key)
  if (changed.length === 1) {
    const key = changed[0]
    const values = VALUES[key]
    if (values) return `${named[0]}: ${values(before[key])} → ${values(after[key])}`
    return named[0]
  }
  if (named.length > 3) return `${named.slice(0, 3).join(', ')} и ещё ${named.length - 3}`
  return named.join(', ')
}

function side(entry: AuditEntry, which: 'old' | 'new'): Record<string, unknown> | null {
  const value = entry.payload?.[which]
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : null
}

// Меняется само и ничего не рассказывает: по этим полям запись выглядела
// бы изменённой, ничем не отличаясь от соседней.
const NOISE = new Set(['id', 'org_id', 'version', 'created_at', 'updated_at', 'card_seq'])

const FIELDS: Record<string, string> = {
  name: 'название',
  key: 'ключ',
  role: 'роль',
  email: 'почта',
  visibility: 'видимость',
  team_id: 'подразделение',
  parent_id: 'вышестоящее подразделение',
  project_id: 'проект',
  archived_at: 'архив',
  discarded_at: 'удаление',
  accepted_at: 'принято',
  revoked_at: 'отозвано',
  expires_at: 'срок действия',
  sle_days: 'ожидаемый срок',
  sle_probability: 'доля в срок',
  audit_retention_days: 'срок хранения журнала',
}

// Значения, у которых есть человеческое имя. Остальные показываются
// только именем поля: подставлять в ленту идентификатор — то же самое,
// что показывать сырой jsonb.
const VALUES: Record<string, (value: unknown) => string> = {
  role: (v) => sideValue(ROLE_NAMES, v),
  visibility: (v) => sideValue(VISIBILITY_NAMES, v),
}

function sideValue(names: Record<string, string>, value: unknown): string {
  if (typeof value !== 'string') return 'не задано'
  return (names[value] ?? value).toLowerCase()
}

const SUBJECTS: Record<string, string> = {
  memberships: 'Участие в организации',
  invites: 'Приглашение',
  teams: 'Подразделение',
  team_members: 'Состав подразделения',
  board_members: 'Состав доски',
  observers: 'Наблюдение',
  boards: 'Доска',
  cards: 'Карточка',
  users: 'Личность',
  // Выгрузка попадает в журнал как действие, а не как таблица: строка
  // берётся из кода, а не из имени отношения, — но читателю журнала
  // это всё равно раздел, и сырое слово в ленте выглядит недоделкой.
  export: 'Выгрузка данных',
}

/** Имя, которое стоит показать рядом с записью. */
export function actorText(actor: string | null): string {
  // Пустой автор — не потеря данных: действие сделано без установленной
  // личности, миграцией или служебной задачей. Подделать подпись нельзя,
  // а не назваться — можно, и это видно как есть.
  return actor ?? 'без имени'
}

export function timeText(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function name(side: unknown): string {
  if (side && typeof side === 'object' && 'name' in side) {
    const value = (side as { name: unknown }).name
    if (typeof value === 'string') return value
  }
  return ''
}

function linkKind(kind: unknown): string {
  switch (kind) {
    case 'subtask':
      return 'подзадача'
    case 'blocks':
      return 'блокирует'
    case 'relates':
      return 'смежная'
    default:
      return typeof kind === 'string' ? kind : 'связь'
  }
}
