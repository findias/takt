// Превращение записей журнала в человеческие строки.
//
// Журнал пишется машиной и хранит ровно то, что произошло: тип события
// и снимок затронутого. Читают его люди, и разбирать jsonb глазами
// им незачем.

import { blockUntilWords, dateWords, priorityLabel } from '../card/model.ts'
import { ROLE_NAMES, VISIBILITY_NAMES } from '../../shared/api/names.ts'
import type { AuditEntry, BoardEvent, CardField, Priority } from '../../shared/api/index.ts'
import { locale, t } from '../../shared/i18n/index.ts'

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
      return t.feed.created
    case 'moved': {
      // В событии лежит снимок колонок на момент перехода, а не ссылки:
      // переименование колонки не должно переписывать историю.
      const to = name(p.to)
      const from = name(p.from)
      const where = from && to ? t.feed.movedFromTo(from, to) : to ? t.feed.movedTo(to) : t.feed.moved
      if (p.crossedStart === true) return t.feed.workStarted(where)
      if (p.crossedFinish === true) return t.feed.workFinished(where)
      return where
    }
    case 'renamed':
      return typeof p.title === 'string' ? t.feed.renamedTo(p.title) : t.feed.renamed
    case 'described':
      return t.feed.described
    case 'archived':
      return t.feed.archived
    case 'linked':
      return t.feed.linked(linkKind(p.kind))
    case 'unlinked':
      return t.feed.unlinked(linkKind(p.kind))
    case 'commented':
      return t.feed.commented
    case 'iteration_added':
      return t.feed.iterationAdded
    case 'iteration_removed':
      return t.feed.iterationRemoved
    case 'estimated':
      return typeof p.estimate === 'number' ? t.feed.estimatedAt(p.estimate) : t.feed.estimated
    case 'restored':
      return t.feed.restored
    case 'committed':
      // Словами, как и вся остальная лента: одна машинная дата посреди
      // речи читается как чужая строка. Но без отсчёта от сегодня —
      // здесь не «просрочено», а «поставили тогда-то вот такой срок»:
      // запись о прошлом не имеет права меняться от того, что прошло
      // время.
      return typeof p.dueOn === 'string'
        ? t.feed.committedTo(dateWords(p.dueOn))
        : t.feed.uncommitted
    case 'prioritised':
      return typeof p.priority === 'string'
        ? t.feed.priorityTo(priorityLabel(p.priority as Priority).toLowerCase())
        : t.feed.prioritised
    // «Отмечена», а не «сделана»: слово называет действие человека,
    // а не факт о работе. Поток о ней по-прежнему судит по колонке
    // финиша, и ставить эти два события в один ряд нельзя.
    case 'done':
      return t.feed.done
    case 'undone':
      return t.feed.undone
    case 'blocked':
      return typeof p.reason === 'string' ? t.feed.blockedFor(p.reason) : t.feed.blocked
    case 'unblocked':
      return t.feed.unblocked
    // Автора у снятия по сроку нет, и строка читается как строка без
    // автора: «сама» говорит, почему подписи нет.
    case 'block_expired':
      return t.feed.blockExpired
    case 'block_until':
      return typeof p.until === 'string'
        ? t.feed.blockUntil(blockUntilWords(p.until))
        : t.feed.blockOpenEnded
    case 'field_set': {
      const field = fields.find((f) => f.id === p.fieldId)
      if (!field) return t.feed.fieldSet
      return t.feed.fieldValue(field.name, fieldValueText(p.value, field.kind))
    }
    case 'field_cleared': {
      const field = fields.find((f) => f.id === p.fieldId)
      return field ? t.feed.fieldClearedNamed(field.name) : t.feed.fieldCleared
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
  if (typeof value === 'boolean') return value ? t.feed.yes : t.feed.no
  if (typeof value === 'number') return String(value)
  if (typeof value !== 'string') return t.feed.valueChanged
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
  const what = t.feed.subjects[entry.subject] ?? entry.subject
  const named = subjectName(entry)
  const about = named ? `${what} «${named}»` : what
  // Записи про людей имени в снимке не хранят — там идентификатор.
  // Имена лежат на этом же экране, и без них «Состав подразделения:
  // добавлено» не отвечает на главный вопрос: кого.
  const who = personName(entry, people)
  const tail = who ? ` · ${who}` : ''
  switch (entry.action) {
    case 'insert':
      return t.feed.added(about, tail)
    case 'update': {
      const changed = changeText(entry)
      return changed ? t.feed.changedWhat(about, changed, tail) : t.feed.changed(about, tail)
    }
    case 'delete':
      return t.feed.removed(about, tail)
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

  const named = changed.map((key) => t.feed.fields[key] ?? key)
  if (changed.length === 1) {
    const key = changed[0]
    const values = VALUES[key]
    if (values) return `${named[0]}: ${values(before[key])} → ${values(after[key])}`
    return named[0]
  }
  if (named.length > 3) return t.feed.andMore(named.slice(0, 3).join(', '), named.length - 3)
  return named.join(', ')
}

function side(entry: AuditEntry, which: 'old' | 'new'): Record<string, unknown> | null {
  const value = entry.payload?.[which]
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : null
}

// Меняется само и ничего не рассказывает: по этим полям запись выглядела
// бы изменённой, ничем не отличаясь от соседней.
const NOISE = new Set(['id', 'org_id', 'version', 'created_at', 'updated_at', 'card_seq'])


// Значения, у которых есть человеческое имя. Остальные показываются
// только именем поля: подставлять в ленту идентификатор — то же самое,
// что показывать сырой jsonb.
const VALUES: Record<string, (value: unknown) => string> = {
  role: (v) => sideValue(ROLE_NAMES, v),
  visibility: (v) => sideValue(VISIBILITY_NAMES, v),
}

function sideValue(names: Record<string, string>, value: unknown): string {
  if (typeof value !== 'string') return t.feed.unset
  return (names[value] ?? value).toLowerCase()
}


/** Имя, которое стоит показать рядом с записью. */
export function actorText(actor: string | null): string {
  // Пустой автор — не потеря данных: действие сделано без установленной
  // личности, миграцией или служебной задачей. Подделать подпись нельзя,
  // а не назваться — можно, и это видно как есть.
  //
  // Не «без имени»: так читается, будто у человека нет имени. Подписи
  // нет — потому что подписывать было некому.
  return actor ?? t.feed.noActor
}

export function timeText(iso: string): string {
  return new Date(iso).toLocaleString(locale(), {
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
      return t.feed.linkSubtask
    case 'blocks':
      return t.feed.linkBlocks
    case 'relates':
      return t.feed.linkRelates
    default:
      return typeof kind === 'string' ? kind : t.feed.link
  }
}
