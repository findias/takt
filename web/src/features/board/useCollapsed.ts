import { useCallback, useState } from 'react'

/**
 * Свёрнутые колонки.
 *
 * Состояние сессии, а не адреса и не сервера: свёрнутая колонка —
 * личное предпочтение того, кто смотрит. Сложить «Готово», чтобы она
 * не занимала треть экрана, — обычное дело, но навязывать это остальным
 * или тащить в ссылку незачем: адрес описывает, что показано, а не как
 * это уложено.
 *
 * Хранится по доскам: на одной доске сворачивают «Готово», на другой —
 * «Идеи», и общий список превратил бы это в лотерею.
 */
const KEY = 'collapsed-columns'

type Stored = Record<string, string[]>

function read(): Stored {
  try {
    const raw = localStorage.getItem(KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : {}
    return parsed && typeof parsed === 'object' ? (parsed as Stored) : {}
  } catch {
    // Испорченное хранилище — не повод не показать доску. Молчание
    // здесь осознанное: человеку об этом знать нечего, колонки просто
    // окажутся развёрнутыми.
    return {}
  }
}

export function useCollapsedColumns(boardId: string): {
  collapsed: Set<string>
  toggle: (columnId: string) => void
} {
  const [stored, setStored] = useState<Stored>(read)

  const toggle = useCallback(
    (columnId: string) => {
      setStored((current) => {
        const mine = new Set(current[boardId] ?? [])
        if (mine.has(columnId)) mine.delete(columnId)
        else mine.add(columnId)
        const next = { ...current, [boardId]: [...mine] }
        try {
          localStorage.setItem(KEY, JSON.stringify(next))
        } catch {
          // Переполненное хранилище: сворачивание переживёт сессию,
          // но не перезагрузку. Ронять из-за этого нечего.
        }
        return next
      })
    },
    [boardId],
  )

  return { collapsed: new Set(stored[boardId] ?? []), toggle }
}
