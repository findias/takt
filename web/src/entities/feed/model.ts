// Превращение записей журнала в человеческие строки.
//
// Журнал пишется машиной и хранит ровно то, что произошло: тип события
// и снимок затронутого. Читают его люди, и разбирать jsonb глазами
// им незачем.

import { dateWords, priorityLabel } from '../card/model.ts'
import type { AuditEntry, BoardEvent, Priority } from '../../shared/api/index.ts'

/** Что произошло с карточкой. */
export function eventText(event: BoardEvent): string {
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
    case 'blocked':
      return typeof p.reason === 'string' ? `заблокирована: ${p.reason}` : 'заблокирована'
    case 'unblocked':
      return 'блокировка снята'
    default:
      // Неизвестный тип показываем как есть. Событие уже случилось,
      // и молчать о нём хуже, чем показать непонятно.
      return event.type
  }
}

/** Что произошло в организации. */
export function auditText(entry: AuditEntry): string {
  const what = SUBJECTS[entry.subject] ?? entry.subject
  switch (entry.action) {
    case 'insert':
      return `${what}: добавлено`
    case 'update':
      return `${what}: изменено`
    case 'delete':
      return `${what}: удалено`
    default:
      return `${what}: ${entry.action}`
  }
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
