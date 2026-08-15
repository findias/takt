// Превращение записей журнала в человеческие строки.
//
// Журнал пишется машиной и хранит ровно то, что произошло: тип события
// и снимок затронутого. Читают его люди, и разбирать jsonb глазами
// им незачем.

import type { AuditEntry, BoardEvent } from '../../shared/api/index.ts'

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
