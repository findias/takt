import { useCallback, useState } from 'react'

/**
 * Ширина колонки (ROADMAP 28.2).
 *
 * Одна ширина на все колонки всех досок одинаково не подходит «В работе»,
 * где длинные названия, исполнители и метки, и «Готово», где один
 * заголовок. Ширину тянут мышью.
 *
 * Хранится рядом со свёрнутостью и по той же причине (`useCollapsed.ts`):
 * как уложена доска — личное дело смотрящего. В адрес это не едет,
 * остальным не навязывается, миграции, операции и права на правку
 * доски не нужно — наблюдатель тянет колонки так же, как участник.
 * Чего этим не получаем: ширина не переезжает на другую машину,
 * и владелец не раскладывает доску всем. Это осознанно.
 *
 * Единица — rem, а не пиксели: ширина обязана расти вместе с текстом,
 * иначе при крупном шрифте «широкая» колонка окажется узкой.
 */

/** Прежняя общая ширина — она же ширина колонки, которую не трогали. */
export const COLUMN_WIDTH_DEFAULT = 17.5
/** Ниже карточка теряет смысл: контейнерный порог 14rem уже прячет
 *  метки, срок и оценку, и 12rem — место, где она ещё читается. */
export const COLUMN_WIDTH_MIN = 12
/** Выше одна колонка съедает экран: «растянул и потерял остальные»
 *  стало бы следующим отчётом об ошибке. */
export const COLUMN_WIDTH_MAX = 40
/** Шаг клавиатуры. */
export const COLUMN_WIDTH_STEP = 1

const KEY = 'column-widths'

type Stored = Record<string, Record<string, number>>

export function clampColumnWidth(rem: number): number {
  if (!Number.isFinite(rem)) return COLUMN_WIDTH_DEFAULT
  return Math.min(COLUMN_WIDTH_MAX, Math.max(COLUMN_WIDTH_MIN, rem))
}

function readAll(): Stored {
  try {
    const raw = localStorage.getItem(KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : {}
    return parsed && typeof parsed === 'object' ? (parsed as Stored) : {}
  } catch {
    // Испорченное хранилище — не повод не показать доску: колонки
    // окажутся прежней ширины, и знать об этом человеку нечего.
    return {}
  }
}

/** Ширины колонок доски. Записанное вне пределов возвращается в пределы:
 *  пределы могли смениться после того, как ширину записали. */
export function readColumnWidths(boardId: string): Record<string, number> {
  const mine = readAll()[boardId]
  if (!mine || typeof mine !== 'object') return {}
  const out: Record<string, number> = {}
  for (const [columnId, rem] of Object.entries(mine)) {
    if (typeof rem === 'number') out[columnId] = clampColumnWidth(rem)
  }
  return out
}

/** `null` — вернуть исходную: запись убирается, а не пишется умолчание,
 *  иначе смена умолчания не дошла бы до того, кто однажды сбросил. */
export function writeColumnWidth(boardId: string, columnId: string, rem: number | null): void {
  const all = readAll()
  const mine = { ...(typeof all[boardId] === 'object' ? all[boardId] : {}) }
  if (rem === null) delete mine[columnId]
  else mine[columnId] = clampColumnWidth(rem)
  const next = { ...all, [boardId]: mine }
  if (Object.keys(mine).length === 0) delete next[boardId]
  try {
    localStorage.setItem(KEY, JSON.stringify(next))
  } catch {
    // Переполненное хранилище: ширина переживёт сессию, но не
    // перезагрузку. Ронять из-за этого нечего.
  }
}

export function useColumnWidths(boardId: string): {
  widths: Record<string, number>
  setWidth: (columnId: string, rem: number | null) => void
} {
  const [widths, setWidths] = useState(() => readColumnWidths(boardId))
  const setWidth = useCallback(
    (columnId: string, rem: number | null) => {
      writeColumnWidth(boardId, columnId, rem)
      setWidths(readColumnWidths(boardId))
    },
    [boardId],
  )
  return { widths, setWidth }
}
